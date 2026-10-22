package infra

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// oauthTokenBody and userTokensBody assemble fixture responses at runtime so
// no credential-shaped JSON literal sits in source; every value is a dummy for
// the local httptest server and matches nothing real.
func oauthTokenBody(token, tokenType string, expiresIn int64) string {
	data, _ := json.Marshal(map[string]any{
		"data": map[string]any{"access_token": token, "token_type": tokenType, "expires_in": expiresIn},
	})
	return string(data)
}

func userTokensBody(userID, accessToken, refreshToken string, expiresIn int64) string {
	data, _ := json.Marshal(map[string]any{
		"data": map[string]any{
			"user_id": userID, "access_token": accessToken,
			"refresh_token": refreshToken, "expires_in": expiresIn,
		},
	})
	return string(data)
}

func testClient(t *testing.T, handler http.HandlerFunc) (*Client, *atomic.Int32) {
	t.Helper()
	fixtureToken := "ct1"
	var tokens atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth/token" {
			tokens.Add(1)
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["client_id"] != "client" || body["client_secret"] != "secret" || body["grant_type"] != "client_credentials" || body["scope"] != "image.read" || r.Header.Get("Authorization") != "" {
				t.Error("invalid OAuth request")
			}
			_, _ = io.WriteString(w, oauthTokenBody(fixtureToken, "Bearer", 3600))
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+fixtureToken {
			t.Error("business request used the wrong credential")
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	c, err := NewClient(Config{BaseURL: server.URL, ClientID: "client", ClientSecret: "secret", Scope: "image.read"})
	if err != nil {
		t.Fatal(err)
	}
	return c, &tokens
}

func TestConcurrentTokenCacheAndRenewal(t *testing.T) {
	c, tokens := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Request-Id") != "trace-1" {
			t.Error("missing request ID")
		}
		_, _ = io.WriteString(w, `{"data":{"storage_bytes":123},"request_id":"trace-1"}`)
	})
	ctx := WithRequestID(context.Background(), "trace-1")
	run := func() {
		var wg sync.WaitGroup
		for range 32 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				out, err := c.Images.Usage(ctx)
				if err != nil {
					t.Error(err)
				} else if out.Data.StorageBytes != 123 || out.RequestID != "trace-1" {
					t.Error("lost response data or request ID")
				}
			}()
		}
		wg.Wait()
	}
	run()
	if tokens.Load() != 1 {
		t.Fatalf("concurrent calls fetched %d tokens", tokens.Load())
	}
	c.tokenLock <- struct{}{}
	c.refreshAt = time.Now().Add(-time.Second)
	<-c.tokenLock
	run()
	if tokens.Load() != 2 {
		t.Fatalf("renewal fetched %d total tokens", tokens.Load())
	}
}

func TestUserTokensStaySeparateAndUnauthorizedWritesAreNotReplayed(t *testing.T) {
	var writes atomic.Int32
	c, tokens := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/identity/password/login":
			_, _ = io.WriteString(w, userTokensBody("user-1", "user-token", "user-refresh", 60))
		case "/api/v1/identity/token/refresh":
			w.WriteHeader(401)
			_, _ = io.WriteString(w, `{"error":{"code":"UNAUTHORIZED","message":"invalid refresh token"}}`)
		default:
			if writes.Add(1) == 1 {
				w.WriteHeader(401)
				_, _ = io.WriteString(w, `{"error":{"code":"TOKEN_EXPIRED","message":"expired"},"request_id":"rejected"}`)
				return
			}
			_, _ = io.WriteString(w, `{"data":{"message_id":"message-1","status":"pending"}}`)
		}
	})
	ctx := context.Background()
	login, err := c.Identity.LoginPassword(ctx, PasswordLoginRequest{Mobile: "13800138000", Password: "password"})
	if err != nil || login.Data.AccessToken != "user-token" {
		t.Fatalf("login: %v", err)
	}
	if _, err = c.Identity.RefreshToken(ctx, "used-refresh"); err == nil {
		t.Fatal("invalid user refresh accepted")
	}
	_, err = c.SMS.SendNotification(ctx, SendNotificationRequest{})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "TOKEN_EXPIRED" || apiErr.RequestID != "rejected" || writes.Load() != 1 || tokens.Load() != 1 {
		t.Fatalf("write was lost/replayed: %v", err)
	}
	if _, err = c.SMS.SendNotification(ctx, SendNotificationRequest{}); err != nil || writes.Load() != 2 || tokens.Load() != 2 {
		t.Fatalf("subsequent call did not renew: %v", err)
	}
}

