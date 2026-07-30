package app

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"qq-status-bot/internal/domain"
	"qq-status-bot/internal/qqbot"
)

var (
	ErrUnauthorized = errors.New("QQ Webhook 签名无效")
	ErrBadPayload   = errors.New("QQ Webhook 请求无效")
)

type StatusImageGenerator interface {
	Generate(ctx context.Context, baseURL, pageID, period string) ([]byte, error)
}

type GroupReplier interface {
	ReplyGroupImage(ctx context.Context, groupOpenID, messageID string, image []byte) error
	ReplyGroupText(ctx context.Context, groupOpenID, messageID, content string, messageSeq int) error
}

type SettingsStore interface {
	Settings() domain.Settings
	UpdateSettings(domain.Settings) (domain.Settings, error)
	Setup(username, password string) error
	SetupStatus() bool
	Login(username, password string) (string, error)
	Authenticated(token string) bool
	Logout(token string)
	AppendLog(domain.EventLog) error
	Logs(limit int) []domain.EventLog
}

type Service struct {
	settings  SettingsStore
	generator StatusImageGenerator
	replier   GroupReplier
	jobs      chan domain.GroupMessage

	dedupMu sync.Mutex
	seen    map[string]time.Time
}

func New(settings SettingsStore, generator StatusImageGenerator, replier GroupReplier, queueSize int) *Service {
	if queueSize < 1 {
		queueSize = 3
	}
	return &Service{
		settings: settings, generator: generator, replier: replier,
		jobs: make(chan domain.GroupMessage, queueSize), seen: make(map[string]time.Time),
	}
}

func (s *Service) Start(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case message := <-s.jobs:
				s.processMessage(ctx, message)
			}
		}
	}()
}

func (s *Service) HandleWebhook(timestamp, signature string, body []byte) ([]byte, error) {
	var payload qqbot.Payload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, ErrBadPayload
	}
	settings := s.settings.Settings()
	if payload.Op == qqbot.OpValidation {
		var request qqbot.ValidationRequest
		if err := json.Unmarshal(payload.Data, &request); err != nil {
			return nil, ErrBadPayload
		}
		response, err := qqbot.ValidationResponse(settings.QQBotAppSecret, request)
		if err != nil {
			return nil, ErrBadPayload
		}
		_ = s.settings.AppendLog(domain.EventLog{Direction: "receive", EventType: "CALLBACK_VALIDATION", Status: "ok"})
		return response, nil
	}
	if !qqbot.VerifyWebhook(settings.QQBotAppSecret, timestamp, signature, body) {
		return nil, ErrUnauthorized
	}
	switch payload.Op {
	case qqbot.OpHeartbeat:
		var seq uint64
		if err := json.Unmarshal(payload.Data, &seq); err != nil {
			return nil, ErrBadPayload
		}
		return qqbot.HeartbeatACK(seq), nil
	case qqbot.OpDispatch:
		return s.handleDispatch(payload, settings), nil
	default:
		return nil, nil
	}
}

func (s *Service) handleDispatch(payload qqbot.Payload, settings domain.Settings) []byte {
	if payload.Type != qqbot.EventGroupAtMessage && payload.Type != qqbot.EventGroupMessage {
		return qqbot.CallbackACK(true)
	}
	var message domain.GroupMessage
	if err := json.Unmarshal(payload.Data, &message); err != nil || message.ID == "" || message.GroupOpenID == "" {
		return qqbot.CallbackACK(true)
	}
	if message.Author.Bot {
		s.logEvent(payload.Type, message, "ignored", "机器人消息")
		return qqbot.CallbackACK(true)
	}
	if !domain.IsCommand(message.Content, settings.Commands) {
		s.logEvent(payload.Type, message, "ignored", "非状态命令")
		return qqbot.CallbackACK(true)
	}
	if !groupAllowed(message.GroupOpenID, settings.AllowedGroups) {
		s.logEvent(payload.Type, message, "ignored", "群不在白名单")
		return qqbot.CallbackACK(true)
	}
	if s.duplicate(message.ID) {
		s.logEvent(payload.Type, message, "duplicate", "重复消息")
		return qqbot.CallbackACK(true)
	}
	select {
	case s.jobs <- message:
		s.markSeen(message.ID)
		s.logEvent(payload.Type, message, "queued", "已加入状态图队列")
		return qqbot.CallbackACK(true)
	default:
		s.logEvent(payload.Type, message, "busy", "状态图队列已满")
		return qqbot.CallbackACK(false)
	}
}

