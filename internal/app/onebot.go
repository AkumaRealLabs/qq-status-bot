package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"ai-upstream-monitor/internal/domain"
	"ai-upstream-monitor/internal/onebot"
)

const (
	oneBotEventTTL      = 5 * time.Minute
	oneBotCommandWindow = 10 * time.Second
	oneBotMessageLimit  = 1900
)

// OneBotService 负责 QQ 群命令的鉴权、过滤和公开状态回复。
type OneBotService struct {
	app *Service
}

type oneBotRuntime struct {
	mu       sync.Mutex
	seen     map[string]time.Time
	cooldown map[string]time.Time
}

func newOneBotRuntime() oneBotRuntime {
	return oneBotRuntime{seen: map[string]time.Time{}, cooldown: map[string]time.Time{}}
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
// 该版本不会发送 Authorization Bearer；签名覆盖原始 JSON 请求体。
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

// HandleEvent 将所有非目标事件静默丢弃。调用方必须先完成 Webhook 签名校验。
func (s *OneBotService) HandleEvent(ctx context.Context, event onebot.Event) error {
	cfg, err := s.app.Store.Settings(ctx)
	if err != nil {
		return err
	}
	if domain.ValidateOneBotSettings(cfg) != nil {
		return nil
	}
	if !event.IsStatusCommand() {
		if allowedOneBotGroup(cfg.OneBotGroupIDs, event.GroupIDString()) && event.IsNormalGroupMessage() && event.HasStatusCommandText() {
			log.Printf("onebot: ignored status candidate segments=%s", event.StatusCommandDiagnostic())
		}
		return nil
	}
	groupID, userID, messageID := event.GroupIDString(), event.UserIDString(), event.MessageIDString()
	if groupID == "" || userID == "" || messageID == "" || !allowedOneBotGroup(cfg.OneBotGroupIDs, groupID) {
		return nil
	}
	if !s.app.oneBotRuntime.accept(groupID, userID, messageID, time.Now()) {
		return nil
	}
	public, err := s.app.PublicMonitorStatus(ctx, "1h")
	if err != nil {
		return err
	}
	if renderer := s.app.OneBotStatusImageRenderer; renderer != nil {
		images, renderErr := renderer.Render(public)
		if renderErr == nil && len(images) > 0 {
			for _, image := range images {
				if err := s.app.OneBotClient.SendGroupImage(ctx, cfg.OneBotBaseURL, cfg.OneBotHTTPToken, groupID, image); err != nil {
					return err
				}
			}
			return nil
		}
	}
	for _, part := range splitOneBotMessage(formatOneBotStatus(public), oneBotMessageLimit) {
		if err := s.app.OneBotClient.SendGroupMessage(ctx, cfg.OneBotBaseURL, cfg.OneBotHTTPToken, groupID, part); err != nil {
			return err
		}
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

func allowedOneBotGroup(groups []string, groupID string) bool {
	for _, allowed := range groups {
		if allowed == groupID {
			return true
		}
	}
	return false
}

func (r *oneBotRuntime) accept(groupID, userID, messageID string, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, expiresAt := range r.seen {
		if !expiresAt.After(now) {
			delete(r.seen, key)
		}
	}
	for key, expiresAt := range r.cooldown {
		if !expiresAt.After(now) {
			delete(r.cooldown, key)
		}
	}
	eventKey := groupID + ":" + userID + ":" + messageID
	if _, found := r.seen[eventKey]; found {
		return false
	}
	r.seen[eventKey] = now.Add(oneBotEventTTL)
	cooldownKey := groupID + ":" + userID
	if until, found := r.cooldown[cooldownKey]; found && until.After(now) {
		return false
	}
	r.cooldown[cooldownKey] = now.Add(oneBotCommandWindow)
	return true
}

type oneBotGroupSummary struct {
	name                               string
	normal, degraded, abnormal, paused int
	muted, noData                      int
	errors                             []string
}

func formatOneBotStatus(status domain.PublicMonitorStatus) string {
	groups := make([]oneBotGroupSummary, 0)
	byName := map[string]int{}
	for _, card := range status.Rows {
		name := strings.TrimSpace(card.DisplayGroup)
		if name == "" {
			name = "其他"
		}
		index, ok := byName[name]
		if !ok {
			index = len(groups)
			byName[name] = index
			groups = append(groups, oneBotGroupSummary{name: name})
		}
		group := &groups[index]
		switch {
		case card.AutoProbePaused:
			group.paused++
		case card.ProbeMuted:
			group.muted++
		case len(card.History) == 0:
			group.noData++
		default:
			switch card.History[len(card.History)-1].Status {
			case "正常":
				group.normal++
			case "延迟偏高":
				group.degraded++
			default:
				group.abnormal++
				group.errors = append(group.errors, oneBotCardName(card.Name))
			}
		}
	}
	lines := []string{
		"公开状态（" + status.Window + "）",
		"请求数 " + intText(status.Requests) + "，成功率 " + percentText(status.SuccessRate) + "，平均延迟 " + intText(status.AvgLatency) + " ms",
	}
	total := oneBotGroupSummary{}
	for _, group := range groups {
		total.normal += group.normal
		total.degraded += group.degraded
		total.abnormal += group.abnormal
		total.paused += group.paused
		total.muted += group.muted
		total.noData += group.noData
	}
	lines = append(lines, "正常 "+intText(total.normal)+"，延迟偏高 "+intText(total.degraded)+"，异常 "+intText(total.abnormal)+"，暂停 "+intText(total.paused)+"，静默 "+intText(total.muted)+"，无数据 "+intText(total.noData))
	for _, group := range groups {
		lines = append(lines, "【"+group.name+"】正常 "+intText(group.normal)+"，延迟偏高 "+intText(group.degraded)+"，异常 "+intText(group.abnormal)+"，暂停 "+intText(group.paused)+"，静默 "+intText(group.muted)+"，无数据 "+intText(group.noData))
		if len(group.errors) > 0 {
			lines = append(lines, "异常："+strings.Join(group.errors, "、"))
		}
	}
	return strings.Join(lines, "\n")
}

func oneBotCardName(name string) string {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return "未命名卡片"
	}
	return name
}

func intText(value int) string {
	return strconv.Itoa(value)
}

func percentText(value float64) string {
	return strconv.Itoa(int(value+0.5)) + "%"
}

// splitOneBotMessage 优先按整行切分；单行超长时再按 Unicode 字符切开。
func splitOneBotMessage(text string, limit int) []string {
	if limit <= 0 || utf8.RuneCountInString(text) <= limit {
		return []string{text}
	}
	var out []string
	var current strings.Builder
	currentLen := 0
	flush := func() {
		if currentLen > 0 {
			out = append(out, strings.TrimSuffix(current.String(), "\n"))
			current.Reset()
			currentLen = 0
		}
	}
	for _, line := range strings.SplitAfter(text, "\n") {
		lineLen := utf8.RuneCountInString(line)
		if lineLen <= limit-currentLen {
			current.WriteString(line)
			currentLen += lineLen
			continue
		}
		flush()
		for utf8.RuneCountInString(line) > limit {
			part, rest := cutRunes(line, limit)
			out = append(out, strings.TrimSuffix(part, "\n"))
			line = rest
		}
		current.WriteString(line)
		currentLen = utf8.RuneCountInString(line)
	}
	flush()
	return out
}

func cutRunes(value string, limit int) (string, string) {
	count := 0
	for index := range value {
		if count == limit {
			return value[:index], value[index:]
		}
		count++
	}
	return value, ""
}