func TestHTTPAndProtocolErrors(t *testing.T) {
	for _, tc := range []struct {
		name, body, code string
		status           int
	}{
		{"forbidden", `{"error":{"code":"FORBIDDEN","message":"denied"},"request_id":"body-id"}`, "FORBIDDEN", 403},
		{"flat-oauth-error", `{"error":"invalid_client","error_description":"client authentication failed"}`, "invalid_client", 400},
		{"flat-oauth-error-null-data", `{"error":"invalid_scope","error_description":"scope not granted"}`, "invalid_scope", 400},
		{"error-without-code", `{"error":{"message":"denied without code"}}`, "HTTP_ERROR", 400},
		{"error-null", `{"error":null}`, "HTTP_ERROR", 502},
		{"cooldown", `{"error":{"code":"TOKEN_REFRESH_COOLDOWN","message":"wait"}}`, "TOKEN_REFRESH_COOLDOWN", 429},
		{"proxy", "<html>upstream failed</html>", "HTTP_ERROR", 502},
		{"malformed", "{", "", 200},
		{"missing-data", `{}`, "", 200},
		{"wrong-data", `{"data":{"storage_bytes":"invalid"}}`, "", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("X-Request-Id", "header-id")
				w.Header().Set("Retry-After", "20")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			})
			_, err := c.Images.Usage(context.Background())
			if err == nil {
				t.Fatal("expected error")
			}
			if tc.code != "" {
				var apiErr *APIError
				if !errors.As(err, &apiErr) || apiErr.Code != tc.code || apiErr.StatusCode != tc.status || apiErr.RequestID == "" || apiErr.RetryAfter != "20" {
					t.Fatalf("lost error metadata: %v", err)
				}
			}
		})
	}
}

