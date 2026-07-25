package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"

	"ai-upstream-monitor/internal/domain"
)

func (s *Service) OpsEvents(ctx context.Context, filter domain.OpsEventFilter) ([]domain.OpsEvent, error) {
	return s.Store.OpsEvents(ctx, filter)
}

func (s *Service) OpsEventGroups(ctx context.Context, filter domain.OpsEventFilter) ([]domain.OpsEventGroup, error) {
	return s.Store.OpsEventGroups(ctx, filter)
}

func (s *Service) AuditLogs(ctx context.Context, action, target string, limit int) ([]domain.AuditLog, error) {
	return s.Store.AuditLogs(ctx, action, target, limit)
}

func (s *Service) NotificationRules(ctx context.Context) (domain.NotificationRules, error) {
	return s.Store.NotificationRules(ctx)
}

func (s *Service) SaveNotificationRules(ctx context.Context, rules domain.NotificationRules) (domain.NotificationRules, error) {
	return s.Store.UpdateNotificationRules(ctx, rules)
}

func (s *Service) TestNotification(ctx context.Context) error {
	cfg, err := s.Store.Settings(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.TelegramBotToken) == "" || strings.TrimSpace(cfg.TelegramChatID) == "" {
		return ErrBadRequest("请先配置 Telegram Bot Token 和 Chat ID")
	}
	return s.Notify.Send(ctx, "通知规则测试")
}

func (s *Service) createAlertOpsEvent(ctx context.Context, u domain.Upstream, kind string, recover bool, message string) {
	eventType, targetType, targetID := domain.AlertEventType(kind, recover)
	severity := "warning"
	if recover {
		severity = "success"
	}
	_, _ = s.Store.CreateOpsEvent(ctx, domain.OpsEvent{
		Type:       eventType,
		Severity:   severity,
		Title:      domain.AlertOpsTitle(eventType, recover),
		Message:    message,
		TargetType: targetType,
		TargetID:   domain.FirstNonEmpty(targetID, u.ID),
		Actions:    domain.AlertOpsActions(eventType),
	})
}

func (s *Service) SelfCheck(ctx context.Context) (domain.SelfCheckResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	out := domain.SelfCheckResponse{CheckedAt: time.Now().UTC()}
	out.Items = append(out.Items, checkItem("app", nil, "HTTP API 正常"))
	out.Items = append(out.Items, checkItem("database_writable", s.Store.CheckWritable(ctx), "数据库可写"))
	out.Items = append(out.Items, diskCheck())
	out.Items = append(out.Items, domain.SelfCheckItem{Name: "build_version", Status: "ok", Message: domain.FirstNonEmpty(os.Getenv("VITE_BUILD_VERSION"), "dev")})
	out.Items = append(out.Items, domain.SelfCheckItem{Name: "container_restart_count", Status: "safe_mode", Message: "安全模式未读取容器重启次数"})
	out.Items = append(out.Items, checkHTTP(ctx, s.Client.HTTP, "browser_http", envDefault("BROWSER_PROXY_URL", "http://127.0.0.1:6080")))
	out.Items = append(out.Items, checkBrowserCDP(ctx, s.Client.HTTP, envDefault("BROWSER_DEBUG_URL", "http://127.0.0.1:19222")))
	out.Items = append(out.Items, domain.SelfCheckItem{Name: "database_backup", Status: "warn", Message: "未配置自动备份时间"})
	return out, nil
}

func diskCheck() domain.SelfCheckItem {
	var stat syscall.Statfs_t
	path := envDefault("AUM_DATA_DIR", "/app/data")
	if err := syscall.Statfs(path, &stat); err != nil {
		_ = syscall.Statfs(".", &stat)
	}
	free := stat.Bavail * uint64(stat.Bsize)
	status := "ok"
	if free < 1<<30 {
		status = "warn"
	}
	return domain.SelfCheckItem{Name: "disk_space", Status: status, Message: fmt.Sprintf("可用 %.1f GB", float64(free)/(1<<30))}
}

func checkBrowserCDP(ctx context.Context, hc *http.Client, baseURL string) domain.SelfCheckItem {
	baseURL = strings.TrimRight(baseURL, "/")
	hostHeader := browserCDPHostHeader(baseURL)
	if item := checkHTTPWithHost(ctx, hc, "browser_cdp", baseURL+"/json/version", hostHeader); item.Status == "ok" {
		return item
	}
	return checkHTTPWithHost(ctx, hc, "browser_cdp", baseURL+"/json", hostHeader)
}

func checkHTTP(ctx context.Context, hc *http.Client, name, rawurl string) domain.SelfCheckItem {
	return checkHTTPWithHost(ctx, hc, name, rawurl, "")
}

func checkHTTPWithHost(ctx context.Context, hc *http.Client, name, rawurl, hostHeader string) domain.SelfCheckItem {
	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if hc == nil {
		hc = &http.Client{Timeout: 5 * time.Second}
	}
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rawurl, nil)
	if err != nil {
		return checkItem(name, err, "")
	}
	if hostHeader != "" {
		req.Host = hostHeader
	}
	resp, err := hc.Do(req)
	if err != nil {
		return checkItem(name, err, "")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return domain.SelfCheckItem{Name: name, Status: "error", Message: resp.Status}
	}
	return domain.SelfCheckItem{Name: name, Status: "ok", Message: rawurl}
}

func browserCDPHostHeader(rawurl string) string {
	if v := strings.TrimSpace(os.Getenv("BROWSER_DEBUG_HOST_HEADER")); v != "" {
		return v
	}
	u, err := url.Parse(rawurl)
	if err != nil {
		return "127.0.0.1:19222"
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") || net.ParseIP(host) != nil {
		return hostWithDefaultPort(u.Host, u.Scheme)
	}
	return "127.0.0.1:19222"
}

func hostWithDefaultPort(host, scheme string) string {
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}
	if strings.Contains(host, ":") {
		return host
	}
	if scheme == "https" || scheme == "wss" {
		return net.JoinHostPort(host, "443")
	}
	return net.JoinHostPort(host, "80")
}

func checkItem(name string, err error, ok string) domain.SelfCheckItem {
	if err != nil {
		return domain.SelfCheckItem{Name: name, Status: "error", Message: err.Error()}
	}
	return domain.SelfCheckItem{Name: name, Status: "ok", Message: ok}
}

func envDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
