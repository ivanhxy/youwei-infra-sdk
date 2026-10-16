package infra

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestWebhookAuthenticationAndReplayWindow(t *testing.T) {
	now := time.Unix(1700000000, 0)
	body := []byte(`{"event_id":"event-1","infra_client_id":"client","channel_key":"mp1","wechat":{"event":"SCAN","raw":{"xml":"original"}},"context":{"openid":"user","scene_str":"campaign"}}`)
	sign := func(timestamp string, data []byte) string {
		mac := hmac.New(sha256.New, []byte("secret"))
		_, _ = mac.Write(append([]byte(timestamp+"\n"), data...))
		return hex.EncodeToString(mac.Sum(nil))
	}
	ts := strconv.FormatInt(now.Unix(), 10)
	event, err := ParseWechatEvent("secret", ts, strings.ToUpper(sign(ts, body)), body, now)
	if err != nil || event.EventID != "event-1" || event.Context.OpenID != "user" || event.Wechat.Event != "SCAN" || string(event.Wechat.Raw) != `{"xml":"original"}` {
		t.Fatalf("valid event rejected or changed: %v", err)
	}
	for _, delta := range []int64{-301, -300, 300, 301} {
		ts := strconv.FormatInt(now.Unix()+delta, 10)
		err := VerifyWebhook("secret", ts, sign(ts, body), body, now)
		if (err == nil) != (delta >= -300 && delta <= 300) {
			t.Errorf("incorrect replay boundary %d: %v", delta, err)
		}
	}
	for _, tc := range []struct {
		secret, timestamp, signature string
		data                         []byte
	}{
		{"", ts, sign(ts, body), body},
		{"wrong", ts, sign(ts, body), body},
		{"secret", "invalid", sign(ts, body), body},
		{"secret", "9223372036854775807", sign(ts, body), body},
		{"secret", ts, "zz", body},
		{"secret", ts, sign(ts, body), append(append([]byte{}, body...), ' ')},
	} {
		if VerifyWebhook(tc.secret, tc.timestamp, tc.signature, tc.data, now) == nil {
			t.Error("forged/expired webhook accepted")
		}
	}
	for _, invalid := range [][]byte{[]byte("{"), []byte(`{}`)} {
		if _, err := ParseWechatEvent("secret", ts, sign(ts, invalid), invalid, now); err == nil {
			t.Error("invalid event accepted")
		}
	}
}
