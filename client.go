// Package infra is the server-side Go SDK for Youwei Infra Center.
package infra

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Config holds backend-only credentials. Reuse one Client across goroutines.
type Config struct {
	BaseURL      string
	ClientID     string
	ClientSecret string
	Scope        string
	HTTPClient   *http.Client
}

type Client struct {
	Identity *IdentityService
	Images   *ImageService
	SMS      *SMSService
	Wechat   *WechatService

	baseURL      string
	clientID     string
	clientSecret string
	scope        string
	http         *http.Client
	tokenLock    chan struct{}
	token        string
	refreshAt    time.Time
}

type Response[T any] struct {
	Data      T      `json:"data"`
	RequestID string `json:"request_id"`
}

// APIError preserves the service error code and correlation/recovery headers.
type APIError struct {
	StatusCode int
	Code       string
	Message    string
	RequestID  string
	RetryAfter string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("infra: HTTP %d %s: %s (request_id=%s)", e.StatusCode, e.Code, e.Message, e.RequestID)
}

func NewClient(cfg Config) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(cfg.BaseURL, "/"))
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, errors.New("infra: BaseURL must be an http(s) URL without credentials, query or fragment")
	}
	if strings.TrimSpace(cfg.ClientID) == "" || strings.TrimSpace(cfg.ClientSecret) == "" {
		return nil, errors.New("infra: ClientID and ClientSecret are required")
	}
	httpClient := http.Client{Timeout: 30 * time.Second}
	if cfg.HTTPClient != nil {
		httpClient = *cfg.HTTPClient
	}
	// Redirects must never forward a credential POST or Bearer to another endpoint.
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	c := &Client{
		baseURL: u.String(), clientID: cfg.ClientID, clientSecret: cfg.ClientSecret,
		scope: cfg.Scope, http: &httpClient, tokenLock: make(chan struct{}, 1),
	}
	c.Identity = &IdentityService{c}
	c.Images = &ImageService{c}
	c.SMS = &SMSService{c}
	c.Wechat = &WechatService{c}
	return c, nil
}

type requestIDKey struct{}

// WithRequestID propagates an application's request ID to both OAuth and API calls.
// The ID must be 1-128 printable ASCII characters without whitespace; anything
// else is rejected when the request is sent.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, requestID)
}

const maxHeaderBytes = 128

// headerSafe reports whether s can be sent as an HTTP header value without
// smuggling extra header lines: printable ASCII 0x21-0x7e only, length capped.
func headerSafe(s string) bool {
	if s == "" || len(s) > maxHeaderBytes {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x21 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

// filenameSafe reports whether s can travel inside a multipart Content-Disposition
// quoted string. mime/multipart escapes quotes and backslashes but not CR or LF,
// which would forge extra part headers; other control bytes are rejected too.
// Spaces and non-ASCII UTF-8 are valid filename bytes and stay allowed.
func filenameSafe(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return false
		}
	}
	return true
}

func (c *Client) accessToken(ctx context.Context) (string, error) {
	select {
	case c.tokenLock <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { <-c.tokenLock }()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if c.token != "" && time.Now().Before(c.refreshAt) {
		return c.token, nil
	}
	var token struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	started := time.Now()
	_, err := c.requestJSON(ctx, http.MethodPost, "/oauth/token", nil, map[string]string{
		"grant_type": "client_credentials", "client_id": c.clientID,
		"client_secret": c.clientSecret, "scope": c.scope,
	}, "", &token)
	if err != nil {
		return "", err
	}
	if token.AccessToken == "" || !strings.EqualFold(token.TokenType, "Bearer") || token.ExpiresIn <= 0 || token.ExpiresIn > int64((1<<63-1)/time.Second) {
		return "", errors.New("infra: invalid OAuth token response")
	}
	ttl := time.Duration(token.ExpiresIn) * time.Second
	margin := min(5*time.Minute, ttl/10)
	c.token, c.refreshAt = token.AccessToken, started.Add(ttl-margin)
	return c.token, nil
}

func (c *Client) invalidateToken(ctx context.Context, usedToken string) {
	select {
	case c.tokenLock <- struct{}{}:
		if c.token == usedToken {
			c.token = ""
		}
		<-c.tokenLock
	case <-ctx.Done():
	}
}

func call[T any](ctx context.Context, c *Client, method, path string, query url.Values, body any) (*Response[T], error) {
	token, err := c.accessToken(ctx)
	if err != nil {
		return nil, err
	}
	result := new(Response[T])
	result.RequestID, err = c.requestJSON(ctx, method, path, query, body, token, &result.Data)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func resource[T any](ctx context.Context, c *Client, method, prefix, id, suffix string, query url.Values, body any) (*Response[T], error) {
	if strings.TrimSpace(id) == "" || id == "." || id == ".." || strings.ContainsAny(id, "/\\%?#") {
		return nil, errors.New("infra: invalid resource identifier")
	}
	return call[T](ctx, c, method, prefix+"/"+url.PathEscape(id)+suffix, query, body)
}

func (c *Client) requestJSON(ctx context.Context, method, path string, query url.Values, body any, token string, out any) (string, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return "", fmt.Errorf("infra: encode request: %w", err)
		}
		reader = bytes.NewReader(data)
	}
	return c.request(ctx, method, path, query, reader, "application/json", token, out)
}

