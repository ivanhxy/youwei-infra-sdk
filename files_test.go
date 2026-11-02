package infra

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestFilesAuthorizeUploadPreservesContract(t *testing.T) {
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/files/upload-authorizations" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["scene_code"] != "archive" || body["original_filename"] != "a.zip" ||
			body["file_size"] != float64(536870912) || body["content_type"] != "application/zip" ||
			body["operator_id"] != "user-1" || body["biz_ref_type"] != "order" ||
			body["biz_ref_id"] != "1001" || body["idempotency_key"] != "order-1001-attachment-1" {
			t.Errorf("request body mismatch: %v", body)
		}
		_, _ = io.WriteString(w, `{"data":{
			"file_id":"0198a7d1-6b7c-7de0-a1e5-3d6cf2e1a001",
			"provider":"aliyun_oss","region":"oss-cn-hangzhou","endpoint":"https://oss-cn-hangzhou.aliyuncs.com",
			"bucket":"files-bucket","object_key":"files/client/archive/unique-key",
			"credentials":{"access_key_id":"sts-id","access_key_secret":"sts-secret","security_token":"sts-token","expires_at":"2026-11-02T12:15:00Z"},
			"callback":{"url":"https://center/api/v1/file-storage-callbacks/1/fid","body":"{\"file_id\":\"fid\"}","contentType":"application/json"},
			"max_file_size_bytes":1073741824,"upload_expires_at":"2026-11-02T18:00:00Z",
			"part_size_bytes":8388608,"parallel":3
		},"request_id":"file-1"}`)
	})
	auth, err := c.Files.AuthorizeUpload(context.Background(), FileUploadAuthorizationRequest{
		SceneCode: "archive", OriginalFilename: "a.zip", FileSize: 536870912, ContentType: "application/zip",
		OperatorID: "user-1", BizRefType: "order", BizRefID: "1001", IdempotencyKey: "order-1001-attachment-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	data := auth.Data
	if data.FileID == "" || data.Provider != "aliyun_oss" || data.Region != "oss-cn-hangzhou" ||
		data.Endpoint != "https://oss-cn-hangzhou.aliyuncs.com" || data.Bucket != "files-bucket" ||
		data.ObjectKey != "files/client/archive/unique-key" || data.Credentials.AccessKeyID != "sts-id" ||
		data.Credentials.AccessKeySecret != "sts-secret" || data.Credentials.SecurityToken != "sts-token" ||
		data.Credentials.ExpiresAt.IsZero() || data.Callback.URL == "" || data.Callback.Body == "" ||
		data.Callback.ContentType != "application/json" || data.MaxFileSizeBytes != 1073741824 ||
		data.PartSizeBytes != 8388608 || data.Parallel != 3 || auth.RequestID != "file-1" {
		t.Fatalf("authorization data lost: %+v", data)
	}
	if expires, _ := time.Parse(time.RFC3339, "2026-11-02T18:00:00Z"); !data.UploadExpiresAt.Equal(expires.UTC()) {
		t.Fatalf("upload expiry lost: %v", data.UploadExpiresAt)
	}
}

func TestFilesResourceRoutesAndEmptyBodies(t *testing.T) {
	const id = "0198a7d1-6b7c-7de0-a1e5-3d6cf2e1a002"
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/files/"+id &&
			r.URL.Path != "/api/v1/files/"+id+"/reconcile" &&
			r.URL.Path != "/api/v1/files/"+id+"/upload-credentials" &&
			r.URL.Path != "/api/v1/files/"+id+"/access-urls" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		switch r.Method {
		case http.MethodGet:
		case http.MethodDelete:
		case http.MethodPost:
			switch r.URL.Path {
			case "/api/v1/files/" + id + "/reconcile", "/api/v1/files/" + id + "/upload-credentials", "/api/v1/files/" + id + "/access-urls":
				if len(body) != 0 && string(body) != "{}" {
					t.Errorf("unexpected body on %s: %s", r.URL.Path, body)
				}
			}
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
		_, _ = io.WriteString(w, `{"data":{"file_id":"`+id+`","status":"UPLOADING"},"request_id":"file-2"}`)
	})
	ctx := context.Background()
	if _, err := c.Files.Get(ctx, id); err != nil {
		t.Errorf("get: %v", err)
	}
	if _, err := c.Files.Reconcile(ctx, id); err != nil {
		t.Errorf("reconcile: %v", err)
	}
	if _, err := c.Files.RenewUploadCredentials(ctx, id); err != nil {
		t.Errorf("renew credentials: %v", err)
	}
	if _, err := c.Files.CreateAccessURL(ctx, id); err != nil {
		t.Errorf("access url: %v", err)
	}
	if _, err := c.Files.Delete(ctx, id); err != nil {
		t.Errorf("delete: %v", err)
	}
}

func TestFilesListFilters(t *testing.T) {
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("keyword") != "a & b" || q.Get("scene_code") != "archive" || q.Get("status") != "ACTIVE" || q.Get("page") != "2" || q.Get("page_size") != "5" {
			t.Errorf("filter missing: %v", q)
		}
		_, _ = io.WriteString(w, `{"data":{"items":[{"file_id":"file-1","status":"UPLOADING"}],"total":6,"page":2,"page_size":5},"request_id":"file-3"}`)
	})
	list, err := c.Files.List(context.Background(), ListFilesRequest{Keyword: "a & b", SceneCode: "archive", Status: FileStatusActive, Page: 2, PageSize: 5})
	if err != nil || list.Data.Total != 6 || list.Data.Page != 2 || list.Data.PageSize != 5 || len(list.Data.Items) != 1 || list.Data.Items[0].Status != FileStatusUploading {
		t.Fatalf("list: %v", err)
	}
}

func TestFilesRejectsInvalidFileID(t *testing.T) {
	c, _ := testClient(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected API call") })
	for _, id := range []string{"", "..", ".", "../x", "a?b", "a#b", "a%2fb", "a\\b"} {
		if _, err := c.Files.Get(context.Background(), id); err == nil {
			t.Errorf("accepted resource ID %q", id)
		}
	}
}
