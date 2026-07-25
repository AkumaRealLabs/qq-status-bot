package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"strings"

	"ai-upstream-monitor/internal/domain"
)

// OneBotService 只保留连接检查与 Webhook 鉴权。
type OneBotService struct {
	app *Service
}

func (s *OneBotService) Status(ctx context.Context) (domain.OneBotStatus, error) {
	cfg, err := s.app.Store.Settings(ctx)
	if err != nil {
		return domain.OneBotStatus{}, err
	}
	if !cfg.OneBotEnabled {
		return domain.OneBotStatus{Status: "disabled"}, nil
	}
	if domain.ValidateOneBotSettings(cfg) != nil {
		return domain.OneBotStatus{Status: "unconfigured"}, nil
	}
	if _, err := s.app.OneBotClient.GetLoginInfo(ctx, cfg.OneBotBaseURL, cfg.OneBotHTTPToken); err != nil {
		return domain.OneBotStatus{Status: "error", Error: "无法连接 OneBot 服务"}, nil
	}
	return domain.OneBotStatus{Status: "online"}, nil
}

// AuthorizeEvent 校验 LuckyLilliaBot HTTP POST 使用的 x-signature HMAC-SHA1。
func (s *OneBotService) AuthorizeEvent(ctx context.Context, signature string, payload []byte) error {
	cfg, err := s.app.Store.Settings(ctx)
	if err != nil {
		return err
	}
	if !cfg.OneBotEnabled || !oneBotSignatureValid(signature, cfg.OneBotWebhookToken, payload) {
		return ErrOneBotUnauthorized
	}
	return nil
}

func oneBotSignatureValid(signature, token string, payload []byte) bool {
	const prefix = "sha1="
	if !strings.HasPrefix(signature, prefix) || strings.TrimSpace(token) == "" {
		return false
	}
	provided, err := hex.DecodeString(strings.TrimSpace(strings.TrimPrefix(signature, prefix)))
	if err != nil {
		return false
	}
	mac := hmac.New(sha1.New, []byte(strings.TrimSpace(token)))
	_, _ = mac.Write(payload)
	return hmac.Equal(provided, mac.Sum(nil))
}
