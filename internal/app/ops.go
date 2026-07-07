package app

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"syscall"
	"time"

	"ai-upstream-monitor/internal/domain"
)

func (s *Service) OpsEvents(ctx context.Context, eventType, state string, limit int) ([]domain.OpsEvent, error) {
	return s.Store.OpsEvents(ctx, eventType, state, limit)
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
	return s.sendTelegram(ctx, "Ops 通知测试")
}

func (s *Service) createAlertOpsEvent(ctx context.Context, u domain.Upstream, kind string, recover bool, message string) {
	eventType, targetType, targetID := alertOpsType(kind, recover)
	severity := "warning"
	if recover {
		severity = "success"
	}
	_, _ = s.Store.CreateOpsEvent(ctx, domain.OpsEvent{
		Type:       eventType,
		Severity:   severity,
		Title:      alertOpsTitle(eventType, recover),
		Message:    message,
		TargetType: targetType,
		TargetID:   firstNonEmpty(targetID, u.ID),
		Actions:    alertOpsActions(eventType),
	})
}

func alertOpsType(kind string, recover bool) (string, string, string) {
	if strings.HasPrefix(kind, "ping:") {
		return "probe_failed", "card", strings.TrimPrefix(kind, "ping:")
	}
	switch kind {
	case "balance":
		return "balance_low", "upstream", ""
	case "credential":
		return "credential_invalid", "upstream", ""
	case "balance_query":
		return "balance_query_failed", "upstream", ""
	default:
		if recover {
			return "system_recovered", "upstream", ""
		}
		return "system_warning", "upstream", ""
	}
}

func alertOpsTitle(eventType string, recover bool) string {
	if recover {
		return "已恢复"
	}
	switch eventType {
	case "probe_failed":
		return "探测失败"
	case "balance_low":
		return "余额低"
	case "credential_invalid":
		return "凭据失效"
	case "balance_query_failed":
		return "额度查询失败"
	default:
		return "系统事件"
	}
}

func alertOpsActions(eventType string) []string {
	switch eventType {
	case "probe_failed":
		return []string{"check_card"}
	case "credential_invalid", "balance_query_failed":
		return []string{"check_upstream", "sync_keys"}
	case "balance_low":
		return []string{"check_upstream"}
	case "cliproxy_error":
		return []string{"refresh_cliproxy_accounts"}
	default:
		return nil
	}
}

func (s *Service) OpsTrends(ctx context.Context, window string) (domain.OpsTrendResponse, error) {
	since, label, duration := opsWindow(window)
	probes, err := s.Store.ProbesSince(ctx, since)
	if err != nil {
		return domain.OpsTrendResponse{}, err
	}
	upstreams, err := s.Store.ListUpstreams(ctx)
	if err != nil {
		return domain.OpsTrendResponse{}, err
	}
	upstreamByID := map[string]domain.Upstream{}
	for _, u := range upstreams {
		upstreamByID[u.ID] = u
	}
	balances, err := s.Store.BalanceSnapshotsSince(ctx, "", since)
	if err != nil {
		return domain.OpsTrendResponse{}, err
	}
	revenue, err := s.Store.RevenueSnapshotsSince(ctx, since)
	if err != nil {
		return domain.OpsTrendResponse{}, err
	}
	quotas, err := s.Store.CLIProxyQuotaSnapshotsSince(ctx, since)
	if err != nil {
		return domain.OpsTrendResponse{}, err
	}
	out := domain.OpsTrendResponse{
		Window:         label,
		Probes:         probeTrend(probes, trendBucket(duration)),
		Revenue:        revenue,
		CLIProxyQuotas: quotas,
	}
	for _, snap := range balances {
		u := upstreamByID[snap.UpstreamID]
		_, _, remain := domain.ConvertedBalanceValues(u.Type, domain.BalanceRate(u), snap.Balance, snap.Used, snap.Remain)
		out.Balances = append(out.Balances, domain.OpsBalanceTrendPoint{At: snap.CheckedAt, UpstreamID: snap.UpstreamID, Name: u.Name, Remain: remain, Error: snap.Error})
	}
	return out, nil
}

func probeTrend(probes []domain.ProbeRun, bucket time.Duration) []domain.OpsProbeTrendPoint {
	type acc struct {
		total, success, latency, samples int
	}
	rows := map[time.Time]*acc{}
	for _, probe := range probes {
		at := probe.CheckedAt.UTC().Truncate(bucket)
		if rows[at] == nil {
			rows[at] = &acc{}
		}
		rows[at].total++
		if probe.Success {
			rows[at].success++
		}
		if probe.LatencyMS > 0 {
			rows[at].latency += probe.LatencyMS
			rows[at].samples++
		}
	}
	keys := make([]time.Time, 0, len(rows))
	for at := range rows {
		keys = append(keys, at)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].Before(keys[j]) })
	out := make([]domain.OpsProbeTrendPoint, 0, len(keys))
	for _, at := range keys {
		row := rows[at]
		out = append(out, domain.OpsProbeTrendPoint{
			At: at, Total: row.total, Success: row.success, Failed: row.total - row.success,
			SuccessRate: percent(row.success, row.total), AvgLatency: avg(row.latency, row.samples),
		})
	}
	return out
}

