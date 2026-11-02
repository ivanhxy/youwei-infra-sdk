//go:build acceptance

// Package acceptance runs the file-storage control-plane acceptance suite
// against a real infra-center environment. It only exercises behavior through
// the SDK; findings are reported, never patched here.
//
// Run:
//
//	INFRA_BASE_URL=... INFRA_CLIENT_ID=... INFRA_CLIENT_SECRET=... \
//	INFRA_FILE_SCENE=archive go test -tags acceptance -run TestFileServiceAcceptance -v ./acceptance/
package acceptance

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"testing"
	"time"

	infra "github.com/ivanhxy/youwei-infra-sdk"
)

// idPattern matches the center's actual file_id format: file_<32hex>.
// UUID kept as an accepted alternative for forward compatibility.
var idPattern = regexp.MustCompile(`^(file_[0-9a-fA-F]{32}|[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})$`)

type env struct {
	baseURL    string
	clientID   string
	secret     string
	scene      string
	runID      string
	hasEnvData bool

	fileExt     string
	contentType string
}

func loadEnv(t *testing.T) env {
	t.Helper()
	e := env{
		baseURL:  os.Getenv("INFRA_BASE_URL"),
		clientID: os.Getenv("INFRA_CLIENT_ID"),
		secret:   os.Getenv("INFRA_CLIENT_SECRET"),
		scene:    os.Getenv("INFRA_FILE_SCENE"),
	}
	e.hasEnvData = e.baseURL != "" && e.clientID != "" && e.secret != "" && e.scene != ""
	if !e.hasEnvData {
		t.Skip("acceptance env missing: INFRA_BASE_URL / INFRA_CLIENT_ID / INFRA_CLIENT_SECRET / INFRA_FILE_SCENE")
	}
	e.runID = "sdk-acc-" + time.Now().Format("20060102150405")
	// Probe extension/MIME must match the scene's allowed_formats whitelist
	// (P-02 from the 2026-11-02 acceptance run); defaults suit the archive scene.
	e.fileExt = cmp.Or(os.Getenv("INFRA_FILE_EXT"), "zip")
	e.contentType = cmp.Or(os.Getenv("INFRA_FILE_CONTENT_TYPE"), "application/zip")
	return e
}

func mask(s string) string {
	if len(s) <= 4 {
		return "***"
	}
	return s[:2] + "***" + s[len(s)-2:]
}

func apiErr(t *testing.T, err error) *infra.APIError {
	t.Helper()
	var ae *infra.APIError
	if !errors.As(err, &ae) {
		t.Fatalf("expected APIError, got: %v", err)
	}
	return ae
}

func expectCode(ae *infra.APIError, statuses []int, codes []string, note string) string {
	got := fmt.Sprintf("HTTP %d %s", ae.StatusCode, ae.Code)
	for _, s := range statuses {
		if ae.StatusCode == s {
			for _, c := range codes {
				if ae.Code == c {
					return got
				}
			}
			return got + " (record: unexpected code)"
		}
	}
	return "UNEXPECTED " + got + " — " + note
}