const maxResponseBytes = 16 << 20

func (c *Client) request(ctx context.Context, method, path string, query url.Values, body io.Reader, contentType, token string, out any) (string, error) {
	endpoint := c.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return "", fmt.Errorf("infra: create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", contentType)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if requestID, _ := ctx.Value(requestIDKey{}).(string); requestID != "" {
		if !headerSafe(requestID) {
			return "", errors.New("infra: request ID must be 1-128 printable ASCII characters without whitespace")
		}
		req.Header.Set("X-Request-Id", requestID)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("infra: request: %w", err)
	}
	defer res.Body.Close()
	requestID := res.Header.Get("X-Request-Id")
	data, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes+1))
	if err != nil {
		return requestID, fmt.Errorf("infra: read response: %w", err)
	}
	if len(data) > maxResponseBytes {
		return requestID, errors.New("infra: response exceeds 16 MiB")
	}
	var envelope struct {
		Data             json.RawMessage `json:"data"`
		Error            json.RawMessage `json:"error"`
		ErrorDescription string          `json:"error_description"`
		RequestID        string          `json:"request_id"`
	}
	decodeErr := json.Unmarshal(data, &envelope)
	if envelope.RequestID != "" {
		requestID = envelope.RequestID
	}
	hasError := len(envelope.Error) > 0 && !bytes.Equal(envelope.Error, []byte("null"))
	if res.StatusCode < 200 || res.StatusCode >= 300 || hasError {
		apiErr := &APIError{StatusCode: res.StatusCode, Code: "HTTP_ERROR", Message: http.StatusText(res.StatusCode), RequestID: requestID, RetryAfter: res.Header.Get("Retry-After")}
		if hasError {
			// Accept both the nested {"error":{"code":...,"message":...}} envelope
			// and RFC 6749's flat {"error":"invalid_client","error_description":...}.
			var nested struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			var flat string
			if err := json.Unmarshal(envelope.Error, &nested); err == nil && nested.Code != "" {
				apiErr.Code, apiErr.Message = nested.Code, nested.Message
			} else if err := json.Unmarshal(envelope.Error, &flat); err == nil && flat != "" {
				apiErr.Code, apiErr.Message = flat, envelope.ErrorDescription
			} else if nested.Message != "" {
				apiErr.Message = nested.Message
			}
		}
		// User refresh also returns UNAUTHORIZED for an invalid/reused User Token.
		if token != "" && res.StatusCode == http.StatusUnauthorized && (apiErr.Code == "TOKEN_EXPIRED" || apiErr.Code == "UNAUTHORIZED" && path != "/api/v1/identity/token/refresh") {
			c.invalidateToken(ctx, token)
		}
		return requestID, apiErr
	}
	if decodeErr != nil || len(envelope.Data) == 0 {
		return requestID, errors.New("infra: invalid response envelope")
	}
	if err := json.Unmarshal(envelope.Data, out); err != nil {
		return requestID, fmt.Errorf("infra: decode response data: %w", err)
	}
	return requestID, nil
}
