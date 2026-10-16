package infra

import (
	"context"
	"net/http"
	"time"
)

type SMSService struct{ client *Client }

type SendVerificationRequest struct {
	SceneCode      string            `json:"scene_code"`
	Phone          string            `json:"phone"`
	BusinessKey    string            `json:"business_key"`
	TemplateParams map[string]string `json:"template_params,omitempty"`
}

type SentVerification struct {
	MessageID string `json:"message_id"`
	ExpiresIn int    `json:"expires_in"`
}

type VerifyCodeRequest struct {
	SceneCode   string `json:"scene_code"`
	Phone       string `json:"phone"`
	BusinessKey string `json:"business_key"`
	Code        string `json:"code"`
}

type VerificationResult struct {
	Verified bool `json:"verified"`
}

type SendNotificationRequest struct {
	SceneCode      string            `json:"scene_code"`
	Phone          string            `json:"phone"`
	TemplateParams map[string]string `json:"template_params,omitempty"`
	IdempotencyKey string            `json:"idempotency_key,omitempty"`
}

type SentNotification struct {
	MessageID string `json:"message_id"`
	Status    string `json:"status"`
}

type SMSMessage struct {
	MessageID           string     `json:"message_id"`
	AppID               string     `json:"app_id"`
	SceneID             int64      `json:"scene_id"`
	SceneCode           string     `json:"scene_code,omitempty"`
	ProviderConfigID    int64      `json:"provider_config_id"`
	TemplateID          int64      `json:"template_id"`
	PhoneMasked         string     `json:"phone_masked"`
	IdempotencyKey      string     `json:"idempotency_key,omitempty"`
	IdempotencyBucket   string     `json:"idempotency_bucket,omitempty"`
	OutID               string     `json:"out_id"`
	ProviderBizID       string     `json:"provider_biz_id"`
	MessageType         string     `json:"message_type"`
	Status              string     `json:"status"`
	SubmitErrorCode     string     `json:"submit_error_code,omitempty"`
	SubmitErrorMessage  string     `json:"submit_error_message,omitempty"`
	ReceiptErrorCode    string     `json:"receipt_error_code,omitempty"`
	ReceiptErrorMessage string     `json:"receipt_error_message,omitempty"`
	SubmittedAt         *time.Time `json:"submitted_at,omitempty"`
	DeliveredAt         *time.Time `json:"delivered_at,omitempty"`
	FailedAt            *time.Time `json:"failed_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

func (s *SMSService) SendVerification(ctx context.Context, req SendVerificationRequest) (*Response[SentVerification], error) {
	return call[SentVerification](ctx, s.client, http.MethodPost, "/api/v1/sms/verifications/send", nil, req)
}

func (s *SMSService) VerifyCode(ctx context.Context, req VerifyCodeRequest) (*Response[VerificationResult], error) {
	return call[VerificationResult](ctx, s.client, http.MethodPost, "/api/v1/sms/verifications/verify", nil, req)
}

func (s *SMSService) SendNotification(ctx context.Context, req SendNotificationRequest) (*Response[SentNotification], error) {
	return call[SentNotification](ctx, s.client, http.MethodPost, "/api/v1/sms/notifications/send", nil, req)
}

func (s *SMSService) GetMessage(ctx context.Context, messageID string) (*Response[SMSMessage], error) {
	return resource[SMSMessage](ctx, s.client, http.MethodGet, "/api/v1/sms/messages", messageID, "", nil, nil)
}
