package httpapi

import (
	"context"
	"embed"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"

	"ai-upstream-monitor/internal/app"
	"ai-upstream-monitor/internal/domain"
	"ai-upstream-monitor/internal/store"
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
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/setup/status", s.setupStatus)
	mux.HandleFunc("GET /api/public/settings", s.publicSettings)
	mux.HandleFunc("GET /api/public/monitor/status", s.publicMonitorStatus)
	mux.HandleFunc("POST /api/onebot/events", s.oneBotEvents)
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
	mux.HandleFunc("GET /api/upstreams/{id}/availability-policy", s.auth(s.availabilityPolicy))
	mux.HandleFunc("PATCH /api/upstreams/{id}/availability-policy", s.auth(s.updateAvailabilityPolicy))
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
	mux.HandleFunc("GET /api/scheduler/axonhub/config", s.auth(s.axonHubConfig))
	mux.HandleFunc("PATCH /api/scheduler/axonhub/config", s.auth(s.updateAxonHubConfig))
	mux.HandleFunc("POST /api/scheduler/axonhub/test", s.auth(s.testAxonHub))
	mux.HandleFunc("GET /api/scheduler/axonhub/preflight", s.auth(s.axonHubPreflight))
	mux.HandleFunc("POST /api/scheduler/provider/switch", s.auth(s.switchSchedulerProvider))
	mux.HandleFunc("POST /api/scheduler/control-plane/adopt-bound", s.auth(s.adoptBoundAxonHubChannels))
	mux.HandleFunc("GET /api/scheduler/groups", s.auth(s.schedulerGroups))
	mux.HandleFunc("GET /api/scheduler/channels", s.auth(s.schedulerChannels))
	mux.HandleFunc("GET /api/scheduler/logs", s.auth(s.schedulerLogs))
	mux.HandleFunc("POST /api/scheduler/groups/apply", s.auth(s.applySchedulerGroups))
	mux.HandleFunc("GET /api/scheduler/availability", s.auth(s.schedulerAvailability))
	mux.HandleFunc("POST /api/scheduler/availability/reconcile", s.auth(s.reconcileSchedulerAvailability))
	mux.HandleFunc("POST /api/scheduler/availability/{card_id}/action", s.auth(s.schedulerAvailabilityAction))
	mux.HandleFunc("GET /api/scheduler/traffic/status", s.auth(s.schedulerTrafficStatus))
	mux.HandleFunc("GET /api/scheduler/traffic", s.auth(s.schedulerTraffic))
	mux.HandleFunc("POST /api/scheduler/traffic/reconcile", s.auth(s.reconcileSchedulerTraffic))
	mux.HandleFunc("POST /api/scheduler/traffic/{channel_id}/baseline", s.auth(s.adoptSchedulerTrafficBaseline))
	mux.HandleFunc("POST /api/scheduler/traffic/{channel_id}/adopt-baseline", s.auth(s.adoptSchedulerTrafficBaseline))
	mux.HandleFunc("GET /api/scheduler/control-plane", s.auth(s.schedulerControlPlane))
	mux.HandleFunc("POST /api/scheduler/control-plane/{channel_id}/adopt", s.auth(s.adoptSchedulerControlPlaneChannel))
	mux.HandleFunc("GET /api/scheduler/ggapi/settings", s.auth(s.ggapiSettings))
	mux.HandleFunc("PUT /api/scheduler/ggapi/settings", s.auth(s.updateGGAPISettings))
	mux.HandleFunc("GET /api/scheduler/ggapi/affinity-cache", s.auth(s.ggapiAffinityCache))
	mux.HandleFunc("DELETE /api/scheduler/ggapi/affinity-cache", s.auth(s.clearGGAPIAffinityCache))
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
	mux.HandleFunc("GET /api/onebot/status", s.auth(s.oneBotStatus))
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
			_ = s.App.RecordAudit(r.Context(), domain.AuditLog{
				Actor: u.Username, Action: auditAction(r), TargetType: targetType, TargetID: targetID,
				Summary: auditSummary(r, body), Fields: fields,
			})
		}
	}
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.Health(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, out)
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

func AuthenticatedRequest(ctx context.Context, st *store.Store, r *http.Request) bool {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}
	_, err = st.UserBySessionToken(ctx, c.Value)
	return err == nil
}