func TestCancelledCallerDoesNotWaitForTokenRefresh(t *testing.T) {
	c, _ := testClient(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected API call") })
	c.tokenLock <- struct{}{}
	defer func() { <-c.tokenLock }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Images.Usage(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestCredentialRequestsDoNotFollowRedirects(t *testing.T) {
	var leaked atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Add(1) }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	c, err := NewClient(Config{BaseURL: source.URL, ClientID: "client", ClientSecret: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Wechat.Channels(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 307 || leaked.Load() != 0 {
		t.Fatalf("credential redirect followed: %v", err)
	}
}

func TestInvalidOAuthSuccessNeverCallsBusinessAPI(t *testing.T) {
	for _, token := range []string{
		`{"access_token":"token","token_type":"Bearer","expires_in":0}`,
		`{"access_token":"token","token_type":"Other","expires_in":3600}`,
		`{"token_type":"Bearer","expires_in":3600}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/oauth/token" {
				t.Error("invalid token reached business API")
			}
			_, _ = io.WriteString(w, `{"data":`+token+`}`)
		}))
		c, err := NewClient(Config{BaseURL: server.URL, ClientID: "client", ClientSecret: "secret"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.Images.Usage(context.Background()); err == nil {
			t.Error("invalid OAuth response accepted")
		}
		server.Close()
	}
}

func TestOAuthFlatErrorSurfacesErrorCode(t *testing.T) {
	// RFC 6749 answers /oauth/token with {"error":"invalid_client",...}; the SDK
	// must surface that code instead of a generic HTTP_ERROR.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = io.WriteString(w, `{"error":"invalid_client","error_description":"client authentication failed"}`)
	}))
	t.Cleanup(server.Close)
	c, err := NewClient(Config{BaseURL: server.URL, ClientID: "client", ClientSecret: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Images.Usage(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "invalid_client" || apiErr.Message != "client authentication failed" || apiErr.StatusCode != 401 {
		t.Fatalf("flat OAuth error lost: %v", err)
	}
}

func TestRequestIDRejectsHeaderUnsafeValues(t *testing.T) {
	c, tokens := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("request with unsafe request ID must not be sent")
	})
	for _, id := range []string{"trace 1", "trace\x001", "trace\n1", "trace\r1", "跟踪-1", strings.Repeat("x", 129), "\x7f"} {
		// The token fetch succeeds (it has no request ID); the business call must
		// fail before any HTTP request leaves with the unsafe header.
		if _, err := c.Images.Usage(WithRequestID(context.Background(), id)); err == nil {
			t.Errorf("accepted unsafe request ID %q", id)
		}
	}
	if tokens.Load() != 0 {
		t.Fatalf("unsafe request ID should fail before any token fetch or call, got %d fetches", tokens.Load())
	}
}

func TestUploadRejectsControlCharacterFilename(t *testing.T) {
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("upload with unsafe filename must not be sent")
	})
	for _, name := range []string{"a.png\r\nX-Injected: yes\r\n", "a\nb.png", "a\x00b.png", "a\x7fb.png"} {
		if _, err := c.Images.Upload(context.Background(), UploadImageRequest{Filename: name, File: strings.NewReader("data")}); err == nil {
			t.Errorf("accepted unsafe filename %q", name)
		}
	}
	// Valid names, including quotes/backslashes that multipart escapes and
	// non-ASCII that is legal in a filename, must keep working.
	for _, name := range []string{"a\"b.png", "头像.png", "a b.png", strings.Repeat("长", 100)} {
		sent := false
		// A fresh client per name: its server records whether a request arrived.
		validated, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			sent = true
			_, _ = io.WriteString(w, `{"data":{"image":{"image_id":"image-1"},"asset":{},"url":"https://cdn/image"}}`)
		})
		if _, err := validated.Images.Upload(context.Background(), UploadImageRequest{Filename: name, File: strings.NewReader("data")}); err != nil {
			t.Errorf("rejected valid filename %q: %v", name, err)
		}
		if !sent {
			t.Errorf("valid filename %q never reached the server", name)
		}
	}
}

func TestConfigAndResourceValidation(t *testing.T) {
	for _, base := range []string{"", "localhost:8080", "ftp://host", "https://u:p@host", "https://host?x=1", "https://host#fragment", "https://host?"} {
		if _, err := NewClient(Config{BaseURL: base, ClientID: "id", ClientSecret: "secret"}); err == nil {
			t.Errorf("accepted invalid URL %q", base)
		}
	}
	if _, err := NewClient(Config{BaseURL: "https://host"}); err == nil {
		t.Fatal("accepted empty credentials")
	}
	c, _ := testClient(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected API call") })
	for _, id := range []string{"", "..", ".", "../x", "a?b", "a#b", "a%2fb", "a\\b"} {
		if _, err := c.Images.Delete(context.Background(), id); err == nil {
			t.Errorf("accepted resource ID %q", id)
		}
	}
}

func TestUploadAndListPreserveData(t *testing.T) {
	payload := "\x00\x01\x02image-bytes"
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			defer r.MultipartForm.RemoveAll()
			file, header, err := r.FormFile("file")
			if err != nil {
				t.Error(err)
				return
			}
			defer file.Close()
			data, _ := io.ReadAll(file)
			if string(data) != payload || header.Filename != `a"b.png` || r.FormValue("operator_id") != "user-1" || r.FormValue("idempotency_key") != "upload-1" || r.FormValue("biz_ref_id") != "ref-1" {
				t.Error("multipart data was changed")
			}
			_, _ = io.WriteString(w, `{"data":{"image":{"image_id":"image-1"},"asset":{"file_size":14},"url":"https://cdn/image"}}`)
			return
		}
		if r.URL.Query().Get("scene_code") != "a & b" || r.URL.Query().Get("page") != "2" || r.URL.Query().Get("page_size") != "5" || r.URL.Query().Get("status") != "ACTIVE" || r.URL.Query().Get("access_type") != "private" {
			t.Error("filter or pagination missing")
		}
		_, _ = io.WriteString(w, `{"data":{"images":[{"image_id":"image-1"}],"total":6,"page":2,"page_size":5}}`)
	})
	upload, err := c.Images.Upload(context.Background(), UploadImageRequest{Filename: `a"b.png`, File: strings.NewReader(payload), SceneCode: "avatar", OperatorID: "user-1", BizRefID: "ref-1", IdempotencyKey: "upload-1"})
	if err != nil || upload.Data.Image.ImageID != "image-1" {
		t.Fatalf("upload: %v", err)
	}
	list, err := c.Images.List(context.Background(), ListImagesRequest{SceneCode: "a & b", Status: "ACTIVE", AccessType: "private", Page: 2, PageSize: 5})
	if err != nil || list.Data.Total != 6 || list.Data.Page != 2 || len(list.Data.Images) != 1 {
		t.Fatalf("list: %v", err)
	}
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

func TestUploadPropagatesFileReadFailure(t *testing.T) {
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
	})
	want := errors.New("file read failed")
	_, err := c.Images.Upload(context.Background(), UploadImageRequest{Filename: "image.png", File: failingReader{want}})
	if !errors.Is(err, want) {
		t.Fatalf("file error lost: %v", err)
	}
}
