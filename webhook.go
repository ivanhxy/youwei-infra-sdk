package infra

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"
)

// WechatEvent matches the event dispatcher, including its raw WeChat escape hatch.
type WechatEvent struct {
	EventID       string             `json:"event_id"`
	DeliveredAt   time.Time          `json:"delivered_at"`
	ChannelKey    string             `json:"channel_key"`
	AppID         string             `json:"app_id"`
	InfraClientID string             `json:"infra_client_id"`
	Wechat        WechatEventPayload `json:"wechat"`
	Context       WechatEventContext `json:"context"`
}

type WechatEventPayload struct {
	MsgType      string          `json:"msg_type,omitempty"`
	Event        string          `json:"event,omitempty"`
	FromUserName string          `json:"from_user_name,omitempty"`
	ToUserName   string          `json:"to_user_name,omitempty"`
	EventKey     string          `json:"event_key,omitempty"`
	Content      string          `json:"content,omitempty"`
	MsgID        string          `json:"msg_id,omitempty"`
	CreateTime   int64           `json:"create_time,omitempty"`
	AppID        string          `json:"app_id,omitempty"`
	Raw          json.RawMessage `json:"raw,omitempty"`
}

type WechatEventContext struct {
	SessionID string `json:"session_id,omitempty"`
	OpenID    string `json:"openid,omitempty"`
	UnionID   string `json:"unionid,omitempty"`
	SceneStr  string `json:"scene_str,omitempty"`
}

// VerifyWebhook verifies HMAC-SHA256 over timestamp + newline + the original body.
// A five-minute clock window limits replay; applications must deduplicate event_id.
func VerifyWebhook(secret, timestamp, signature string, body []byte, now time.Time) error {
	if secret == "" {
		return errors.New("infra: webhook secret is required")
	}
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || ts < now.Unix()-300 || ts > now.Unix()+300 {
		return errors.New("infra: webhook timestamp is invalid or expired")
	}
	got, err := hex.DecodeString(signature)
	if err != nil || len(got) != sha256.Size {
		return errors.New("infra: invalid webhook signature")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp + "\n"))
	_, _ = mac.Write(body)
	if !hmac.Equal(mac.Sum(nil), got) {
		return errors.New("infra: invalid webhook signature")
	}
	return nil
}

// ParseWechatEvent verifies before decoding; body must be the unchanged HTTP body.
func ParseWechatEvent(secret, timestamp, signature string, body []byte, now time.Time) (*WechatEvent, error) {
	if err := VerifyWebhook(secret, timestamp, signature, body, now); err != nil {
		return nil, err
	}
	var event WechatEvent
	if err := json.Unmarshal(body, &event); err != nil {
		return nil, errors.New("infra: invalid WeChat event JSON")
	}
	if event.EventID == "" || event.InfraClientID == "" || event.ChannelKey == "" {
		return nil, errors.New("infra: incomplete WeChat event")
	}
	return &event, nil
}
