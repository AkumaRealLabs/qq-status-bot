package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"ai-upstream-monitor/internal/app"
	"ai-upstream-monitor/internal/domain"
	"ai-upstream-monitor/internal/monitor"
	"ai-upstream-monitor/internal/store"

	"golang.org/x/net/websocket"
)

const (
	sessionCookie        = "monitor_session"
	defaultJSONBodyLimit = 1 << 20
	loginFailLimit       = 5
	loginFailWindow      = 15 * time.Minute
)

type Server struct {
	App        *app.Service
	Static     embed.FS
	loginMu    sync.Mutex
	loginFails map[string][]time.Time
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/setup/status", s.setupStatus)
	mux.HandleFunc("GET /api/public/settings", s.publicSettings)
	mux.HandleFunc("GET /api/public/monitor/status", s.publicMonitorStatus)
	mux.HandleFunc("POST /api/setup", s.setup)
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("POST /api/auth/logout", s.auth(s.logout))
	mux.HandleFunc("GET /api/auth/me", s.auth(s.me))
	mux.HandleFunc("GET /api/upstreams", s.auth(s.listUpstreams))
	mux.HandleFunc("POST /api/upstreams", s.auth(s.createUpstream))
	mux.HandleFunc("PATCH /api/upstreams/{id}", s.auth(s.updateUpstream))
	mux.HandleFunc("DELETE /api/upstreams/{id}", s.auth(s.deleteUpstream))
	mux.HandleFunc("POST /api/upstreams/{id}/check", s.auth(s.checkUpstream))
	mux.HandleFunc("POST /api/upstreams/{id}/sync-keys", s.auth(s.syncKeys))
	mux.HandleFunc("GET /api/upstreams/{id}/balance-recharge/capabilities", s.auth(s.balanceRechargeCapabilities))
	mux.HandleFunc("POST /api/upstreams/{id}/balance-recharge/redeem", s.auth(s.redeemBalance))
	mux.HandleFunc("POST /api/upstreams/{id}/balance-recharge/order", s.auth(s.createBalanceRechargeOrder))
	mux.HandleFunc("GET /api/upstreams/{id}/balance-recharge/logs", s.auth(s.balanceRechargeLogs))
	mux.HandleFunc("POST /api/upstreams/{id}/balance-recharge/logs/{log_id}/refresh", s.auth(s.refreshBalanceRechargeLog))
	mux.HandleFunc("DELETE /api/upstreams/{id}/balance-recharge/logs/{log_id}", s.auth(s.deleteBalanceRechargeLog))
	mux.HandleFunc("POST /api/upstreams/{id}/browser-login", s.auth(s.browserLogin))
	mux.HandleFunc("POST /api/upstreams/{id}/browser-capture", s.auth(s.browserCapture))
	mux.HandleFunc("GET /api/cards", s.auth(s.listCards))
	mux.HandleFunc("POST /api/cards", s.auth(s.createCard))
	mux.HandleFunc("POST /api/cards/order", s.auth(s.sortCards))
	mux.HandleFunc("PATCH /api/cards/{id}", s.auth(s.updateCard))
	mux.HandleFunc("DELETE /api/cards/{id}", s.auth(s.deleteCard))
	mux.HandleFunc("POST /api/cards/{id}/check", s.auth(s.checkCard))
	mux.HandleFunc("POST /api/cards/{id}/scheduler/status", s.auth(s.setCardSchedulerStatus))
	mux.HandleFunc("GET /api/scheduler/config", s.auth(s.schedulerConfig))
	mux.HandleFunc("PATCH /api/scheduler/config", s.auth(s.updateSchedulerConfig))
	mux.HandleFunc("GET /api/scheduler/groups", s.auth(s.schedulerGroups))
	mux.HandleFunc("GET /api/scheduler/channels", s.auth(s.schedulerChannels))
	mux.HandleFunc("GET /api/scheduler/logs", s.auth(s.schedulerLogs))
	mux.HandleFunc("POST /api/scheduler/groups/apply", s.auth(s.applySchedulerGroups))
	mux.HandleFunc("GET /api/pools/cliproxy/config", s.auth(s.cliProxyConfig))
	mux.HandleFunc("PATCH /api/pools/cliproxy/config", s.auth(s.updateCLIProxyConfig))
	mux.HandleFunc("GET /api/pools/cliproxy/accounts", s.auth(s.cliProxyAccounts))
	mux.HandleFunc("POST /api/pools/cliproxy/accounts", s.auth(s.uploadCLIProxyAccount))
	mux.HandleFunc("GET /api/pools/cliproxy/accounts/{name}/quota", s.auth(s.cliProxyAccountQuota))
	mux.HandleFunc("GET /api/pools/cliproxy/accounts/{name}/download", s.auth(s.downloadCLIProxyAccount))
	mux.HandleFunc("DELETE /api/pools/cliproxy/accounts/{name}", s.auth(s.deleteCLIProxyAccount))
	mux.HandleFunc("POST /api/pools/cliproxy/accounts/{name}/reset-quota", s.auth(s.resetCLIProxyQuota))
	mux.HandleFunc("GET /api/settings", s.auth(s.settings))
	mux.HandleFunc("PATCH /api/settings", s.auth(s.updateSettings))
	mux.HandleFunc("GET /api/settings/export", s.auth(s.exportData))
	mux.HandleFunc("POST /api/settings/import", s.auth(s.importData))
	mux.HandleFunc("GET /api/ops/events", s.auth(s.opsEvents))
	mux.HandleFunc("GET /api/ops/event-groups", s.auth(s.opsEventGroups))
	mux.HandleFunc("POST /api/ops/events/read", s.auth(s.markOpsEventsRead))
	mux.HandleFunc("POST /api/ops/events/ack", s.auth(s.ackOpsEvents))
	mux.HandleFunc("POST /api/ops/events/{id}/read", s.auth(s.markOpsEventRead))
	mux.HandleFunc("POST /api/ops/events/{id}/ack", s.auth(s.ackOpsEvent))
	mux.HandleFunc("GET /api/ops/audit", s.auth(s.auditLogs))
	mux.HandleFunc("GET /api/ops/notifications", s.auth(s.notificationRules))
	mux.HandleFunc("PATCH /api/ops/notifications", s.auth(s.updateNotificationRules))
	mux.HandleFunc("POST /api/ops/notifications/test", s.auth(s.testNotification))
	mux.HandleFunc("GET /api/ops/profit", s.auth(s.opsProfit))
	mux.HandleFunc("GET /api/ops/self-check", s.auth(s.opsSelfCheck))
	mux.HandleFunc("GET /api/monitor/status", s.auth(s.monitorStatus))
	mux.HandleFunc("GET /api/monitor/balances", s.auth(s.balances))
	mux.HandleFunc("POST /api/monitor/balances/refresh", s.auth(s.refreshBalances))
	mux.HandleFunc("GET /api/revenue/today", s.auth(s.todayRevenue))
	mux.HandleFunc("GET /api/revenue/cards", s.auth(s.listRevenueCards))
	mux.HandleFunc("GET /api/revenue/cards/{id}/orders", s.auth(s.revenueCardOrders))
	mux.HandleFunc("POST /api/revenue/cards", s.auth(s.createRevenueCard))
	mux.HandleFunc("POST /api/revenue/cards/order", s.auth(s.sortRevenueCards))
	mux.HandleFunc("PATCH /api/revenue/cards/{id}", s.auth(s.updateRevenueCard))
	mux.HandleFunc("DELETE /api/revenue/cards/{id}", s.auth(s.deleteRevenueCard))
	mux.HandleFunc("GET /api/tg/session/status", s.auth(s.tgSessionStatus))
	mux.HandleFunc("POST /api/tg/session/start", s.auth(s.startTGSession))
	mux.HandleFunc("POST /api/tg/session/verify", s.auth(s.verifyTGSession))
	mux.HandleFunc("POST /api/tg/session/password", s.auth(s.tgSessionPassword))
	mux.HandleFunc("GET /api/tg/channels", s.auth(s.listTGChannels))
	mux.HandleFunc("POST /api/tg/channels", s.auth(s.createTGChannel))
	mux.HandleFunc("PATCH /api/tg/channels/{id}", s.auth(s.updateTGChannel))
	mux.HandleFunc("DELETE /api/tg/channels/{id}", s.auth(s.deleteTGChannel))
	mux.HandleFunc("POST /api/tg/channels/sync", s.auth(s.syncTGChannels))
	mux.HandleFunc("GET /api/tg/messages", s.auth(s.listTGMessages))
	mux.HandleFunc("DELETE /api/tg/messages", s.auth(s.clearTGMessages))
	mux.HandleFunc("POST /api/tg/messages/refresh", s.auth(s.refreshTGMessages))
	mux.HandleFunc("DELETE /api/tg/messages/{id}", s.auth(s.deleteTGMessage))
	mux.HandleFunc("GET /api/tg/media/{name}", s.auth(s.tgMedia))
	mux.HandleFunc("/browser/", s.auth(s.proxyBrowser))
	mux.HandleFunc("/websockify", s.auth(s.proxyBrowser))
	mux.HandleFunc("GET /admin", redirectTo("/admin/status"))
	mux.HandleFunc("GET /admin/merchant-balance", redirectTo("/admin/revenue"))
	mux.HandleFunc("GET /admin/ops", redirectTo("/admin/events"))
	mux.HandleFunc("GET /status", redirectTo("/admin/status"))
	mux.HandleFunc("GET /balances", redirectTo("/admin/balances"))
	mux.HandleFunc("GET /revenue", redirectTo("/admin/revenue"))
	mux.HandleFunc("GET /merchant-balance", redirectTo("/admin/revenue"))
	mux.HandleFunc("GET /messages", redirectTo("/admin/messages"))
	mux.HandleFunc("GET /upstreams", redirectTo("/admin/upstreams"))
	mux.HandleFunc("GET /scheduler", redirectTo("/admin/scheduler"))
	mux.HandleFunc("GET /pools", redirectTo("/admin/pools"))
	mux.HandleFunc("GET /ops", redirectTo("/admin/events"))
	mux.HandleFunc("GET /settings", redirectTo("/admin/settings"))
	mux.Handle("/", s.static())
	return s.checkOrigin(mux)
}