func trendBucket(duration time.Duration) time.Duration {
	if duration > 24*time.Hour {
		return 6 * time.Hour
	}
	return time.Hour
}

func (s *Service) Profit(ctx context.Context, window string) (domain.ProfitResponse, error) {
	since, label, _ := opsWindow(window)
	if label == "today" {
		_, _ = s.TodayRevenue(ctx)
	}
	revenueSnaps, err := s.Store.RevenueSnapshotsSince(ctx, since)
	if err != nil {
		return domain.ProfitResponse{}, err
	}
	latestRevenue := map[string]domain.RevenueSnapshot{}
	for _, snap := range revenueSnaps {
		key := firstNonEmpty(snap.SourceID, snap.SourceName, snap.ID)
		if snap.Error == "" && latestRevenue[key].CheckedAt.Before(snap.CheckedAt) {
			latestRevenue[key] = snap
		}
	}
	out := domain.ProfitResponse{Window: label, Note: "收入取各收入源最新快照，成本用余额下降估算；充值导致余额上升不计负成本。"}
	for _, snap := range latestRevenue {
		out.Revenue += snap.Revenue
	}
	upstreams, err := s.Store.ListUpstreams(ctx)
	if err != nil {
		return out, err
	}
	for _, u := range upstreams {
		snaps, err := s.Store.BalanceSnapshotsSince(ctx, u.ID, since)
		if err != nil {
			return out, err
		}
		cost := balanceCost(u, snaps)
		if cost > 0 {
			out.UpstreamCost = append(out.UpstreamCost, domain.ProfitCostRow{UpstreamID: u.ID, Name: u.Name, Cost: cost})
			out.Cost += cost
		}
	}
	out.Profit = out.Revenue - out.Cost
	return out, nil
}

func balanceCost(u domain.Upstream, snaps []domain.BalanceSnapshot) float64 {
	var cost float64
	var prev float64
	for i, snap := range snaps {
		_, _, remain := domain.ConvertedBalanceValues(u.Type, domain.BalanceRate(u), snap.Balance, snap.Used, snap.Remain)
		if i > 0 && remain < prev {
			cost += prev - remain
		}
		prev = remain
	}
	return cost
}

func (s *Service) SelfCheck(ctx context.Context) (domain.SelfCheckResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	out := domain.SelfCheckResponse{CheckedAt: time.Now().UTC()}
	out.Items = append(out.Items, checkItem("app", nil, "HTTP API 正常"))
	out.Items = append(out.Items, checkItem("database_writable", s.Store.CheckWritable(ctx), "数据库可写"))
	out.Items = append(out.Items, diskCheck())
	out.Items = append(out.Items, domain.SelfCheckItem{Name: "build_version", Status: "ok", Message: firstNonEmpty(os.Getenv("VITE_BUILD_VERSION"), "dev")})
	out.Items = append(out.Items, domain.SelfCheckItem{Name: "container_restart_count", Status: "safe_mode", Message: "安全模式未读取容器重启次数"})
	out.Items = append(out.Items, checkHTTP(ctx, s.Client.HTTP, "browser_http", envDefault("BROWSER_PROXY_URL", "http://127.0.0.1:6080")))
	out.Items = append(out.Items, checkHTTP(ctx, s.Client.HTTP, "browser_cdp", strings.TrimRight(envDefault("BROWSER_DEBUG_URL", "http://127.0.0.1:19222"), "/")+"/json/version"))
	out.Items = append(out.Items, s.cliProxySelfCheck(ctx))
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

func checkHTTP(ctx context.Context, hc *http.Client, name, rawurl string) domain.SelfCheckItem {
	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if hc == nil {
		hc = &http.Client{Timeout: 5 * time.Second}
	}
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rawurl, nil)
	if err != nil {
		return checkItem(name, err, "")
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

func (s *Service) cliProxySelfCheck(ctx context.Context) domain.SelfCheckItem {
	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cfg, err := s.Store.CLIProxyConfig(ctx)
	if err != nil {
		return checkItem("cliproxy_management", err, "")
	}
	if !cfg.Enabled {
		return domain.SelfCheckItem{Name: "cliproxy_management", Status: "warn", Message: "未启用"}
	}
	if strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.ManagementKey) == "" {
		return domain.SelfCheckItem{Name: "cliproxy_management", Status: "warn", Message: "未配置"}
	}
	_, _, err = s.cliProxyRequest(reqCtx, cfg, http.MethodGet, "/auth-files", nil, "")
	return checkItem("cliproxy_management", err, "管理接口可连通")
}

func checkItem(name string, err error, ok string) domain.SelfCheckItem {
	if err != nil {
		return domain.SelfCheckItem{Name: name, Status: "error", Message: err.Error()}
	}
	return domain.SelfCheckItem{Name: name, Status: "ok", Message: ok}
}

func opsWindow(window string) (time.Time, string, time.Duration) {
	now := time.Now()
	switch window {
	case "today":
		y, m, d := now.Date()
		start := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
		return start.UTC(), "today", now.Sub(start)
	case "7d":
		return now.UTC().Add(-7 * 24 * time.Hour), "7d", 7 * 24 * time.Hour
	default:
		return now.UTC().Add(-24 * time.Hour), "24h", 24 * time.Hour
	}
}

func envDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
