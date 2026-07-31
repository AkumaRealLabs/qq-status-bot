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
	"qq-status-bot/internal/mailer"
	"qq-status-bot/internal/qqbot"
)

var (
	ErrUnauthorized            = errors.New("QQ Webhook 签名无效")
	ErrBadPayload              = errors.New("QQ Webhook 请求无效")
	ErrAlertGroupNotConfigured = errors.New("只能测试已保存的告警群")
	ErrActiveGroupNotAvailable = errors.New("只能发送到已发现或已配置的群")
	ErrInvalidAlertSimulation  = errors.New("模拟类型只支持 offline 或 recovery")
)

type StatusImageGenerator interface {
	Generate(ctx context.Context, baseURL, pageID, period string) ([]byte, error)
}

type GroupReplier interface {
	ReplyGroupImage(ctx context.Context, groupOpenID, messageID string, image []byte) error
	ReplyGroupText(ctx context.Context, groupOpenID, messageID, content string, messageSeq int) error
}

type ActiveImageSender interface {
	SendGroupImage(context.Context, string, []byte) error
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

type AlertStateStore interface {
	AlertState() domain.AlertState
	UpdateAlertState(domain.AlertState) error
}

type DiscoveredGroupStore interface {
	DiscoveredGroups() []string
}

type Service struct {
	settings      SettingsStore
	generator     StatusImageGenerator
	replier       GroupReplier
	jobs          chan domain.GroupMessage
	helpJobs      chan domain.GroupMessage
	accountJobs   chan domain.GroupMessage
	accountMu     sync.Mutex
	account       *AccountService
	alertFetcher  StatusFetcher
	alertInterval time.Duration
	alertStateMu  sync.Mutex
	alertState    domain.AlertState

	dedupMu sync.Mutex
	seen    map[string]time.Time
}

var shanghaiLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

func New(settings SettingsStore, generator StatusImageGenerator, replier GroupReplier, queueSize int, fetcher ...StatusFetcher) *Service {
	if queueSize < 1 {
		queueSize = 3
	}
	service := &Service{
		settings: settings, generator: generator, replier: replier,
		jobs: make(chan domain.GroupMessage, queueSize), helpJobs: make(chan domain.GroupMessage, queueSize), seen: make(map[string]time.Time),
		alertInterval: alertPollInterval,
	}
	if bindingStore, ok := settings.(AccountBindingStore); ok {
		service.account = NewAccountService(settings, bindingStore, nil, nil)
		service.accountJobs = make(chan domain.GroupMessage, queueSize)
	}
	if len(fetcher) > 0 {
		service.alertFetcher = fetcher[0]
	}
	return service
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
	s.accountMu.Lock()
	accountJobs := s.accountJobs
	s.accountMu.Unlock()
	if accountJobs != nil {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case message := <-accountJobs:
					s.processAccountMessage(ctx, message)
				}
			}
		}()
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case message := <-s.helpJobs:
				s.processHelpMessage(ctx, message)
			}
		}
	}()
	s.startAlerts(ctx)
}

func (s *Service) TestAlert(ctx context.Context, groupOpenID string) error {
	settings := s.settings.Settings()
	groupOpenID = strings.TrimSpace(groupOpenID)
	if groupOpenID == "" || !contains(settings.AlertGroups, groupOpenID) {
		return ErrAlertGroupNotConfigured
	}
	content := "[测试通知] 故障通知发送测试，当前时间：" + shanghaiNow().Format("2006-01-02 15:04:05 -0700")
	err := s.sendActiveText(ctx, groupOpenID, content)
	status := "sent"
	if err != nil {
		status = "failed"
	}
	s.appendAlertLog("send", "ALERT_TEST", status, trimLog(errorOrMessage(err, content)), groupOpenID)
	return err
}

func (s *Service) SendStatus(ctx context.Context, groupOpenID string) error {
	groupOpenID = strings.TrimSpace(groupOpenID)
	if !s.activeGroupAvailable(groupOpenID) {
		return ErrActiveGroupNotAvailable
	}
	sender, activeOK := s.replier.(ActiveImageSender)
	interactiveSender, interactiveOK := s.replier.(activeInteractiveImageSender)
	if !activeOK && !interactiveOK {
		return errors.New("QQ 客户端不支持主动图片消息")
	}
	image, err := s.generateStatusImage(ctx)
	if err == nil {
		if interactiveOK {
			err = interactiveSender.SendGroupImageWithKeyboard(ctx, groupOpenID, image, mainKeyboard(s.settings.Settings(), ""))
		} else {
			err = sender.SendGroupImage(ctx, groupOpenID, image)
		}
	}
	status := "sent"
	message := "状态图已主动发送"
	if err != nil {
		status = "failed"
		message = err.Error()
	}
	s.appendAlertLog("send", "STATUS_ACTIVE", status, trimLog(message), groupOpenID)
	return err
}

