package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
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
	return s.sendTelegram(ctx, "通知规则测试")
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

func (s *Service) Profit(ctx context.Context, window string) (domain.ProfitResponse, error) {
	since, label, _ := opsWindow(window)
	cfg, err := s.Store.SchedulerConfig(ctx)
	if err != nil {
		return domain.ProfitResponse{}, err
	}
	out := domain.ProfitResponse{Window: label, Complete: true, Note: "按消费日志时间命中当时生效的售价/成本快照；旧日志没有快照覆盖时标为不完整。"}
	if strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.UserID) == "" || strings.TrimSpace(cfg.AccessToken) == "" {
		return out, ErrBadRequest("请先配置调度器连接")
	}
	tiers := domain.NormalizeSchedulerTiers(cfg.Tiers)
	end := time.Now().UTC()
	groups, tierMeta, err := s.profitGroups(ctx, tiers, end)
	if err != nil {
		return domain.ProfitResponse{}, err
	}
	pools := map[string]*profitPool{}
	upstreamCosts := map[string]domain.ProfitCostRow{}
	for _, group := range groups {
		logs, err := s.schedulerProfitLogs(ctx, cfg, since, end, group)
		if err != nil {
			return out, err
		}
		for _, log := range logs {
			group := strings.TrimSpace(firstNonEmpty(log.Group, group))
			pool := pools[group]
			if pool == nil {
				tier := tierMeta[group]
				pool = &profitPool{row: domain.ProfitPoolRow{Group: group, Tag: strings.TrimSpace(tier.Tag), SalePrice: tier.SalePrice, Complete: true}, channels: map[string]*domain.ProfitChannelRow{}}
				pools[group] = pool
			}
			units, ok := profitUsageUnits(log.Quota, log.GroupRatio)
			row := pool.channel(log.ChannelID, log.ChannelName)
			if !ok {
				markProfitIncomplete(row, pool, &out, "缺 group_ratio")
				continue
			}
			row.Usage += units
			pool.row.Usage += units
			if log.LogTime.IsZero() {
				markProfitIncomplete(row, pool, &out, "缺日志时间")
				continue
			}
			sale, ok, err := s.Store.SchedulerGroupSaleSnapshotAt(ctx, group, log.LogTime)
			if err != nil {
				return out, err
			}
			if !ok || !sale.Active || sale.SalePrice <= 0 {
				markProfitIncomplete(row, pool, &out, "缺售价快照")
				continue
			}
			pool.row.Tag = firstNonEmpty(pool.row.Tag, sale.Tag)
			pool.row.SalePrice = sale.SalePrice
			row.SaleEffective = mergeProfitMeta(row.SaleEffective, sale.EffectiveAt.Format(time.RFC3339Nano))
			revenue := units * sale.SalePrice
			row.Revenue += revenue
			costSnap, ok, err := s.Store.SchedulerChannelCostSnapshotAt(ctx, log.ChannelID, log.LogTime)
			if err != nil {
				return out, err
			}
			if !ok || !costSnap.Active || costSnap.CostPerUnit <= 0 {
				markProfitIncomplete(row, pool, &out, firstNonEmpty(costSnap.MissingReason, "缺成本快照"))
				pool.row.MissingRevenue += revenue
				out.MissingRevenue += revenue
				continue
			}
			row.ChannelName = firstNonEmpty(row.ChannelName, costSnap.ChannelName)
			row.CardID, row.CardName = costSnap.CardID, costSnap.CardName
			row.UpstreamID, row.UpstreamName = costSnap.UpstreamID, costSnap.UpstreamName
			row.KeyID, row.KeyName = costSnap.KeyID, costSnap.KeyName
			row.CostPerUnit = costSnap.CostPerUnit
			row.CostSource = mergeProfitMeta(row.CostSource, costSnap.SourceType)
			row.CostEffective = mergeProfitMeta(row.CostEffective, costSnap.EffectiveAt.Format(time.RFC3339Nano))
			cost := units * costSnap.CostPerUnit
			row.Cost += cost
			row.Profit = row.Revenue - row.Cost
			pool.row.Revenue += revenue
			pool.row.Cost += cost
			out.Revenue += revenue
			out.Cost += cost
			if costSnap.UpstreamID != "" {
				costRow := upstreamCosts[costSnap.UpstreamID]
				costRow.UpstreamID, costRow.Name = costSnap.UpstreamID, costSnap.UpstreamName
				costRow.Cost += cost
				upstreamCosts[costSnap.UpstreamID] = costRow
			}
		}
	}
	for _, pool := range pools {
		pool.row.Profit = pool.row.Revenue - pool.row.Cost
		for _, ch := range pool.channels {
			pool.row.Channels = append(pool.row.Channels, *ch)
		}
		sort.Slice(pool.row.Channels, func(i, j int) bool { return pool.row.Channels[i].ChannelID < pool.row.Channels[j].ChannelID })
		out.Pools = append(out.Pools, pool.row)
	}
	sort.Slice(out.Pools, func(i, j int) bool { return out.Pools[i].Group < out.Pools[j].Group })
	for _, row := range upstreamCosts {
		out.UpstreamCost = append(out.UpstreamCost, row)
	}
	sort.Slice(out.UpstreamCost, func(i, j int) bool { return out.UpstreamCost[i].Name < out.UpstreamCost[j].Name })
	out.Profit = out.Revenue - out.Cost
	return out, nil
}

