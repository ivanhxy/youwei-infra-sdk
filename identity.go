package infra

import (
	"context"
	"net/http"
)

type IdentityService struct{ client *Client }

type PasswordRegisterRequest struct {
	Mobile   string `json:"mobile"`
	Code     string `json:"code"`
	Password string `json:"password"`
}

type PasswordLoginRequest struct {
	Mobile   string `json:"mobile"`
	Password string `json:"password"`
}

type SMSLoginRequest struct {
	Mobile string `json:"mobile"`
	Code   string `json:"code"`
}

// UserTokens are end-user credentials, separate from the SDK's cached Client Token.
type UserTokens struct {
	UserID         string          `json:"user_id"`
	AccessToken    string          `json:"access_token"`
	ExpiresIn      int64           `json:"expires_in"`
	RefreshToken   string          `json:"refresh_token"`
	IsNewUser      bool            `json:"is_new_user"`
	MobileMasked   string          `json:"mobile_masked,omitempty"`
	WechatIdentity *WechatIdentity `json:"wechat_identity,omitempty"`
}

type WechatIdentity struct {
	OpenID string `json:"open_id"`
}

type LoginQRCode struct {
	LoginID       string `json:"login_id"`
	QRURL         string `json:"qr_url"`
	ExpireSeconds int    `json:"expire_seconds"`
}

type LoginStatus struct {
	Status    string `json:"status"`
	ExpiresIn int64  `json:"expires_in"`
}

func (s *IdentityService) RegisterPassword(ctx context.Context, req PasswordRegisterRequest) (*Response[UserTokens], error) {
	return call[UserTokens](ctx, s.client, http.MethodPost, "/api/v1/identity/password/register", nil, req)
}

func (s *IdentityService) LoginPassword(ctx context.Context, req PasswordLoginRequest) (*Response[UserTokens], error) {
	return call[UserTokens](ctx, s.client, http.MethodPost, "/api/v1/identity/password/login", nil, req)
}

// SendSMSCode sends an identity login/registration code, not a business SMS code.
func (s *IdentityService) SendSMSCode(ctx context.Context, mobile string) (*Response[struct{}], error) {
	return call[struct{}](ctx, s.client, http.MethodPost, "/api/v1/identity/sms/send-code", nil, struct {
		Mobile string `json:"mobile"`
	}{mobile})
}

func (s *IdentityService) LoginSMS(ctx context.Context, req SMSLoginRequest) (*Response[UserTokens], error) {
	return call[UserTokens](ctx, s.client, http.MethodPost, "/api/v1/identity/sms/login", nil, req)
}

func (s *IdentityService) RefreshToken(ctx context.Context, refreshToken string) (*Response[UserTokens], error) {
	return call[UserTokens](ctx, s.client, http.MethodPost, "/api/v1/identity/token/refresh", nil, struct {
		RefreshToken string `json:"refresh_token"`
	}{refreshToken})
}

func (s *IdentityService) CreateWechatLoginQR(ctx context.Context, channelKey string) (*Response[LoginQRCode], error) {
	return call[LoginQRCode](ctx, s.client, http.MethodPost, "/api/v1/identity/wechat/login/qrcode", nil, struct {
		ChannelKey string `json:"channel_key,omitempty"`
	}{channelKey})
}

func (s *IdentityService) GetWechatLoginStatus(ctx context.Context, loginID string) (*Response[LoginStatus], error) {
	return resource[LoginStatus](ctx, s.client, http.MethodGet, "/api/v1/identity/wechat/login/qrcode", loginID, "", nil, nil)
}

// LoginWechat consumes a confirmed login session once; it does not process a webhook.
func (s *IdentityService) LoginWechat(ctx context.Context, loginID string) (*Response[UserTokens], error) {
	return resource[UserTokens](ctx, s.client, http.MethodPost, "/api/v1/identity/wechat/login/qrcode", loginID, "/login", nil, struct{}{})
}