func (s *Server) checkOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		origin := r.Header.Get("Origin")
		if origin == "" || sameOrigin(origin, r) {
			next.ServeHTTP(w, r)
			return
		}
		writeError(w, http.StatusForbidden, "bad origin")
	})
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil || c.Value == "" {
			writeError(w, http.StatusUnauthorized, "未登录")
			return
		}
		u, err := s.App.Me(r.Context(), c.Value)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "登录已过期")
			return
		}
		if !shouldAuditRequest(r) {
			next(w, r)
			return
		}
		body, fields := auditBodyFields(r)
		rr := &auditResponseWriter{ResponseWriter: w}
		next(rr, r)
		if rr.status == 0 {
			rr.status = http.StatusOK
		}
		if rr.status < http.StatusBadRequest {
			targetType, targetID := auditTarget(r)
			_ = s.App.Store.CreateAudit(r.Context(), domain.AuditLog{
				Actor: u.Username, Action: auditAction(r), TargetType: targetType, TargetID: targetID,
				Summary: auditSummary(r, body), Fields: fields,
			})
		}
	}
}

type auditResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *auditResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *auditResponseWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func shouldAuditRequest(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, "/api/") && r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions
}

func auditBodyFields(r *http.Request) (string, []string) {
	if r.Body == nil || !strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "json") {
		return "", nil
	}
	if r.ContentLength > defaultJSONBodyLimit {
		return "", nil
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, defaultJSONBodyLimit))
	if err != nil {
		r.Body = io.NopCloser(bytes.NewReader(nil))
		return "", nil
	}
	r.Body = io.NopCloser(bytes.NewReader(b))
	if len(b) == 0 {
		return "", nil
	}
	var raw any
	if json.Unmarshal(b, &raw) != nil {
		return "", nil
	}
	seen := map[string]bool{}
	collectJSONFields("", raw, seen)
	fields := make([]string, 0, len(seen))
	for field := range seen {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	return string(b), fields
}

func collectJSONFields(prefix string, value any, seen map[string]bool) {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			seen[path] = true
			collectJSONFields(path, child, seen)
		}
	case []any:
		for _, child := range v {
			collectJSONFields(prefix, child, seen)
		}
	}
}

func auditAction(r *http.Request) string {
	pattern := r.Pattern
	if pattern == "" {
		pattern = r.URL.Path
	}
	if strings.HasPrefix(pattern, r.Method+" ") {
		return pattern
	}
	return r.Method + " " + pattern
}

func auditTarget(r *http.Request) (string, string) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	targetType := ""
	if len(parts) >= 2 {
		targetType = parts[1]
	}
	for _, key := range []string{"id", "name", "log_id"} {
		if v := r.PathValue(key); v != "" {
			return targetType, v
		}
	}
	return targetType, ""
}

func auditSummary(r *http.Request, body string) string {
	if body == "" {
		return "no json body"
	}
	var raw any
	if json.Unmarshal([]byte(body), &raw) != nil {
		return "json body"
	}
	return "json fields recorded; values omitted"
}

