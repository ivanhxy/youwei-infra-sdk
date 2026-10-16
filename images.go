package infra

import (
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type ImageService struct{ client *Client }

type Image struct {
	ImageID         string     `json:"image_id"`
	ProductLineID   string     `json:"product_line_id"`
	ProductLineName string     `json:"product_line_name"`
	ClientID        string     `json:"client_id"`
	SceneCode       string     `json:"scene_code"`
	Status          string     `json:"status"`
	AccessType      string     `json:"access_type"`
	UploadedBy      string     `json:"uploaded_by"`
	OperatorID      string     `json:"operator_id,omitempty"`
	BizRefType      string     `json:"biz_ref_type,omitempty"`
	BizRefID        string     `json:"biz_ref_id,omitempty"`
	IdempotencyKey  string     `json:"idempotency_key,omitempty"`
	Width           int        `json:"width"`
	Height          int        `json:"height"`
	Format          string     `json:"format"`
	MIMEType        string     `json:"mime_type"`
	FileHash        string     `json:"file_hash"`
	OriginalName    string     `json:"original_filename"`
	ExpireAt        *time.Time `json:"expire_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type ImageAsset struct {
	AssetID   string    `json:"asset_id"`
	ImageID   string    `json:"image_id"`
	Variant   string    `json:"variant"`
	Bucket    string    `json:"bucket"`
	ObjectKey string    `json:"object_key"`
	CDNURL    string    `json:"cdn_url"`
	FileSize  int64     `json:"file_size"`
	Width     int       `json:"width"`
	Height    int       `json:"height"`
	Format    string    `json:"format"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type UploadImageRequest struct {
	SceneCode      string
	Filename       string
	File           io.Reader
	OperatorID     string
	BizRefType     string
	BizRefID       string
	IdempotencyKey string
}

type UploadedImage struct {
	Image        Image      `json:"image"`
	Asset        ImageAsset `json:"asset"`
	URL          string     `json:"url"`
	ThumbnailURL string     `json:"thumbnail_url,omitempty"`
}

type ImageDetail struct {
	Image  Image        `json:"image"`
	Assets []ImageAsset `json:"assets"`
}

type ListImagesRequest struct {
	SceneCode  string
	Status     string
	AccessType string
	Page       int
	PageSize   int
}

type ImageList struct {
	Images   []Image `json:"images"`
	Total    int64   `json:"total"`
	Page     int     `json:"page"`
	PageSize int     `json:"page_size"`
}

type CreateDirectUploadRequest struct {
	SceneCode        string `json:"scene_code"`
	OriginalFilename string `json:"original_filename"`
	FileSize         int64  `json:"file_size"`
	ContentType      string `json:"content_type"`
	OperatorID       string `json:"operator_id,omitempty"`
	BizRefType       string `json:"biz_ref_type,omitempty"`
	BizRefID         string `json:"biz_ref_id,omitempty"`
	IdempotencyKey   string `json:"idempotency_key,omitempty"`
}

type UploadInstruction struct {
	Mode          string            `json:"mode"`
	Method        string            `json:"method"`
	URL           string            `json:"url"`
	Headers       map[string]string `json:"headers,omitempty"`
	FormFields    map[string]string `json:"form_fields,omitempty"`
	FileFieldName string            `json:"file_field_name"`
}

type DirectUploadSession struct {
	UploadSessionID  string            `json:"upload_session_id"`
	Upload           UploadInstruction `json:"upload"`
	ExpiresAt        time.Time         `json:"expires_at"`
	ExpiresInSeconds int64             `json:"expires_in_seconds"`
	MaxFileSizeBytes int64             `json:"max_file_size_bytes"`
}

type DirectUploadStatus struct {
	UploadSessionID string      `json:"upload_session_id"`
	Status          string      `json:"status"`
	Image           *Image      `json:"image,omitempty"`
	Asset           *ImageAsset `json:"asset,omitempty"`
	URL             string      `json:"url,omitempty"`
	ThumbnailURL    string      `json:"thumbnail_url,omitempty"`
	ErrorMessage    string      `json:"error_message,omitempty"`
}

type ImageAccessURL struct {
	URL          string     `json:"url"`
	ThumbnailURL string     `json:"thumbnail_url,omitempty"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
}

type CreateVariantRequest struct {
	VariantType string `json:"variant_type"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
}

type ImageUsage struct {
	UploadCount         int64 `json:"upload_count"`
	StorageBytes        int64 `json:"storage_bytes"`
	ProcessCount        int64 `json:"process_count"`
	DeleteCount         int64 `json:"delete_count"`
	APIAccessCount      int64 `json:"api_access_count"`
	FailedCallCount     int64 `json:"failed_call_count"`
	PendingReleaseBytes int64 `json:"pending_release_bytes"`
}

type ImageCallRecord struct {
	RequestID     string    `json:"request_id"`
	ProductLineID string    `json:"product_line_id,omitempty"`
	ClientID      string    `json:"client_id,omitempty"`
	SceneCode     string    `json:"scene_code,omitempty"`
	Operation     string    `json:"operation"`
	ImageID       string    `json:"image_id,omitempty"`
	Result        string    `json:"result"`
	ErrorCode     string    `json:"error_code,omitempty"`
	ErrorMessage  string    `json:"error_message,omitempty"`
	LatencyMS     int       `json:"latency_ms"`
	IP            string    `json:"ip,omitempty"`
	UserAgent     string    `json:"user_agent,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// Upload streams a multipart request without buffering the whole file.
// The caller owns File; the SDK never closes or retries it.
func (s *ImageService) Upload(ctx context.Context, input UploadImageRequest) (*Response[UploadedImage], error) {
	if input.File == nil || input.Filename == "" {
		return nil, errors.New("infra: File and Filename are required")
	}
	token, err := s.client.accessToken(ctx)
	if err != nil {
		return nil, err
	}
	reader, writer := io.Pipe()
	defer reader.Close()
	form := multipart.NewWriter(writer)
	contentType := form.FormDataContentType()
	go func() {
		err := writeImageForm(form, input)
		if err == nil {
			err = form.Close()
		}
		_ = writer.CloseWithError(err)
	}()
	result := new(Response[UploadedImage])
	result.RequestID, err = s.client.request(ctx, http.MethodPost, "/api/v1/images", nil, reader, contentType, token, &result.Data)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func writeImageForm(form *multipart.Writer, req UploadImageRequest) error {
	for key, value := range map[string]string{
		"scene_code": req.SceneCode, "operator_id": req.OperatorID,
		"biz_ref_type": req.BizRefType, "biz_ref_id": req.BizRefID, "idempotency_key": req.IdempotencyKey,
	} {
		if err := form.WriteField(key, value); err != nil {
			return err
		}
	}
	part, err := form.CreateFormFile("file", req.Filename)
	if err != nil {
		return err
	}
	_, err = io.Copy(part, req.File)
	return err
}

func (s *ImageService) CreateDirectUploadSession(ctx context.Context, req CreateDirectUploadRequest) (*Response[DirectUploadSession], error) {
	return call[DirectUploadSession](ctx, s.client, http.MethodPost, "/api/v1/images/direct-upload-sessions", nil, req)
}

func (s *ImageService) GetDirectUploadSession(ctx context.Context, sessionID string) (*Response[DirectUploadStatus], error) {
	return resource[DirectUploadStatus](ctx, s.client, http.MethodGet, "/api/v1/images/direct-upload-sessions", sessionID, "", nil, nil)
}

// ReconcileDirectUploadSession is a fallback for missing callbacks, not a normal completion step.
func (s *ImageService) ReconcileDirectUploadSession(ctx context.Context, sessionID string) (*Response[DirectUploadStatus], error) {
	return resource[DirectUploadStatus](ctx, s.client, http.MethodPost, "/api/v1/images/direct-upload-sessions", sessionID, "/reconcile", nil, struct{}{})
}

func (s *ImageService) List(ctx context.Context, req ListImagesRequest) (*Response[ImageList], error) {
	query := url.Values{}
	for key, value := range map[string]string{"scene_code": req.SceneCode, "status": req.Status, "access_type": req.AccessType} {
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
	return call[ImageList](ctx, s.client, http.MethodGet, "/api/v1/images", query, nil)
}

func (s *ImageService) Get(ctx context.Context, imageID string) (*Response[ImageDetail], error) {
	return resource[ImageDetail](ctx, s.client, http.MethodGet, "/api/v1/images", imageID, "", nil, nil)
}

func (s *ImageService) CreateAccessURL(ctx context.Context, imageID string) (*Response[ImageAccessURL], error) {
	return resource[ImageAccessURL](ctx, s.client, http.MethodPost, "/api/v1/images", imageID, "/access-url", nil, struct{}{})
}

func (s *ImageService) CreateVariant(ctx context.Context, imageID string, req CreateVariantRequest) (*Response[ImageAsset], error) {
	return resource[ImageAsset](ctx, s.client, http.MethodPost, "/api/v1/images", imageID, "/variants", nil, req)
}

func (s *ImageService) Delete(ctx context.Context, imageID string) (*Response[Image], error) {
	return resource[Image](ctx, s.client, http.MethodDelete, "/api/v1/images", imageID, "", nil, nil)
}

func (s *ImageService) Usage(ctx context.Context) (*Response[ImageUsage], error) {
	return call[ImageUsage](ctx, s.client, http.MethodGet, "/api/v1/image-usage", nil, nil)
}

func (s *ImageService) CallRecords(ctx context.Context) (*Response[[]ImageCallRecord], error) {
	return call[[]ImageCallRecord](ctx, s.client, http.MethodGet, "/api/v1/image-call-records", nil, nil)
}
