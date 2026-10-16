package infra

import (
	"context"
	"net/http"
	"net/url"
)

type WechatService struct{ client *Client }

type WechatChannel struct {
	ChannelKey string `json:"channel_key"`
	Name       string `json:"name"`
	AppID      string `json:"app_id"`
	Enabled    bool   `json:"enabled"`
}

type CreateTempQRRequest struct {
	ChannelKey    string `json:"channel_key,omitempty"`
	SceneStr      string `json:"scene_str"`
	ExpireSeconds int    `json:"expire_seconds,omitempty"`
}

type TempQRCode struct {
	Ticket        string `json:"ticket"`
	URL           string `json:"url"`
	QRURL         string `json:"qr_url"`
	ExpireSeconds int    `json:"expire_seconds"`
}

type WechatUser struct {
	OpenID   string `json:"openid"`
	UnionID  string `json:"unionid"`
	Nickname string `json:"nickname"`
}

type SendCustomMessageRequest struct {
	ChannelKey string `json:"channel_key,omitempty"`
	OpenID     string `json:"openid"`
	MsgType    string `json:"msg_type,omitempty"`
	Content    string `json:"content"`
}

type SentCustomMessage struct {
	ChannelKey string `json:"channel_key"`
	OpenID     string `json:"openid"`
	MsgType    string `json:"msg_type"`
}

type WechatMenuButton struct {
	Name      string             `json:"name"`
	Type      string             `json:"type,omitempty"`
	URL       string             `json:"url,omitempty"`
	Key       string             `json:"key,omitempty"`
	AppID     string             `json:"appid,omitempty"`
	PagePath  string             `json:"pagepath,omitempty"`
	Value     string             `json:"value,omitempty"`
	SubButton []WechatMenuButton `json:"sub_button,omitempty"`
}

type WechatMenu struct {
	ChannelKey     string             `json:"channel_key"`
	AppID          string             `json:"app_id"`
	Button         []WechatMenuButton `json:"button"`
	Source         string             `json:"source"`
	HasConditional bool               `json:"has_conditional_menu"`
}

type WechatAccessToken struct {
	ChannelKey   string `json:"channel_key"`
	AppID        string `json:"app_id"`
	AccessToken  string `json:"access_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenVersion string `json:"token_version"`
}

type WechatTokenFailure struct {
	FailedTokenVersion string `json:"failed_token_version"`
	WechatErrCode      int    `json:"wechat_errcode"`
}

func (s *WechatService) Channels(ctx context.Context) (*Response[[]WechatChannel], error) {
	return call[[]WechatChannel](ctx, s.client, http.MethodGet, "/api/v1/wechat/mp/channels", nil, nil)
}

func (s *WechatService) CreateTempQR(ctx context.Context, req CreateTempQRRequest) (*Response[TempQRCode], error) {
	return call[TempQRCode](ctx, s.client, http.MethodPost, "/api/v1/wechat/mp/qrcode/temp", nil, req)
}

func (s *WechatService) GetUser(ctx context.Context, openID, channelKey string) (*Response[WechatUser], error) {
	query := url.Values{}
	if channelKey != "" {
		query.Set("channel_key", channelKey)
	}
	return resource[WechatUser](ctx, s.client, http.MethodGet, "/api/v1/wechat/mp/users", openID, "", query, nil)
}

func (s *WechatService) SendCustomMessage(ctx context.Context, req SendCustomMessageRequest) (*Response[SentCustomMessage], error) {
	return call[SentCustomMessage](ctx, s.client, http.MethodPost, "/api/v1/wechat/mp/messages/custom", nil, req)
}

func (s *WechatService) GetMenu(ctx context.Context, channelKey string) (*Response[WechatMenu], error) {
	return resource[WechatMenu](ctx, s.client, http.MethodGet, "/api/v1/wechat/mp/channels", channelKey, "/menu", nil, nil)
}

// GetAccessToken reads a WeChat credential only when token export is authorized.
// The SDK does not maintain a second WeChat token cache.
func (s *WechatService) GetAccessToken(ctx context.Context, channelKey string) (*Response[WechatAccessToken], error) {
	return resource[WechatAccessToken](ctx, s.client, http.MethodGet, "/api/v1/wechat/mp/channels", channelKey, "/access-token", nil, nil)
}

func (s *WechatService) RecoverAccessToken(ctx context.Context, channelKey string, req WechatTokenFailure) (*Response[WechatAccessToken], error) {
	return resource[WechatAccessToken](ctx, s.client, http.MethodPost, "/api/v1/wechat/mp/channels", channelKey, "/access-token/refresh", nil, req)
}