func sameOrigin(origin string, r *http.Request) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	for _, host := range []string{r.Host, firstHeader(r.Header.Get("X-Forwarded-Host"))} {
		if host != "" && strings.EqualFold(u.Host, host) {
			return true
		}
	}
	return false
}

func secureCookie(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(firstHeader(r.Header.Get("X-Forwarded-Proto")), "https") || strings.EqualFold(r.Header.Get("X-Forwarded-Ssl"), "on")
}

func firstHeader(v string) string {
	if i := strings.IndexByte(v, ','); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

func loginKey(r *http.Request, username string) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || host == "" {
		host = r.RemoteAddr
	}
	return host + "|" + strings.ToLower(strings.TrimSpace(username))
}

func (s *Server) loginLimited(key string) bool {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	now := time.Now()
	fails := pruneLoginFailures(s.loginFails[key], now)
	if s.loginFails != nil {
		s.loginFails[key] = fails
	}
	return len(fails) >= loginFailLimit
}

func (s *Server) recordLoginFailure(key string) {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	now := time.Now()
	if s.loginFails == nil {
		s.loginFails = map[string][]time.Time{}
	}
	s.loginFails[key] = append(pruneLoginFailures(s.loginFails[key], now), now)
}

func (s *Server) clearLoginFailures(key string) {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	delete(s.loginFails, key)
}

func pruneLoginFailures(fails []time.Time, now time.Time) []time.Time {
	cutoff := now.Add(-loginFailWindow)
	i := 0
	for ; i < len(fails); i++ {
		if fails[i].After(cutoff) {
			break
		}
	}
	return fails[i:]
}

func (s *Server) setupStatus(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.SetupStatus(r.Context())
	writeJSONOrError(w, out, err)
}

func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &body) {
		return
	}
	u, err := s.App.Setup(r.Context(), strings.TrimSpace(body.Username), body.Password)
	writeJSONOrError(w, map[string]any{"user": u}, err)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &body) {
		return
	}
	key := loginKey(r, body.Username)
	if s.loginLimited(key) {
		writeError(w, http.StatusTooManyRequests, "too many login attempts")
		return
	}
	token, u, err := s.App.Login(r.Context(), body.Username, body.Password)
	if err != nil {
		s.recordLoginFailure(key)
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	s.clearLoginFailures(key)
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: secureCookie(r), Expires: time.Now().Add(30 * 24 * time.Hour),
	})
	writeJSON(w, map[string]any{"user": u})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = s.App.Logout(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: secureCookie(r)})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	c, _ := r.Cookie(sessionCookie)
	u, err := s.App.Me(r.Context(), c.Value)
	writeJSONOrError(w, map[string]any{"user": u}, err)
}

func (s *Server) listUpstreams(w http.ResponseWriter, r *http.Request) {
	rows, err := s.App.UpstreamRows(r.Context())
	writeJSONOrError(w, rows, err)
}

func (s *Server) createUpstream(w http.ResponseWriter, r *http.Request) {
	var body domain.Upstream
	if !decode(w, r, &body) {
		return
	}
	u, err := s.App.SaveUpstream(r.Context(), "", body)
	writeJSONOrError(w, u, err)
}

func (s *Server) updateUpstream(w http.ResponseWriter, r *http.Request) {
	var body domain.Upstream
	if !decode(w, r, &body) {
		return
	}
	u, err := s.App.SaveUpstream(r.Context(), r.PathValue("id"), body)
	writeJSONOrError(w, u, err)
}

func (s *Server) deleteUpstream(w http.ResponseWriter, r *http.Request) {
	err := s.App.Store.DeleteUpstream(r.Context(), r.PathValue("id"))
	writeNoContentOrError(w, err)
}

func (s *Server) checkUpstream(w http.ResponseWriter, r *http.Request) {
	writeNoContentOrError(w, s.App.CheckUpstream(r.Context(), r.PathValue("id")))
}

func (s *Server) syncKeys(w http.ResponseWriter, r *http.Request) {
	writeNoContentOrError(w, s.App.SyncKeys(r.Context(), r.PathValue("id")))
}

func (s *Server) balanceRechargeCapabilities(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.BalanceRechargeCapabilities(r.Context(), r.PathValue("id"))
	writeJSONOrError(w, out, err)
}

func (s *Server) redeemBalance(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code string `json:"code"`
	}
	if !decode(w, r, &body) {
		return
	}
	out, err := s.App.RedeemBalance(r.Context(), r.PathValue("id"), body.Code)
	writeJSONOrError(w, out, err)
}

func (s *Server) createBalanceRechargeOrder(w http.ResponseWriter, r *http.Request) {
	var body monitor.RechargeOrderRequest
	if !decode(w, r, &body) {
		return
	}
	out, err := s.App.CreateBalanceRechargeOrder(r.Context(), r.PathValue("id"), body)
	writeJSONOrError(w, out, err)
}

func (s *Server) balanceRechargeLogs(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.BalanceRechargeLogs(r.Context(), r.PathValue("id"))
	writeJSONOrError(w, out, err)
}

func (s *Server) refreshBalanceRechargeLog(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.RefreshBalanceRechargeLog(r.Context(), r.PathValue("id"), r.PathValue("log_id"))
	writeJSONOrError(w, out, err)
}

func (s *Server) deleteBalanceRechargeLog(w http.ResponseWriter, r *http.Request) {
	err := s.App.DeleteBalanceRechargeLog(r.Context(), r.PathValue("id"), r.PathValue("log_id"))
	writeNoContentOrError(w, err)
}

func (s *Server) listCards(w http.ResponseWriter, r *http.Request) {
	rows, err := s.App.ListCards(r.Context())
	writeJSONOrError(w, rows, err)
}