type schedulerProfitLog struct {
	Group       string
	ChannelID   string
	ChannelName string
	Quota       float64
	GroupRatio  float64
	LogTime     time.Time
}

type profitPool struct {
	row      domain.ProfitPoolRow
	channels map[string]*domain.ProfitChannelRow
}

func (s *Service) profitGroups(ctx context.Context, tiers []domain.SchedulerTier, end time.Time) ([]string, map[string]domain.SchedulerTier, error) {
	seen := map[string]bool{}
	meta := map[string]domain.SchedulerTier{}
	for _, tier := range tiers {
		group := strings.TrimSpace(tier.Group)
		if group == "" {
			continue
		}
		seen[group] = true
		meta[group] = tier
	}
	snapshotGroups, err := s.Store.SchedulerSaleSnapshotGroups(ctx, end)
	if err != nil {
		return nil, nil, err
	}
	for _, group := range snapshotGroups {
		seen[group] = true
	}
	out := make([]string, 0, len(seen))
	for group := range seen {
		out = append(out, group)
	}
	sort.Strings(out)
	return out, meta, nil
}

func markProfitIncomplete(row *domain.ProfitChannelRow, pool *profitPool, out *domain.ProfitResponse, reason string) {
	row.Complete = false
	row.MissingReason = firstNonEmpty(row.MissingReason, reason)
	pool.row.Complete = false
	out.Complete = false
}

func mergeProfitMeta(old, next string) string {
	next = strings.TrimSpace(next)
	if old == "" {
		return next
	}
	if next == "" || old == next {
		return old
	}
	return "mixed"
}

func (p *profitPool) channel(id, name string) *domain.ProfitChannelRow {
	key := firstNonEmpty(id, name, "unknown")
	row := p.channels[key]
	if row == nil {
		row = &domain.ProfitChannelRow{ChannelID: id, ChannelName: name, Complete: true}
		p.channels[key] = row
	}
	return row
}