// TestFileServiceAcceptance runs TC-01..TC-17 in order; later cases reuse the
// file created in TC-05, so the sequence is intentionally stateful.
func TestFileServiceAcceptance(t *testing.T) {
	e := loadEnv(t)
	client, err := infra.NewClient(infra.Config{
		BaseURL: e.baseURL, ClientID: e.clientID, ClientSecret: e.secret,
	})
	if err != nil {
		t.Fatalf("TC-00 new client: %v", err)
	}
	ctx := context.Background()

	// TC-01 Client Token and connectivity.
	t.Run("TC-01_client_token_and_list", func(t *testing.T) {
		out, err := client.Files.List(ctx, infra.ListFilesRequest{Page: 1, PageSize: 1})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if out.RequestID == "" {
			t.Error("missing request_id")
		}
		t.Logf("PASS total=%d page=%d size=%d", out.Data.Total, out.Data.Page, out.Data.PageSize)
	})

	// TC-02 unauthenticated access is rejected.
	t.Run("TC-02_unauthorized_without_token", func(t *testing.T) {
		res, err := http.Get(e.baseURL + "/api/v1/files")
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer res.Body.Close()
		_, _ = io.Copy(io.Discard, res.Body)
		if res.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401 without credentials, got %d", res.StatusCode)
		}
		t.Logf("PASS HTTP %d", res.StatusCode)
	})

	// TC-03 unknown scene must not issue credentials.
	t.Run("TC-03_unknown_scene", func(t *testing.T) {
		_, err := client.Files.AuthorizeUpload(ctx, infra.FileUploadAuthorizationRequest{
			SceneCode: "sdk-acc-not-exist-" + e.runID,
			OriginalFilename: "probe." + e.fileExt, FileSize: 1, ContentType: e.contentType,
		})
		if err == nil {
			t.Fatal("unknown scene issued an upload authorization")
		}
		ae := apiErr(t, err)
		t.Logf("%s message=%q", expectCode(ae, []int{400, 403, 404},
			[]string{"INVALID_INPUT", "FORBIDDEN", "SCENE_UNAUTHORIZED", "NOT_FOUND"}, "scene must not authorize"), ae.Message)
	})

	// TC-04 disabled scene (optional, provided by the operator).
	if disabled := os.Getenv("INFRA_FILE_DISABLED_SCENE"); disabled != "" {
		t.Run("TC-04_disabled_scene", func(t *testing.T) {
			_, err := client.Files.AuthorizeUpload(ctx, infra.FileUploadAuthorizationRequest{
				SceneCode: disabled, OriginalFilename: "probe." + e.fileExt, FileSize: 1, ContentType: e.contentType,
			})
			if err == nil {
				t.Fatal("disabled scene issued an upload authorization")
			}
			ae := apiErr(t, err)
			t.Logf("%s message=%q", expectCode(ae, []int{400, 403, 409},
				[]string{"INVALID_INPUT", "FORBIDDEN", "SCENE_DISABLED", "CONFLICT"}, "disabled scene must reject"), ae.Message)
		})
	} else {
		t.Log("TC-04 SKIPPED: INFRA_FILE_DISABLED_SCENE not provided")
	}

	// TC-05 authorize upload and validate the full contract.
	var auth *infra.Response[infra.FileUploadAuthorization]
	fileMeta := infra.FileUploadAuthorizationRequest{
		SceneCode:        e.scene,
		OriginalFilename: "sdk-acceptance-" + e.runID + "." + e.fileExt,
		FileSize:         1024,
		ContentType:      e.contentType,
		BizRefType:       "acceptance",
		BizRefID:         e.runID,
		IdempotencyKey:   e.runID,
	}
	t.Run("TC-05_authorize_upload", func(t *testing.T) {
		out, err := client.Files.AuthorizeUpload(ctx, fileMeta)
		if err != nil {
			t.Fatalf("authorize: %v", err)
		}
		auth = out
		d := out.Data
		if !idPattern.MatchString(d.FileID) {
			t.Errorf("file_id format invalid: %q", d.FileID)
		}
		if d.Provider != "aliyun_oss" {
			t.Errorf("provider = %q, want aliyun_oss", d.Provider)
		}
		for name, value := range map[string]string{
			"region": d.Region, "endpoint": d.Endpoint, "bucket": d.Bucket, "object_key": d.ObjectKey,
		} {
			if value == "" {
				t.Errorf("%s is empty", name)
			}
		}
		cred := d.Credentials
		for name, value := range map[string]string{
			"access_key_id": cred.AccessKeyID, "access_key_secret": cred.AccessKeySecret, "security_token": cred.SecurityToken,
		} {
			if value == "" {
				t.Errorf("credentials.%s is empty", name)
			}
		}
		if cred.ExpiresAt.IsZero() || !cred.ExpiresAt.After(time.Now()) {
			t.Errorf("credentials.expired_at not in the future: %v", cred.ExpiresAt)
		}
		if d.Callback.URL == "" || d.Callback.Body == "" || d.Callback.ContentType == "" {
			t.Errorf("callback incomplete: %+v", d.Callback)
		}
		if d.MaxFileSizeBytes <= 0 || d.PartSizeBytes <= 0 || d.Parallel <= 0 {
			t.Errorf("upload parameters non-positive: max=%d part=%d parallel=%d", d.MaxFileSizeBytes, d.PartSizeBytes, d.Parallel)
		}
		if d.UploadExpiresAt.IsZero() || !d.UploadExpiresAt.After(time.Now()) {
			t.Errorf("upload_expires_at not in the future: %v", d.UploadExpiresAt)
		}
		t.Logf("PASS file_id=%s cred=%s/%s/%s expires=%s", d.FileID,
			mask(cred.AccessKeyID), mask(cred.AccessKeySecret), mask(cred.SecurityToken),
			cred.ExpiresAt.Format(time.RFC3339))
	})
	if auth == nil {
		t.Fatal("TC-05 failed; stateful cases TC-06..TC-17 cannot run")
	}

	// TC-06 idempotent replay with the same metadata.
	t.Run("TC-06_idempotent_replay", func(t *testing.T) {
		out, err := client.Files.AuthorizeUpload(ctx, fileMeta)
		if err != nil {
			t.Fatalf("replay: %v", err)
		}
		if out.Data.FileID != auth.Data.FileID || out.Data.ObjectKey != auth.Data.ObjectKey {
			t.Errorf("replay created a new target: file_id %s -> %s", auth.Data.FileID, out.Data.FileID)
		}
		t.Logf("PASS same file_id=%s", out.Data.FileID)
	})

	// TC-07 idempotency conflict with different metadata.
	t.Run("TC-07_idempotency_conflict", func(t *testing.T) {
		conflict := fileMeta
		conflict.FileSize = fileMeta.FileSize + 1
		_, err := client.Files.AuthorizeUpload(ctx, conflict)
		if err == nil {
			t.Fatal("conflicting metadata accepted for the same idempotency key")
		}
		ae := apiErr(t, err)
		t.Logf("%s message=%q", expectCode(ae, []int{400, 409},
			[]string{"INVALID_INPUT", "CONFLICT"}, "same key must not create a new object"), ae.Message)
	})

	// TC-08 status and metadata consistency.
	t.Run("TC-08_get_metadata", func(t *testing.T) {
		out, err := client.Files.Get(ctx, auth.Data.FileID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		f := out.Data
		if f.Status != infra.FileStatusUploading {
			t.Errorf("status = %q, want UPLOADING", f.Status)
		}
		if f.SceneCode != fileMeta.SceneCode || f.OriginalFilename != fileMeta.OriginalFilename ||
			f.ContentType != fileMeta.ContentType || f.DeclaredSizeBytes != fileMeta.FileSize {
			t.Errorf("metadata mismatch: %+v", f)
		}
		if f.CreatedAt.IsZero() {
			t.Error("created_at is zero")
		}
		t.Logf("PASS status=%s scene=%s size=%d", f.Status, f.SceneCode, f.DeclaredSizeBytes)
	})

	// TC-09 list contains the file.
	t.Run("TC-09_list_filter", func(t *testing.T) {
		out, err := client.Files.List(ctx, infra.ListFilesRequest{
			SceneCode: e.scene, Status: infra.FileStatusUploading, Page: 1, PageSize: 20,
		})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		found := false
		for _, f := range out.Data.Items {
			if f.FileID == auth.Data.FileID {
				found = true
			}
		}
		if !found {
			t.Errorf("file %s not in page 1 (total=%d)", auth.Data.FileID, out.Data.Total)
		}
		t.Logf("PASS total=%d items=%d", out.Data.Total, len(out.Data.Items))
	})

	// TC-10 reconcile without an actual OSS object must not fake success.
	t.Run("TC-10_reconcile_no_object", func(t *testing.T) {
		out, err := client.Files.Reconcile(ctx, auth.Data.FileID)
		if err == nil {
			if out.Data.Status == infra.FileStatusActive {
				t.Error("reconcile faked ACTIVE without an uploaded object")
			}
			if out.Data.ReconcileResult != infra.FileReconcileObjectNotFound {
				t.Errorf("reconcile_result=%q, want %s", out.Data.ReconcileResult, infra.FileReconcileObjectNotFound)
			}
			t.Logf("PASS status=%s result=%s", out.Data.Status, out.Data.ReconcileResult)
			return
		}
		ae := apiErr(t, err)
		t.Logf("RECORD reconcile returned %s message=%q (accepted if the center explains the missing object)", expectCode(ae, []int{400, 404, 409},
			[]string{"INVALID_INPUT", "NOT_FOUND", "CONFLICT", "RECONCILE_NOT_READY", "UPLOAD_NOT_FINISHED"}, "explainable error"), ae.Message)
	})

	// TC-11 access URL for a non-ACTIVE file (record actual policy).
	t.Run("TC-11_access_url_non_active", func(t *testing.T) {
		_, err := client.Files.CreateAccessURL(ctx, auth.Data.FileID)
		if err == nil {
			t.Error("non-ACTIVE file got a download URL (record as issue)")
			return
		}
		ae := apiErr(t, err)
		t.Logf("%s message=%q", expectCode(ae, []int{400, 403, 404, 409},
			[]string{"INVALID_INPUT", "FORBIDDEN", "NOT_FOUND", "CONFLICT", "FILE_NOT_ACTIVE"}, "must refuse before ACTIVE"), ae.Message)
	})

	// TC-12 renew credentials keeps the same upload target.
	t.Run("TC-12_renew_credentials", func(t *testing.T) {
		out, err := client.Files.RenewUploadCredentials(ctx, auth.Data.FileID)
		if err != nil {
			t.Fatalf("renew: %v", err)
		}
		d := out.Data
		if d.FileID != auth.Data.FileID || d.ObjectKey != auth.Data.ObjectKey || d.Bucket != auth.Data.Bucket {
			t.Errorf("renew changed the upload target: %+v", d)
		}
		if d.Credentials.AccessKeyID == "" || d.Credentials.ExpiresAt.IsZero() || !d.Credentials.ExpiresAt.After(time.Now()) {
			t.Errorf("renewed credentials invalid: expires_at=%v", d.Credentials.ExpiresAt)
		}
		t.Logf("PASS same target, new cred expires=%s", d.Credentials.ExpiresAt.Format(time.RFC3339))
	})

	// TC-13 invalid file_id is rejected client-side.
	t.Run("TC-13_invalid_file_id", func(t *testing.T) {
		for _, id := range []string{"", "..", "../x", "a?b", "a/b"} {
			if _, err := client.Files.Get(ctx, id); err == nil {
				t.Errorf("accepted file_id %q", id)
			}
		}
		t.Log("PASS all invalid ids rejected client-side")
	})

	// TC-14 unknown but well-formed file_id.
	t.Run("TC-14_unknown_file_id", func(t *testing.T) {
		_, err := client.Files.Get(ctx, "00000000-0000-4000-8000-000000000000")
		if err == nil {
			t.Fatal("unknown file_id returned a result")
		}
		ae := apiErr(t, err)
		t.Logf("%s message=%q", expectCode(ae, []int{403, 404},
			[]string{"NOT_FOUND", "FORBIDDEN"}, "unknown id must 404 (403 for cross-client is acceptable)"), ae.Message)
	})

	// TC-15 delete, verify status, repeat delete.
	t.Run("TC-15_delete_and_repeat", func(t *testing.T) {
		out, err := client.Files.Delete(ctx, auth.Data.FileID)
		if err != nil {
			t.Fatalf("delete: %v", err)
		}
		if out.Data.Status != infra.FileStatusDeleting && out.Data.Status != infra.FileStatusDeleted {
			t.Logf("RECORD delete returned status %q", out.Data.Status)
		}
		got, err := client.Files.Get(ctx, auth.Data.FileID)
		if err != nil {
			ae := apiErr(t, err)
			t.Logf("RECORD get after delete: %s", expectCode(ae, []int{403, 404}, []string{"NOT_FOUND", "FORBIDDEN"}, "terminal or hidden"))
		} else if got.Data.Status != infra.FileStatusDeleting && got.Data.Status != infra.FileStatusDeleted {
			t.Errorf("status after delete = %q", got.Data.Status)
		}
		if _, err := client.Files.Delete(ctx, auth.Data.FileID); err != nil {
			ae := apiErr(t, err)
			t.Logf("RECORD repeat delete: %s message=%q (idempotent success or explicit terminal error both accepted)", expectCode(ae, []int{200, 403, 404, 409},
				[]string{"", "NOT_FOUND", "FORBIDDEN", "CONFLICT"}, ""), ae.Message)
		} else {
			t.Log("PASS repeat delete idempotent")
		}
	})

	// TC-16 renewal after delete must fail.
	t.Run("TC-16_renew_after_delete", func(t *testing.T) {
		_, err := client.Files.RenewUploadCredentials(ctx, auth.Data.FileID)
		if err == nil {
			t.Fatal("renewal succeeded after delete")
		}
		ae := apiErr(t, err)
		t.Logf("%s message=%q", expectCode(ae, []int{400, 403, 404, 409},
			[]string{"INVALID_INPUT", "FORBIDDEN", "NOT_FOUND", "CONFLICT"}, "must refuse renewal"), ae.Message)
	})

	// TC-17 access URL after delete must fail.
	t.Run("TC-17_access_url_after_delete", func(t *testing.T) {
		_, err := client.Files.CreateAccessURL(ctx, auth.Data.FileID)
		if err == nil {
			t.Fatal("download URL issued after delete")
		}
		ae := apiErr(t, err)
		t.Logf("%s message=%q", expectCode(ae, []int{400, 403, 404, 409},
			[]string{"INVALID_INPUT", "FORBIDDEN", "NOT_FOUND", "CONFLICT"}, "must refuse download"), ae.Message)
	})

	t.Logf("RUN complete: idempotency key=%s scene=%s file_id=%s (TC-18 real OSS upload is manual)", e.runID, e.scene, auth.Data.FileID)
}