func (s *Server) createCard(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name                 string `json:"name"`
		BaseURL              string `json:"base_url"`
		APIKey               string `json:"api_key"`
		UpstreamID           string `json:"upstream_id"`
		KeyID                string `json:"key_id"`
		DisplayGroup         string `json:"display_group"`
		PoolEnabled          *bool  `json:"pool_enabled"`
		ManualCostRatio      string `json:"manual_cost_ratio"`
		SchedulerGroup       string `json:"scheduler_group"`
		SchedulerChannelID   string `json:"scheduler_channel_id"`
		SchedulerChannelName string `json:"scheduler_channel_name"`
		Enabled              *bool  `json:"enabled"`
		PublicEnabled        *bool  `json:"public_enabled"`
	}
	if !decode(w, r, &body) {
		return
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	publicEnabled := false
	if body.PublicEnabled != nil {
		publicEnabled = *body.PublicEnabled
	}
	poolEnabled := true
	if body.PoolEnabled != nil {
		poolEnabled = *body.PoolEnabled
	}
	card, err := s.App.SaveCard(r.Context(), "", domain.ModelCard{
		Name: body.Name, BaseURL: body.BaseURL, APIKey: body.APIKey, UpstreamID: body.UpstreamID, KeyID: body.KeyID,
		DisplayGroup: body.DisplayGroup, PoolEnabled: poolEnabled, PoolEnabledSet: true, ManualCostRatio: body.ManualCostRatio,
		SchedulerGroup: body.SchedulerGroup, SchedulerChannelID: body.SchedulerChannelID, SchedulerChannelName: body.SchedulerChannelName, Enabled: enabled, PublicEnabled: publicEnabled,
	})
	writeJSONOrError(w, card, err)
}

func (s *Server) updateCard(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name                 string  `json:"name"`
		BaseURL              string  `json:"base_url"`
		APIKey               string  `json:"api_key"`
		UpstreamID           string  `json:"upstream_id"`
		KeyID                string  `json:"key_id"`
		DisplayGroup         *string `json:"display_group"`
		PoolEnabled          *bool   `json:"pool_enabled"`
		ManualCostRatio      *string `json:"manual_cost_ratio"`
		SchedulerGroup       *string `json:"scheduler_group"`
		SchedulerChannelID   *string `json:"scheduler_channel_id"`
		SchedulerChannelName *string `json:"scheduler_channel_name"`
		Enabled              *bool   `json:"enabled"`
		PublicEnabled        *bool   `json:"public_enabled"`
	}
	if !decode(w, r, &body) {
		return
	}
	old, err := s.App.Store.Card(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	enabled := old.Enabled
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	publicEnabled := old.PublicEnabled
	if body.PublicEnabled != nil {
		publicEnabled = *body.PublicEnabled
	}
	name, baseURL, apiKey, upstreamID, keyID := body.Name, body.BaseURL, body.APIKey, body.UpstreamID, body.KeyID
	displayGroup := old.DisplayGroup
	if name == "" {
		name = old.Name
	}
	if body.DisplayGroup != nil {
		displayGroup = *body.DisplayGroup
	}
	poolEnabled, manualCostRatio := old.PoolEnabled, old.ManualCostRatio
	if body.PoolEnabled != nil {
		poolEnabled = *body.PoolEnabled
	}
	if body.ManualCostRatio != nil {
		manualCostRatio = *body.ManualCostRatio
	}
	schedulerGroup, schedulerChannelID, schedulerChannelName, schedulerAutoDisabled := old.SchedulerGroup, old.SchedulerChannelID, old.SchedulerChannelName, old.SchedulerAutoDisabled
	if body.SchedulerGroup != nil {
		schedulerGroup = *body.SchedulerGroup
		if strings.TrimSpace(schedulerGroup) == "" || strings.TrimSpace(schedulerGroup) != old.SchedulerGroup {
			schedulerChannelID, schedulerChannelName, schedulerAutoDisabled = "", "", false
		}
	}
	if body.SchedulerChannelID != nil {
		oldChannelID := schedulerChannelID
		schedulerChannelID = *body.SchedulerChannelID
		if strings.TrimSpace(schedulerChannelID) == "" || schedulerChannelID != oldChannelID {
			schedulerAutoDisabled = false
		}
	}
	if body.SchedulerChannelName != nil {
		schedulerChannelName = *body.SchedulerChannelName
	}
	if baseURL == "" && apiKey == "" && upstreamID == "" && keyID == "" {
		baseURL, apiKey, upstreamID, keyID = old.BaseURL, old.APIKey, old.UpstreamID, old.KeyID
	}
	card, err := s.App.SaveCard(r.Context(), r.PathValue("id"), domain.ModelCard{
		Name: name, BaseURL: baseURL, APIKey: apiKey, UpstreamID: upstreamID, KeyID: keyID,
		DisplayGroup: displayGroup, PoolEnabled: poolEnabled, PoolEnabledSet: true, ManualCostRatio: manualCostRatio,
		SchedulerGroup: schedulerGroup, SchedulerChannelID: schedulerChannelID, SchedulerChannelName: schedulerChannelName, SchedulerAutoDisabled: schedulerAutoDisabled, Enabled: enabled, PublicEnabled: publicEnabled,
	})
	writeJSONOrError(w, card, err)
}

func (s *Server) deleteCard(w http.ResponseWriter, r *http.Request) {
	writeNoContentOrError(w, s.App.DeleteCard(r.Context(), r.PathValue("id")))
}

func (s *Server) sortCards(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if !decode(w, r, &body) {
		return
	}
	writeNoContentOrError(w, s.App.SortCards(r.Context(), body.IDs))
}

func (s *Server) checkCard(w http.ResponseWriter, r *http.Request) {
	writeNoContentOrError(w, s.App.CheckCard(r.Context(), r.PathValue("id")))
}

func (s *Server) schedulerConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.App.SchedulerConfig(r.Context())
	writeJSONOrError(w, cfg, err)
}

func (s *Server) updateSchedulerConfig(w http.ResponseWriter, r *http.Request) {
	var cfg domain.SchedulerConfig
	if !decode(w, r, &cfg) {
		return
	}
	cfg, err := s.App.SaveSchedulerConfig(r.Context(), cfg)
	writeJSONOrError(w, cfg, err)
}

func (s *Server) schedulerChannels(w http.ResponseWriter, r *http.Request) {
	rows, err := s.App.SchedulerChannels(r.Context(), r.URL.Query().Get("keyword"))
	writeJSONOrError(w, rows, err)
}

func (s *Server) schedulerGroups(w http.ResponseWriter, r *http.Request) {
	rows, err := s.App.SchedulerGroups(r.Context())
	writeJSONOrError(w, rows, err)
}

func (s *Server) schedulerLogs(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	rows, err := s.App.SchedulerLogs(r.Context(), limit)
	writeJSONOrError(w, rows, err)
}

func (s *Server) applySchedulerGroups(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.ApplySchedulerGroups(r.Context())
	writeJSONOrError(w, out, err)
}

func (s *Server) setCardSchedulerStatus(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Status int `json:"status"`
	}
	if !decode(w, r, &body) {
		return
	}
	card, err := s.App.SetCardSchedulerChannelStatus(r.Context(), r.PathValue("id"), body.Status)
	writeJSONOrError(w, card, err)
}

