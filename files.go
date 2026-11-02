package infra

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// FilesService wraps the STS direct-upload control plane. File bodies never
// travel through the center; terminal clients upload with the official OSS SDK
// using the credentials and parameters returned here.
type FilesService struct{ client *Client }

// File statuses reported by the center. Only FileStatusActive marks a file
// usable as a business attachment; an OSS upload success is not.
const (
	FileStatusUploading = "UPLOADING"
	FileStatusActive    = "ACTIVE"
	FileStatusFailed    = "FAILED"
	FileStatusExpired   = "EXPIRED"
	FileStatusDeleting  = "DELETING"
	FileStatusDeleted   = "DELETED"
)

type FileUploadAuthorizationRequest struct {
	SceneCode        string `json:"scene_code"`
	OriginalFilename string `json:"original_filename"`
	FileSize         int64  `json:"file_size"`
	ContentType      string `json:"content_type"`
	OperatorID       string `json:"operator_id,omitempty"`
	BizRefType       string `json:"biz_ref_type,omitempty"`
	BizRefID         string `json:"biz_ref_id,omitempty"`
	IdempotencyKey   string `json:"idempotency_key,omitempty"`
}

type FileUploadCredentials struct {
	AccessKeyID     string    `json:"access_key_id"`
	AccessKeySecret string    `json:"access_key_secret"`
	SecurityToken   string    `json:"security_token"`
	ExpiresAt       time.Time `json:"expires_at"`
}

type FileCallback struct {
	URL         string `json:"url"`
	Body        string `json:"body"`
	ContentType string `json:"contentType"`
}

type FileUploadAuthorization struct {
	FileID           string                `json:"file_id"`
	Provider         string                `json:"provider"`
	Region           string                `json:"region"`
	Endpoint         string                `json:"endpoint"`
	Bucket           string                `json:"bucket"`
	ObjectKey        string                `json:"object_key"`
	Credentials      FileUploadCredentials `json:"credentials"`
	Callback         FileCallback          `json:"callback"`
	MaxFileSizeBytes int64                 `json:"max_file_size_bytes"`
	UploadExpiresAt  time.Time             `json:"upload_expires_at"`
	PartSizeBytes    int64                 `json:"part_size_bytes"`
	Parallel         int                   `json:"parallel"`
}

// Reconcile 核验结果枚举：仅 Reconcile 响应的 data.reconcile_result 携带，
// 其余端点（Get/List/Delete）不返回该字段。
// OBJECT_CONFIRMED：对象核验通过（含已 ACTIVE 的幂等重放）；
// OBJECT_NOT_FOUND：云端对象尚不存在（状态保持 UPLOADING，可稍后重试）；
// MISMATCH_FAILED：对象存在但大小/类型/版本不符合授权（状态置 FAILED）。
const (
	FileReconcileObjectConfirmed = "OBJECT_CONFIRMED"
	FileReconcileObjectNotFound  = "OBJECT_NOT_FOUND"
	FileReconcileMismatchFailed  = "MISMATCH_FAILED"
)

type File struct {
	FileID            string     `json:"file_id"`
	ClientID          string     `json:"client_id"`
	ProductLineID     string     `json:"product_line_id"`
	SceneCode         string     `json:"scene_code"`
	OriginalFilename  string     `json:"original_filename"`
	ContentType       string     `json:"content_type"`
	DeclaredSizeBytes int64      `json:"declared_size_bytes"`
	FileSizeBytes     int64      `json:"file_size_bytes"`
	Status            string     `json:"status"`
	StorageTargetID   int64      `json:"storage_target_id"`
	ObjectKey         string     `json:"object_key"`
	VersionID         string     `json:"version_id,omitempty"`
	ETag              string     `json:"etag,omitempty"`
	OperatorID        string     `json:"operator_id,omitempty"`
	BizRefType        string     `json:"biz_ref_type,omitempty"`
	BizRefID          string     `json:"biz_ref_id,omitempty"`
	ErrorMessage      string     `json:"error_message,omitempty"`
	ReconcileResult   string     `json:"reconcile_result,omitempty"`
	UploadExpiresAt   time.Time  `json:"upload_expires_at"`
	ExpireAt          *time.Time `json:"expire_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type FileAccessURL struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

type ListFilesRequest struct {
	Keyword   string
	SceneCode string
	Status    string
	Page      int
	PageSize  int
}

type FileList struct {
	Items    []File `json:"items"`
	Total    int64  `json:"total"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}

// AuthorizeUpload creates an upload intent and returns scoped STS credentials
// plus official OSS SDK parameters. Credentials are short-lived; never cache
// or log them. The idempotency key is per Client: replays must repeat the same
// metadata, and deleted or terminal files cannot be recreated with an old key.
func (s *FilesService) AuthorizeUpload(ctx context.Context, req FileUploadAuthorizationRequest) (*Response[FileUploadAuthorization], error) {
	return call[FileUploadAuthorization](ctx, s.client, http.MethodPost, "/api/v1/files/upload-authorizations", nil, req)
}

// RenewUploadCredentials re-issues STS credentials for the same upload target
// when resuming a multipart upload before it expires.
func (s *FilesService) RenewUploadCredentials(ctx context.Context, fileID string) (*Response[FileUploadAuthorization], error) {
	return resource[FileUploadAuthorization](ctx, s.client, http.MethodPost, "/api/v1/files", fileID, "/upload-credentials", nil, struct{}{})
}

// Reconcile asks the center to Head-verify the cloud object, compensating for
// a lost or failed callback. Call this before re-uploading a whole file.
func (s *FilesService) Reconcile(ctx context.Context, fileID string) (*Response[File], error) {
	return resource[File](ctx, s.client, http.MethodPost, "/api/v1/files", fileID, "/reconcile", nil, struct{}{})
}

func (s *FilesService) Get(ctx context.Context, fileID string) (*Response[File], error) {
	return resource[File](ctx, s.client, http.MethodGet, "/api/v1/files", fileID, "", nil, nil)
}

func (s *FilesService) List(ctx context.Context, req ListFilesRequest) (*Response[FileList], error) {
	query := url.Values{}
	for key, value := range map[string]string{"keyword": req.Keyword, "scene_code": req.SceneCode, "status": req.Status} {
		if value != "" {
			query.Set(key, value)
		}
	}
	if req.Page != 0 {
		query.Set("page", strconv.Itoa(req.Page))
	}
	if req.PageSize != 0 {
		query.Set("page_size", strconv.Itoa(req.PageSize))
	}
	return call[FileList](ctx, s.client, http.MethodGet, "/api/v1/files", query, nil)
}

// CreateAccessURL issues a short-lived download URL bound to the confirmed
// object version. It is never a permanent address and cannot revoke URLs
// already issued.
func (s *FilesService) CreateAccessURL(ctx context.Context, fileID string) (*Response[FileAccessURL], error) {
	return resource[FileAccessURL](ctx, s.client, http.MethodPost, "/api/v1/files", fileID, "/access-urls", nil, struct{}{})
}

// Delete blocks new downloads and credential renewals and schedules delayed
// cleanup; repeated deletes are idempotent.
func (s *FilesService) Delete(ctx context.Context, fileID string) (*Response[File], error) {
	return resource[File](ctx, s.client, http.MethodDelete, "/api/v1/files", fileID, "", nil, nil)
}