func (s *Service) SimulateAlert(ctx context.Context, groupOpenID, kind string) error {
	groupOpenID = strings.TrimSpace(groupOpenID)
	if !s.activeGroupAvailable(groupOpenID) {
		return ErrActiveGroupNotAvailable
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind != "offline" && kind != "recovery" {
		return ErrInvalidAlertSimulation
	}
	now := shanghaiNow()
	sample := alertSample{
		key: "simulation", groupName: "控制台测试", nodeName: "模拟节点",
		incident: now.Add(-5 * time.Minute).Format(time.RFC3339), recovery: now.Format(time.RFC3339),
	}
	content := "[模拟测试] " + formatAlertMessage(kind, []alertSample{sample})
	err := s.sendActiveText(ctx, groupOpenID, content)
	status := "sent"
	if err != nil {
		status = "failed"
	}
	eventType := "ALERT_SIMULATED_OFFLINE"
	if kind == "recovery" {
		eventType = "ALERT_SIMULATED_RECOVERY"
	}
	s.appendAlertLog("send", eventType, status, trimLog(errorOrMessage(err, content)), groupOpenID)
	return err
}

func errorOrMessage(err error, fallback string) string {
	if err != nil {
		return err.Error()
	}
	return fallback
}

func (s *Service) HandleWebhook(timestamp, signature string, body []byte) ([]byte, error) {
	return s.HandleWebhookContext(context.Background(), timestamp, signature, body)
}

func (s *Service) HandleWebhookContext(ctx context.Context, timestamp, signature string, body []byte) ([]byte, error) {
	settings := s.settings.Settings()
	if !qqbot.VerifyWebhook(settings.QQBotAppSecret, timestamp, signature, body) {
		return nil, ErrUnauthorized
	}
	var payload qqbot.Payload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, ErrBadPayload
	}
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
	switch payload.Op {
	case qqbot.OpHeartbeat:
		var seq uint64
		if err := json.Unmarshal(payload.Data, &seq); err != nil {
			return nil, ErrBadPayload
		}
		return qqbot.HeartbeatACK(seq), nil
	case qqbot.OpDispatch:
		return s.handleDispatchContext(ctx, payload, settings), nil
	default:
		return nil, nil
	}
}

func (s *Service) handleDispatch(payload qqbot.Payload, settings domain.Settings) []byte {
	return s.handleDispatchContext(context.Background(), payload, settings)
}