func (s *Server) cliProxyConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.App.CLIProxyConfig(r.Context())
	writeJSONOrError(w, cfg, err)
}

func (s *Server) updateCLIProxyConfig(w http.ResponseWriter, r *http.Request) {
	var cfg domain.CLIProxyConfig
	if !decode(w, r, &cfg) {
		return
	}
	cfg, err := s.App.SaveCLIProxyConfig(r.Context(), cfg)
	writeJSONOrError(w, cfg, err)
}

func (s *Server) cliProxyAccounts(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.CLIProxyAccounts(r.Context())
	writeJSONOrError(w, out, err)
}

func (s *Server) cliProxyAccountQuota(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	out, err := s.App.CLIProxyAccountQuota(r.Context(), r.PathValue("name"), q.Get("auth_index"), q.Get("account"), q.Get("account_type"))
	writeJSONOrError(w, out, err)
}

func (s *Server) uploadCLIProxyAccount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if !decode(w, r, &body) {
		return
	}
	writeNoContentOrError(w, s.App.UploadCLIProxyAccount(r.Context(), body.Name, body.Content))
}

func (s *Server) downloadCLIProxyAccount(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	b, contentType, err := s.App.DownloadCLIProxyAccount(r.Context(), name)
	if err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	if contentType == "" {
		contentType = "application/json; charset=utf-8"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+strings.ReplaceAll(name, `"`, "")+`"`)
	_, _ = w.Write(b)
}

func (s *Server) deleteCLIProxyAccount(w http.ResponseWriter, r *http.Request) {
	writeNoContentOrError(w, s.App.DeleteCLIProxyAccount(r.Context(), r.PathValue("name")))
}

func (s *Server) resetCLIProxyQuota(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.ResetCLIProxyQuota(r.Context(), r.PathValue("name"))
	writeJSONOrError(w, out, err)
}

func (s *Server) settings(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.App.Store.Settings(r.Context())
	writeJSONOrError(w, cfg, err)
}

func (s *Server) publicSettings(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.App.Store.Settings(r.Context())
	if err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	writeJSON(w, map[string]string{"site_name": cfg.SiteName, "site_icon": cfg.SiteIcon})
}

func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) {
	var cfg domain.Settings
	if !decode(w, r, &cfg) {
		return
	}
	cfg, err := s.App.Store.UpdateSettings(r.Context(), cfg)
	writeJSONOrError(w, cfg, err)
}