func (s *Service) schedulerProfitLogs(ctx context.Context, cfg domain.SchedulerConfig, start, end time.Time, group string) ([]schedulerProfitLog, error) {
	const pageSize = 100
	var out []schedulerProfitLog
	for page := 1; page <= 1000; page++ {
		values := url.Values{}
		values.Set("type", "2")
		values.Set("start_timestamp", strconv.FormatInt(start.Unix(), 10))
		values.Set("end_timestamp", strconv.FormatInt(end.Unix(), 10))
		values.Set("group", group)
		values.Set("p", strconv.Itoa(page))
		values.Set("page_size", strconv.Itoa(pageSize))
		var raw map[string]any
		if err := s.schedulerJSON(ctx, cfg, http.MethodGet, "/api/log/?"+values.Encode(), nil, &raw); err != nil {
			return nil, err
		}
		if ok, exists := raw["success"].(bool); exists && !ok {
			return nil, errors.New(schedulerMessage(raw))
		}
		items := profitLogItems(raw)
		for _, item := range items {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			other := profitMap(firstScheduler(m, "other"))
			groupRatio := profitFloat(firstScheduler(other, "group_ratio", "groupRatio", "ratio"))
			if groupRatio == 0 {
				groupRatio = profitFloat(firstScheduler(m, "group_ratio", "groupRatio", "ratio"))
			}
			out = append(out, schedulerProfitLog{
				Group:       firstNonEmpty(schedulerString(firstScheduler(m, "group")), group),
				ChannelID:   schedulerString(firstScheduler(m, "channel", "channel_id", "channelId")),
				ChannelName: schedulerString(firstScheduler(m, "channel_name", "channelName")),
				Quota:       profitFloat(firstScheduler(m, "quota")),
				GroupRatio:  groupRatio,
				LogTime:     profitTime(firstScheduler(m, "created_at", "createdAt", "created_time", "createdTime", "timestamp", "time", "created")),
			})
		}
		if len(items) < pageSize {
			return out, nil
		}
	}
	return out, errors.New("调度器日志分页超过 1000 页")
}

func profitUsageUnits(quota, groupRatio float64) (float64, bool) {
	if groupRatio <= 0 || quota < 0 {
		return 0, false
	}
	return quota / 500000 / groupRatio, true
}

func profitLogItems(raw map[string]any) []any {
	for _, key := range []string{"data", "items", "logs", "rows", "records", "list"} {
		if items := profitArray(raw[key]); len(items) != 0 {
			return items
		}
	}
	return nil
}

func profitArray(v any) []any {
	switch x := v.(type) {
	case []any:
		return x
	case map[string]any:
		for _, key := range []string{"items", "logs", "rows", "records", "list", "data"} {
			if items := profitArray(x[key]); len(items) != 0 {
				return items
			}
		}
	}
	return nil
}

func profitMap(v any) map[string]any {
	switch x := v.(type) {
	case map[string]any:
		return x
	case string:
		var out map[string]any
		_ = json.Unmarshal([]byte(x), &out)
		return out
	default:
		return nil
	}
}

func profitFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case json.Number:
		f, _ := x.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f
	default:
		return 0
	}
}

func profitTime(v any) time.Time {
	switch x := v.(type) {
	case float64:
		return unixProfitTime(x)
	case int:
		return unixProfitTime(float64(x))
	case int64:
		return unixProfitTime(float64(x))
	case json.Number:
		f, _ := x.Float64()
		return unixProfitTime(f)
	case string:
		x = strings.TrimSpace(x)
		if x == "" {
			return time.Time{}
		}
		if f, err := strconv.ParseFloat(x, 64); err == nil {
			return unixProfitTime(f)
		}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05.000Z"} {
			if t, err := time.Parse(layout, x); err == nil {
				return t.UTC()
			}
		}
		if t, err := time.ParseInLocation("2006-01-02 15:04:05", x, appLocation()); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

func unixProfitTime(v float64) time.Time {
	if v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return time.Time{}
	}
	if v > 1e12 {
		return time.UnixMilli(int64(v)).UTC()
	}
	return time.Unix(int64(v), int64((v-math.Floor(v))*1e9)).UTC()
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
	out.Items = append(out.Items, checkBrowserCDP(ctx, s.Client.HTTP, envDefault("BROWSER_DEBUG_URL", "http://127.0.0.1:19222")))
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
	now := time.Now().In(appLocation())
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