func (s *Service) handleDispatchContext(ctx context.Context, payload qqbot.Payload, settings domain.Settings) []byte {
	if payload.Type == qqbot.EventInteractionCreate {
		return s.handleInteraction(ctx, payload, settings)
	}
	if payload.Type != qqbot.EventGroupAtMessage && payload.Type != qqbot.EventGroupMessage {
		return qqbot.CallbackACK(true)
	}
	var message domain.GroupMessage
	if err := json.Unmarshal(payload.Data, &message); err != nil || message.ID == "" || message.GroupOpenID == "" {
		return qqbot.CallbackACK(true)
	}
	message.Content = domain.NormalizeContent(message.Content)
	if message.Author.Bot {
		s.logEvent(payload.Type, message, "ignored", "机器人消息")
		return qqbot.CallbackACK(true)
	}
	if !groupAllowed(message.GroupOpenID, settings.AllowedGroups) {
		s.logEvent(payload.Type, message, "ignored", "群不在白名单")
		return qqbot.CallbackACK(true)
	}
	if !botMentioned(payload.Type, message) {
		s.logEvent(payload.Type, message, "ignored", "未提及机器人")
		return qqbot.CallbackACK(true)
	}
	if domain.IsHelpCommand(message.Content) {
		s.accountMu.Lock()
		account := s.account
		s.accountMu.Unlock()
		if account == nil {
			if s.duplicate(message.ID) {
				s.logEvent(payload.Type, message, "duplicate", "重复消息")
				return qqbot.CallbackACK(true)
			}
			select {
			case s.helpJobs <- message:
				s.markSeen(message.ID)
				s.logEvent(payload.Type, message, "queued", "已加入帮助队列")
				return qqbot.CallbackACK(true)
			default:
				s.logEvent(payload.Type, message, "busy", "帮助队列已满")
				return qqbot.CallbackACK(false)
			}
		}
	}
	if strings.TrimSpace(message.Author.MemberOpenID) != "" {
		s.accountMu.Lock()
		account := s.account
		s.accountMu.Unlock()
		accountInput := account != nil && domain.IsAccountCommand(strings.TrimSpace(message.Content))
		if account != nil && account.HasPending(message.GroupOpenID, message.Author.MemberOpenID) {
			accountInput = true
		}
		if accountInput {
			if s.duplicate(message.ID) {
				s.logEvent(payload.Type, message, "duplicate", "重复消息")
				return qqbot.CallbackACK(true)
			}
			s.accountMu.Lock()
			if s.accountJobs == nil {
				s.accountJobs = make(chan domain.GroupMessage, cap(s.jobs))
			}
			accountJobs := s.accountJobs
			s.accountMu.Unlock()
			select {
			case accountJobs <- message:
				s.markSeen(message.ID)
				s.logEvent(payload.Type, message, "queued", "已加入账号功能队列")
				return qqbot.CallbackACK(true)
			default:
				s.logEvent(payload.Type, message, "busy", "账号功能队列已满")
				return qqbot.CallbackACK(false)
			}
		}
	}
	if !domain.IsCommand(message.Content, settings.Commands) {
		s.logEvent(payload.Type, message, "ignored", "非状态命令")
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

// botMentioned 同时兼容 QQ 的 @ 专用事件和开启全量群消息后的事件。
// 全量事件只有 mentions 中的结构化标记能证明消息确实提及了当前机器人。
func botMentioned(eventType string, message domain.GroupMessage) bool {
	if eventType == qqbot.EventGroupAtMessage {
		return true
	}
	return eventType == qqbot.EventGroupMessage && message.MentionsBot()
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
		err = s.replyImageWithMenu(ctx, message, image)
	}
	if err == nil {
		eventType := qqbot.EventGroupAtMessage
		if message.EventID != "" {
			eventType = qqbot.EventInteractionCreate
		}
		s.logEventDirection("send", eventType, message, "sent", "状态图已回复")
		return
	}
	log.Printf("状态图回复失败 group=%s: %v", message.GroupOpenID, err)
	eventType := qqbot.EventGroupAtMessage
	if message.EventID != "" {
		eventType = qqbot.EventInteractionCreate
	}
	s.logEventDirection("send", eventType, message, "failed", err.Error())
	errorCtx, errorCancel := context.WithTimeout(parent, 15*time.Second)
	defer errorCancel()
	errorText := "状态图生成失败，请稍后再试。"
	if strings.TrimSpace(message.Author.MemberOpenID) != "" || domain.NormalizeContent(message.Content) != "" {
		errorText += "重试示例：@机器人 状态"
	}
	if replyErr := s.replyTextWithMenu(errorCtx, message, errorText, 2, mainKeyboard(settings, message.Author.MemberOpenID)); replyErr != nil {
		log.Printf("状态图错误提示发送失败 group=%s: %v", message.GroupOpenID, replyErr)
	}
}

func (s *Service) processAccountMessage(parent context.Context, message domain.GroupMessage) {
	s.accountMu.Lock()
	account := s.account
	s.accountMu.Unlock()
	if account == nil {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	handled, reply := account.Handle(ctx, message)
	if !handled || reply == "" {
		return
	}
	keyboard := mainKeyboard(s.settings.Settings(), message.Author.MemberOpenID)
	if account.HasPending(message.GroupOpenID, message.Author.MemberOpenID) {
		keyboard = pendingKeyboard(message.Author.MemberOpenID, account.CanResend(message.GroupOpenID, message.Author.MemberOpenID))
	}
	if err := s.replyTextWithMenu(ctx, message, reply, 1, keyboard); err != nil {
		log.Printf("账号功能回复失败 group=%s: %v", message.GroupOpenID, err)
	}
}

func (s *Service) processHelpMessage(parent context.Context, message domain.GroupMessage) {
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	content := "可用命令示例：@机器人 状态\n查看状态：@机器人 状态\n文字命令也可以直接点击下方按钮。"
	if s.settings.Settings().GGAPIBalanceEnabled {
		content = accountHelp()
	}
	if err := s.replyTextWithMenu(ctx, message, content, 1, mainKeyboard(s.settings.Settings(), message.Author.MemberOpenID)); err != nil {
		log.Printf("帮助回复失败 group=%s: %v", message.GroupOpenID, err)
	}
}

func (s *Service) StatusPreview(parent context.Context) ([]byte, error) {
	return s.generateStatusImage(parent)
}

func (s *Service) generateStatusImage(parent context.Context) ([]byte, error) {
	settings := s.settings.Settings()
	timeout := time.Duration(settings.ScreenshotTimeout) * time.Second
	if timeout < 15*time.Second {
		timeout = 90 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	return s.generator.Generate(ctx, settings.StatusURL, settings.StatusPageID, settings.StatusPeriod)
}

func (s *Service) activeGroupAvailable(group string) bool {
	if group == "" {
		return false
	}
	settings := s.settings.Settings()
	for _, groups := range [][]string{settings.AlertGroups, settings.AllowedGroups, s.DiscoveredGroups()} {
		for _, candidate := range groups {
			if strings.TrimSpace(candidate) == group {
				return true
			}
		}
	}
	return false
}

func shanghaiNow() time.Time {
	return time.Now().In(shanghaiLocation)
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

func (s *Service) ConfigureAccounts(verifier AccountVerifier, sender mailer.Mailer) {
	store, ok := s.settings.(AccountBindingStore)
	if !ok {
		return
	}
	s.accountMu.Lock()
	defer s.accountMu.Unlock()
	if s.account == nil {
		s.account = NewAccountService(s.settings, store, verifier, sender)
	} else {
		s.account.Configure(verifier, sender)
	}
	if s.accountJobs == nil {
		s.accountJobs = make(chan domain.GroupMessage, cap(s.jobs))
	}
}

func (s *Service) SetupStatus() bool                     { return s.settings.SetupStatus() }
func (s *Service) Setup(username, password string) error { return s.settings.Setup(username, password) }
func (s *Service) Login(username, password string) (string, error) {
	return s.settings.Login(username, password)
}
func (s *Service) Authenticated(token string) bool  { return s.settings.Authenticated(token) }
func (s *Service) Logout(token string)              { s.settings.Logout(token) }
func (s *Service) Logs(limit int) []domain.EventLog { return s.settings.Logs(limit) }
func (s *Service) DiscoveredGroups() []string {
	if store, ok := s.settings.(DiscoveredGroupStore); ok {
		return store.DiscoveredGroups()
	}
	seen := make(map[string]struct{})
	groups := []string{}
	for _, item := range s.settings.Logs(500) {
		group := strings.TrimSpace(item.GroupOpenID)
		if item.Direction != "receive" || group == "" {
			continue
		}
		if _, exists := seen[group]; exists {
			continue
		}
		seen[group] = struct{}{}
		groups = append(groups, group)
	}
	return groups
}

func (s *Service) AccountBindings() []domain.AccountBindingView {
	s.accountMu.Lock()
	account := s.account
	s.accountMu.Unlock()
	if account != nil {
		return account.Bindings()
	}
	if store, ok := s.settings.(AccountBindingStore); ok {
		items := store.AccountBindings()
		views := make([]domain.AccountBindingView, 0, len(items))
		for _, item := range items {
			views = append(views, item.PublicView())
		}
		return views
	}
	return []domain.AccountBindingView{}
}

func (s *Service) DeleteAccountBinding(id string) error {
	s.accountMu.Lock()
	account := s.account
	s.accountMu.Unlock()
	if account != nil {
		deleted, err := account.Revoke(id)
		if err != nil {
			return err
		}
		if !deleted {
			return ErrAccountBindingNotFound
		}
		return nil
	}
	store, ok := s.settings.(AccountBindingStore)
	if !ok {
		return ErrAccountNotConfigured
	}
	deleted, err := store.DeleteAccountBinding(id)
	if err != nil {
		return err
	}
	if !deleted {
		return ErrAccountBindingNotFound
	}
	return nil
}

func (s *Service) TestSMTP(ctx context.Context, recipient string) error {
	s.accountMu.Lock()
	account := s.account
	s.accountMu.Unlock()
	if account == nil {
		return ErrAccountNotConfigured
	}
	return account.TestEmail(ctx, recipient)
}

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