func (s *Server) exportData(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.Store.ExportData(r.Context())
	if err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="ai-upstream-monitor-sensitive-export.json"`)
	w.Header().Set("X-Backup-Contains-Secrets", "true")
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) importData(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<20)
	var in store.ExportData
	if !decodeJSON(w, r, &in, 0) {
		return
	}
	writeNoContentOrError(w, s.App.Store.ImportData(r.Context(), in))
}

func (s *Server) opsEvents(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.OpsEvents(r.Context(), opsEventFilterFromQuery(r))
	writeJSONOrError(w, out, err)
}

func (s *Server) opsEventGroups(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.OpsEventGroups(r.Context(), opsEventFilterFromQuery(r))
	writeJSONOrError(w, out, err)
}

func (s *Server) markOpsEventRead(w http.ResponseWriter, r *http.Request) {
	writeNoContentOrError(w, s.App.Store.MarkOpsEventRead(r.Context(), r.PathValue("id")))
}

func (s *Server) ackOpsEvent(w http.ResponseWriter, r *http.Request) {
	writeNoContentOrError(w, s.App.Store.AckOpsEvent(r.Context(), r.PathValue("id")))
}

func (s *Server) markOpsEventsRead(w http.ResponseWriter, r *http.Request) {
	filter, ok := opsEventFilterFromBody(w, r)
	if !ok {
		return
	}
	writeNoContentOrError(w, s.App.Store.MarkOpsEventsRead(r.Context(), filter))
}

func (s *Server) ackOpsEvents(w http.ResponseWriter, r *http.Request) {
	filter, ok := opsEventFilterFromBody(w, r)
	if !ok {
		return
	}
	writeNoContentOrError(w, s.App.Store.AckOpsEvents(r.Context(), filter))
}

func opsEventFilterFromQuery(r *http.Request) domain.OpsEventFilter {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	return domain.OpsEventFilter{
		Type:       r.URL.Query().Get("type"),
		State:      r.URL.Query().Get("state"),
		TargetType: r.URL.Query().Get("target_type"),
		TargetID:   r.URL.Query().Get("target_id"),
		Limit:      limit,
	}
}

func opsEventFilterFromBody(w http.ResponseWriter, r *http.Request) (domain.OpsEventFilter, bool) {
	var filter domain.OpsEventFilter
	if !decode(w, r, &filter) {
		return filter, false
	}
	return filter, true
}

func (s *Server) auditLogs(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	out, err := s.App.AuditLogs(r.Context(), r.URL.Query().Get("action"), r.URL.Query().Get("target"), limit)
	writeJSONOrError(w, out, err)
}

func (s *Server) notificationRules(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.NotificationRules(r.Context())
	writeJSONOrError(w, out, err)
}

func (s *Server) updateNotificationRules(w http.ResponseWriter, r *http.Request) {
	var rules domain.NotificationRules
	if !decode(w, r, &rules) {
		return
	}
	out, err := s.App.SaveNotificationRules(r.Context(), rules)
	writeJSONOrError(w, out, err)
}

func (s *Server) testNotification(w http.ResponseWriter, r *http.Request) {
	writeNoContentOrError(w, s.App.TestNotification(r.Context()))
}

func (s *Server) opsProfit(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.Profit(r.Context(), r.URL.Query().Get("window"))
	writeJSONOrError(w, out, err)
}

func (s *Server) opsSelfCheck(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.SelfCheck(r.Context())
	writeJSONOrError(w, out, err)
}

func (s *Server) monitorStatus(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.MonitorStatus(r.Context(), r.URL.Query().Get("window"))
	writeJSONOrError(w, out, err)
}

func (s *Server) publicMonitorStatus(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.PublicMonitorStatus(r.Context(), r.URL.Query().Get("window"))
	writeJSONOrError(w, out, err)
}

func (s *Server) balances(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.BalanceRows(r.Context())
	writeJSONOrError(w, out, err)
}

func (s *Server) refreshBalances(w http.ResponseWriter, r *http.Request) {
	writeNoContentOrError(w, s.App.RefreshBalances(r.Context()))
}

func (s *Server) todayRevenue(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.TodayRevenue(r.Context())
	writeJSONOrError(w, out, err)
}

func (s *Server) listRevenueCards(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.ListRevenueCards(r.Context())
	writeJSONOrError(w, out, err)
}

func (s *Server) revenueCardOrders(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.RevenueCardOrders(r.Context(), r.PathValue("id"))
	writeJSONOrError(w, out, err)
}

func (s *Server) createRevenueCard(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string `json:"name"`
		SourceType  string `json:"source_type"`
		BaseURL     string `json:"base_url"`
		UserID      string `json:"user_id"`
		AccessToken string `json:"access_token"`
		AdminAPIKey string `json:"admin_api_key"`
		EpayPID     string `json:"epay_pid"`
		EpayKey     string `json:"epay_key"`
		UpstreamID  string `json:"upstream_id"`
		Enabled     *bool  `json:"enabled"`
	}
	if !decode(w, r, &body) {
		return
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	card, err := s.App.SaveRevenueCard(r.Context(), "", domain.RevenueCard{
		Name: body.Name, SourceType: body.SourceType, BaseURL: body.BaseURL, UserID: body.UserID, AccessToken: body.AccessToken,
		AdminAPIKey: body.AdminAPIKey, EpayPID: body.EpayPID, EpayKey: body.EpayKey, UpstreamID: body.UpstreamID, Enabled: enabled,
	})
	writeJSONOrError(w, card, err)
}

func (s *Server) updateRevenueCard(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string `json:"name"`
		SourceType  string `json:"source_type"`
		BaseURL     string `json:"base_url"`
		UserID      string `json:"user_id"`
		AccessToken string `json:"access_token"`
		AdminAPIKey string `json:"admin_api_key"`
		EpayPID     string `json:"epay_pid"`
		EpayKey     string `json:"epay_key"`
		UpstreamID  string `json:"upstream_id"`
		Enabled     *bool  `json:"enabled"`
	}
	if !decode(w, r, &body) {
		return
	}
	old, err := s.App.Store.RevenueCard(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	enabled := old.Enabled
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	name, sourceType, upstreamID := body.Name, body.SourceType, body.UpstreamID
	if name == "" {
		name = old.Name
	}
	if sourceType == "" {
		sourceType = old.SourceType
	}
	hasCardConfig := body.BaseURL != "" || body.UserID != "" || body.AccessToken != "" || body.AdminAPIKey != "" || body.EpayPID != "" || body.EpayKey != ""
	if upstreamID == "" && sourceType == old.SourceType && !hasCardConfig {
		upstreamID = old.UpstreamID
	}
	in := domain.RevenueCard{
		Name: name, SourceType: sourceType, BaseURL: body.BaseURL, UserID: body.UserID, AccessToken: body.AccessToken,
		AdminAPIKey: body.AdminAPIKey, EpayPID: body.EpayPID, EpayKey: body.EpayKey, UpstreamID: upstreamID, Enabled: enabled,
	}
	if !hasCardConfig && sourceType == old.SourceType {
		in.BaseURL, in.UserID, in.AccessToken, in.AdminAPIKey, in.EpayPID, in.EpayKey = old.BaseURL, old.UserID, old.AccessToken, old.AdminAPIKey, old.EpayPID, old.EpayKey
	}
	card, err := s.App.SaveRevenueCard(r.Context(), r.PathValue("id"), in)
	writeJSONOrError(w, card, err)
}

func (s *Server) deleteRevenueCard(w http.ResponseWriter, r *http.Request) {
	writeNoContentOrError(w, s.App.Store.DeleteRevenueCard(r.Context(), r.PathValue("id")))
}

func (s *Server) sortRevenueCards(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if !decode(w, r, &body) {
		return
	}
	writeNoContentOrError(w, s.App.SortRevenueCards(r.Context(), body.IDs))
}

func (s *Server) tgSessionStatus(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.TGSessionStatus(r.Context())
	writeJSONOrError(w, out, err)
}

func (s *Server) startTGSession(w http.ResponseWriter, r *http.Request) {
	var body struct {
		APIID   int    `json:"api_id"`
		APIHash string `json:"api_hash"`
		Phone   string `json:"phone"`
	}
	if !decode(w, r, &body) {
		return
	}
	out, err := s.App.StartTGSession(r.Context(), body.APIID, body.APIHash, body.Phone)
	writeJSONOrError(w, out, err)
}

func (s *Server) verifyTGSession(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code string `json:"code"`
	}
	if !decode(w, r, &body) {
		return
	}
	out, err := s.App.VerifyTGSession(r.Context(), body.Code)
	writeJSONOrError(w, out, err)
}

func (s *Server) tgSessionPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if !decode(w, r, &body) {
		return
	}
	out, err := s.App.TGSessionPassword(r.Context(), body.Password)
	writeJSONOrError(w, out, err)
}

func (s *Server) listTGChannels(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.Store.ListTGChannels(r.Context())
	writeJSONOrError(w, out, err)
}

func (s *Server) createTGChannel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DisplayName  string `json:"display_name"`
		Identifier   string `json:"identifier"`
		Enabled      *bool  `json:"enabled"`
		MessageLimit int    `json:"message_limit"`
		PinnedOnly   *bool  `json:"pinned_only"`
	}
	if !decode(w, r, &body) {
		return
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	pinnedOnly := false
	if body.PinnedOnly != nil {
		pinnedOnly = *body.PinnedOnly
	}
	out, err := s.App.SaveTGChannel(r.Context(), "", domain.TGChannel{
		DisplayName: body.DisplayName, Identifier: body.Identifier, Enabled: enabled, MessageLimit: body.MessageLimit, PinnedOnly: pinnedOnly,
	})
	writeJSONOrError(w, out, err)
}

func (s *Server) updateTGChannel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DisplayName  string `json:"display_name"`
		Enabled      *bool  `json:"enabled"`
		MessageLimit int    `json:"message_limit"`
		PinnedOnly   *bool  `json:"pinned_only"`
	}
	if !decode(w, r, &body) {
		return
	}
	old, err := s.App.Store.TGChannel(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	enabled := old.Enabled
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	if body.DisplayName == "" {
		body.DisplayName = old.DisplayName
	}
	if body.MessageLimit == 0 {
		body.MessageLimit = old.MessageLimit
	}
	pinnedOnly := old.PinnedOnly
	if body.PinnedOnly != nil {
		pinnedOnly = *body.PinnedOnly
	}
	out, err := s.App.SaveTGChannel(r.Context(), old.ID, domain.TGChannel{
		DisplayName: body.DisplayName, Identifier: old.Identifier, Username: old.Username, PeerID: old.PeerID, AccessHash: old.AccessHash,
		AvatarURL: old.AvatarURL, Enabled: enabled, MessageLimit: body.MessageLimit, PinnedOnly: pinnedOnly,
	})
	writeJSONOrError(w, out, err)
}

func (s *Server) deleteTGChannel(w http.ResponseWriter, r *http.Request) {
	writeNoContentOrError(w, s.App.Store.DeleteTGChannel(r.Context(), r.PathValue("id")))
}

func (s *Server) syncTGChannels(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.SyncTGChannels(r.Context())
	writeJSONOrError(w, out, err)
}

func (s *Server) listTGMessages(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	out, err := s.App.Store.TGMessages(r.Context(), r.URL.Query().Get("channel_id"), limit)
	writeJSONOrError(w, out, err)
}

func (s *Server) refreshTGMessages(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ChannelID string `json:"channel_id"`
	}
	if r.Body != nil && r.ContentLength != 0 && !decode(w, r, &body) {
		return
	}
	writeNoContentOrError(w, s.App.RefreshTGMessages(r.Context(), body.ChannelID))
}

func (s *Server) clearTGMessages(w http.ResponseWriter, r *http.Request) {
	writeNoContentOrError(w, s.App.Store.DeleteAllTGMessages(r.Context()))
}

func (s *Server) deleteTGMessage(w http.ResponseWriter, r *http.Request) {
	writeNoContentOrError(w, s.App.Store.DeleteTGMessage(r.Context(), r.PathValue("id")))
}

func (s *Server) tgMedia(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" || name != filepath.Base(name) {
		writeError(w, http.StatusBadRequest, "bad media name")
		return
	}
	http.ServeFile(w, r, filepath.Join(s.App.TGMediaDir, name))
}

func (s *Server) browserLogin(w http.ResponseWriter, r *http.Request) {
	u, err := s.App.Store.Upstream(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	if err := openBrowserURL(u.BaseURL); err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	writeJSON(w, map[string]any{"url": u.BaseURL, "vnc_url": browserVNCURL()})
}

func (s *Server) browserCapture(w http.ResponseWriter, r *http.Request) {
	u, err := s.App.Store.Upstream(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	tab, err := browserTab(u.BaseURL)
	if err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	access, refresh, err := readBrowserTokens(tab)
	if err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	if access == "" && refresh == "" {
		writeError(w, http.StatusBadRequest, "没有在浏览器 localStorage/sessionStorage/cookie 里找到 token")
		return
	}
	if access != "" {
		u.Sub2APIAccessToken = access
	}
	if refresh != "" {
		u.Sub2APIRefreshToken = refresh
	}
	_, err = s.App.Store.UpdateUpstream(r.Context(), u)
	writeJSONOrError(w, map[string]bool{"access_token": access != "", "refresh_token": refresh != ""}, err)
}

func decode(w http.ResponseWriter, r *http.Request, out any) bool {
	return decodeJSON(w, r, out, defaultJSONBodyLimit)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, out any, limit int64) bool {
	if limit > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, limit)
	}
	if err := json.NewDecoder(r.Body).Decode(out); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusBadRequest, "JSON 请求体过大")
			return false
		}
		writeError(w, http.StatusBadRequest, "JSON 格式错误")
		return false
	}
	return true
}

func writeJSONOrError(w http.ResponseWriter, out any, err error) {
	if err != nil {
		status := http.StatusInternalServerError
		if app.IsBadRequest(err) {
			status = http.StatusBadRequest
		} else if code, ok := app.ErrorStatus(err); ok {
			status = code
		} else if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, out)
}

func writeNoContentOrError(w http.ResponseWriter, err error) {
	if err != nil {
		writeJSONOrError(w, nil, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, out any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(out)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func redirectTo(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, path, http.StatusTemporaryRedirect)
	}
}

func (s *Server) static() http.Handler {
	dist, err := fs.Sub(s.Static, "frontend/dist")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { writeError(w, http.StatusNotFound, "frontend not built") })
	}
	files := http.FileServer(http.FS(dist))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if f, err := dist.Open(p); err == nil {
			_ = f.Close()
			files.ServeHTTP(w, r)
			return
		}
		r.URL.Path = "/"
		files.ServeHTTP(w, r)
	})
}

const (
	defaultBrowserDebugURL = "http://127.0.0.1:19222"
	defaultBrowserProxyURL = "http://127.0.0.1:6080"
	defaultBrowserVNCURL   = "/browser/vnc.html?autoconnect=true&resize=scale"
)

func browserDebugURL() string {
	if v := strings.TrimRight(os.Getenv("BROWSER_DEBUG_URL"), "/"); v != "" {
		return v
	}
	return defaultBrowserDebugURL
}

func browserVNCURL() string {
	if v := os.Getenv("BROWSER_VNC_URL"); strings.TrimSpace(v) != "" {
		return v
	}
	return defaultBrowserVNCURL
}

func browserProxyURL() string {
	if v := strings.TrimRight(os.Getenv("BROWSER_PROXY_URL"), "/"); v != "" {
		return v
	}
	return defaultBrowserProxyURL
}

func (s *Server) proxyBrowser(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/browser/package.json" {
		writeJSON(w, map[string]string{"name": "novnc"})
		return
	}
	target, err := url.Parse(browserProxyURL())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "bad browser proxy url")
		return
	}
	path := r.URL.Path
	if strings.HasPrefix(path, "/browser/") {
		path = "/" + strings.TrimPrefix(path, "/browser/")
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Director = func(req *http.Request) {
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.URL.Path = path
		req.Host = target.Host
	}
	proxy.ServeHTTP(w, r)
}

func openBrowserURL(rawurl string) error {
	tabs, err := browserTabs()
	if err == nil {
		base, _ := url.Parse(rawurl)
		keepID := ""
		for _, tab := range tabs {
			u, _ := url.Parse(tab.URL)
			if tab.Type != "page" || tab.ID == "" || u.Host != base.Host {
				continue
			}
			if keepID == "" {
				keepID = tab.ID
				continue
			}
			_ = closeBrowserTab(tab.ID)
		}
		if keepID != "" {
			return activateBrowserTab(keepID)
		}
	}
	resp, err := browserDebugDo(http.MethodPut, "/json/new?"+url.QueryEscape(rawurl))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New("browser devtools status " + resp.Status)
	}
	return nil
}

func activateBrowserTab(id string) error {
	resp, err := browserDebugDo(http.MethodGet, "/json/activate/"+url.PathEscape(id))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New("browser devtools status " + resp.Status)
	}
	return nil
}

func closeBrowserTab(id string) error {
	resp, err := browserDebugDo(http.MethodGet, "/json/close/"+url.PathEscape(id))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New("browser devtools status " + resp.Status)
	}
	return nil
}

type debugTab struct {
	ID                   string `json:"id"`
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

func browserTab(baseURL string) (debugTab, error) {
	tabs, err := browserTabs()
	if err != nil {
		return debugTab{}, err
	}
	base, _ := url.Parse(baseURL)
	for _, tab := range tabs {
		u, _ := url.Parse(tab.URL)
		if tab.Type == "page" && tab.WebSocketDebuggerURL != "" && u.Host == base.Host {
			tab.WebSocketDebuggerURL = rewriteWebSocketURL(browserDebugURL(), tab.WebSocketDebuggerURL)
			return tab, nil
		}
	}
	return debugTab{}, errors.New("找不到已打开的登录页")
}

func browserTabs() ([]debugTab, error) {
	resp, err := browserDebugDo(http.MethodGet, "/json")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var tabs []debugTab
	if err := json.NewDecoder(resp.Body).Decode(&tabs); err != nil {
		return nil, err
	}
	return tabs, nil
}

func browserDebugDo(method, path string) (*http.Response, error) {
	req, err := http.NewRequest(method, browserDebugURL()+path, nil)
	if err != nil {
		return nil, err
	}
	req.Host = browserDebugHostHeader()
	return http.DefaultClient.Do(req)
}

func rewriteWebSocketURL(debugURL, wsURL string) string {
	debug, err := url.Parse(debugURL)
	if err != nil {
		return wsURL
	}
	ws, err := url.Parse(wsURL)
	if err != nil {
		return wsURL
	}
	ws.Host = browserDebugHostHeader()
	if debug.Scheme == "https" {
		ws.Scheme = "wss"
	} else if debug.Scheme == "http" {
		ws.Scheme = "ws"
	}
	return ws.String()
}

func readBrowserTokens(tab debugTab) (string, string, error) {
	ws, err := dialBrowserWebSocket(tab.WebSocketDebuggerURL)
	if err != nil {
		return "", "", err
	}
	defer ws.Close()
	snapshot, err := cdpEval(ws)
	if err != nil {
		return "", "", err
	}
	cookies, err := cdpCookies(ws)
	if err != nil {
		return "", "", err
	}
	for k, v := range cookies {
		snapshot[k] = v
	}
	access, refresh := "", ""
	for k, v := range snapshot {
		access, refresh = pickToken(k, v, access, refresh)
	}
	return access, refresh, nil
}

func dialBrowserWebSocket(location string) (*websocket.Conn, error) {
	config, err := websocket.NewConfig(location, browserDebugOrigin())
	if err != nil {
		return nil, err
	}
	if browserDebugHostHeader() == browserDebugConnectAddress() {
		return websocket.DialConfig(config)
	}
	conn, err := net.DialTimeout("tcp", browserDebugConnectAddress(), 10*time.Second)
	if err != nil {
		return nil, err
	}
	ws, err := websocket.NewClient(config, conn)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return ws, nil
}

func browserDebugOrigin() string {
	scheme := "http"
	if debug, err := url.Parse(browserDebugURL()); err == nil && debug.Scheme != "" {
		scheme = debug.Scheme
	}
	return scheme + "://" + browserDebugHostHeader()
}

func browserDebugHostHeader() string {
	if v := strings.TrimSpace(os.Getenv("BROWSER_DEBUG_HOST_HEADER")); v != "" {
		return v
	}
	debug, err := url.Parse(browserDebugURL())
	if err != nil {
		return "127.0.0.1:19222"
	}
	host := debug.Hostname()
	if strings.EqualFold(host, "localhost") || net.ParseIP(host) != nil {
		return hostWithDefaultPort(debug.Host, debug.Scheme)
	}
	return "127.0.0.1:19222"
}

func browserDebugConnectAddress() string {
	debug, err := url.Parse(browserDebugURL())
	if err != nil {
		return "127.0.0.1:19222"
	}
	return hostWithDefaultPort(debug.Host, debug.Scheme)
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

func cdpEval(ws *websocket.Conn) (map[string]string, error) {
	expr := `(() => {
const out = {};
for (const store of [localStorage, sessionStorage]) {
  for (let i = 0; i < store.length; i++) {
    const k = store.key(i);
    out[k] = store.getItem(k);
  }
}
out.cookie = document.cookie || "";
return JSON.stringify(out);
})()`
	msg, err := cdpCall(ws, 1, "Runtime.evaluate", map[string]any{"expression": expr, "returnByValue": true})
	if err != nil {
		return nil, err
	}
	result := objMap(objMap(msg["result"])["result"])
	raw, _ := result["value"].(string)
	out := map[string]string{}
	_ = json.Unmarshal([]byte(raw), &out)
	return out, nil
}

func cdpCookies(ws *websocket.Conn) (map[string]string, error) {
	msg, err := cdpCall(ws, 2, "Network.getAllCookies", map[string]any{})
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, item := range arrayAny(objMap(msg["result"])["cookies"]) {
		c := objMap(item)
		name, _ := c["name"].(string)
		value, _ := c["value"].(string)
		if name != "" {
			out[name] = value
		}
	}
	return out, nil
}

func cdpCall(ws *websocket.Conn, id int, method string, params map[string]any) (map[string]any, error) {
	if err := json.NewEncoder(ws).Encode(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(ws)
	for {
		var msg map[string]any
		if err := dec.Decode(&msg); err != nil {
			return nil, err
		}
		if got, _ := msg["id"].(float64); int(got) == id {
			if msg["error"] != nil {
				return nil, errors.New("browser cdp error")
			}
			return msg, nil
		}
	}
}

func pickToken(key, value, access, refresh string) (string, string) {
	k := strings.ToLower(key)
	if refresh == "" && strings.Contains(k, "refresh") && strings.Contains(k, "token") {
		refresh = strings.TrimSpace(value)
	}
	if access == "" && ((strings.Contains(k, "access") && strings.Contains(k, "token")) || k == "token" || k == "auth_token") {
		access = strings.TrimSpace(value)
	}
	var nested map[string]any
	if json.Unmarshal([]byte(value), &nested) == nil {
		for nk, nv := range nested {
			if s, ok := nv.(string); ok {
				access, refresh = pickToken(nk, s, access, refresh)
			}
		}
	}
	return strings.TrimPrefix(access, "Bearer "), strings.TrimPrefix(refresh, "Bearer ")
}

func objMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func arrayAny(v any) []any {
	if a, ok := v.([]any); ok {
		return a
	}
	return nil
}

func AuthenticatedRequest(ctx context.Context, st *store.Store, r *http.Request) bool {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}
	_, err = st.UserBySessionToken(ctx, c.Value)
	return err == nil
}