func (s *Service) processMessage(parent context.Context, message domain.GroupMessage) {
	settings := s.settings.Settings()
	timeout := time.Duration(settings.ScreenshotTimeout) * time.Second
	if timeout < 15*time.Second {
		timeout = 90 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	image, err := s.generator.Generate(ctx, settings.StatusURL, settings.StatusPageID, settings.StatusPeriod)
	if err == nil {
		err = s.replier.ReplyGroupImage(ctx, message.GroupOpenID, message.ID, image)
	}
	if err == nil {
		s.logEventDirection("send", qqbot.EventGroupAtMessage, message, "sent", "状态图已回复")
		return
	}
	log.Printf("状态图回复失败 group=%s: %v", message.GroupOpenID, err)
	s.logEventDirection("send", qqbot.EventGroupAtMessage, message, "failed", err.Error())
	errorCtx, errorCancel := context.WithTimeout(parent, 15*time.Second)
	defer errorCancel()
	if replyErr := s.replier.ReplyGroupText(errorCtx, message.GroupOpenID, message.ID, "状态图生成失败，请稍后再试。", 2); replyErr != nil {
		log.Printf("状态图错误提示发送失败 group=%s: %v", message.GroupOpenID, replyErr)
	}
}

func (s *Service) StatusPreview(parent context.Context) ([]byte, error) {
	settings := s.settings.Settings()
	timeout := time.Duration(settings.ScreenshotTimeout) * time.Second
	if timeout < 15*time.Second {
		timeout = 90 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	return s.generator.Generate(ctx, settings.StatusURL, settings.StatusPageID, settings.StatusPeriod)
}

func (s *Service) logEvent(eventType string, message domain.GroupMessage, status, detail string) {
	s.logEventDirection("receive", eventType, message, status, detail)
}

func (s *Service) logEventDirection(direction, eventType string, message domain.GroupMessage, status, detail string) {
	if err := s.settings.AppendLog(domain.EventLog{
		Direction: direction, EventType: eventType, GroupOpenID: message.GroupOpenID,
		MessageID: message.ID, Status: status, Message: trimLog(detail),
	}); err != nil {
		log.Printf("写入事件日志失败: %v", err)
	}
}

func (s *Service) Settings() domain.Settings { return s.settings.Settings().Public() }

func (s *Service) UpdateSettings(settings domain.Settings) (domain.Settings, error) {
	merged := settings.MergeUpdate(s.settings.Settings())
	if err := merged.Validate(); err != nil {
		return domain.Settings{}, err
	}
	updated, err := s.settings.UpdateSettings(merged)
	return updated.Public(), err
}

func (s *Service) SetupStatus() bool                     { return s.settings.SetupStatus() }
func (s *Service) Setup(username, password string) error { return s.settings.Setup(username, password) }
func (s *Service) Login(username, password string) (string, error) {
	return s.settings.Login(username, password)
}
func (s *Service) Authenticated(token string) bool  { return s.settings.Authenticated(token) }
func (s *Service) Logout(token string)              { s.settings.Logout(token) }
func (s *Service) Logs(limit int) []domain.EventLog { return s.settings.Logs(limit) }
func (s *Service) Health() map[string]string {
	return map[string]string{"status": "ok", "service": "qq-status-bot"}
}

func groupAllowed(group string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, candidate := range allowed {
		if strings.TrimSpace(candidate) == group {
			return true
		}
	}
	return false
}

func (s *Service) duplicate(messageID string) bool {
	now := time.Now()
	s.dedupMu.Lock()
	defer s.dedupMu.Unlock()
	for id, seenAt := range s.seen {
		if now.Sub(seenAt) > 10*time.Minute {
			delete(s.seen, id)
		}
	}
	_, ok := s.seen[messageID]
	return ok
}

func (s *Service) markSeen(messageID string) {
	s.dedupMu.Lock()
	s.seen[messageID] = time.Now()
	s.dedupMu.Unlock()
}

func trimLog(message string) string {
	message = strings.Join(strings.Fields(message), " ")
	if len(message) > 300 {
		return message[:300]
	}
	return message
}
