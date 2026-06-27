package httpapi

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	"ai-upstream-monitor/internal/app"
	"ai-upstream-monitor/internal/domain"
	"ai-upstream-monitor/internal/store"

	"golang.org/x/net/websocket"
)

const sessionCookie = "monitor_session"

type Server struct {
	App    *app.Service
	Static embed.FS
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/setup/status", s.setupStatus)
	mux.HandleFunc("GET /api/public/settings", s.publicSettings)
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
	mux.HandleFunc("POST /api/upstreams/{id}/browser-login", s.auth(s.browserLogin))
	mux.HandleFunc("POST /api/upstreams/{id}/browser-capture", s.auth(s.browserCapture))
	mux.HandleFunc("GET /api/cards", s.auth(s.listCards))
	mux.HandleFunc("POST /api/cards", s.auth(s.createCard))
	mux.HandleFunc("PATCH /api/cards/{id}", s.auth(s.updateCard))
	mux.HandleFunc("DELETE /api/cards/{id}", s.auth(s.deleteCard))
	mux.HandleFunc("POST /api/cards/{id}/check", s.auth(s.checkCard))
	mux.HandleFunc("GET /api/settings", s.auth(s.settings))
	mux.HandleFunc("PATCH /api/settings", s.auth(s.updateSettings))
	mux.HandleFunc("GET /api/settings/export", s.auth(s.exportData))
	mux.HandleFunc("POST /api/settings/import", s.auth(s.importData))
	mux.HandleFunc("GET /api/monitor/status", s.auth(s.monitorStatus))
	mux.HandleFunc("GET /api/monitor/balances", s.auth(s.balances))
	mux.HandleFunc("POST /api/monitor/balances/refresh", s.auth(s.refreshBalances))
	mux.HandleFunc("/browser/", s.auth(s.proxyBrowser))
	mux.HandleFunc("/websockify", s.auth(s.proxyBrowser))
	mux.Handle("/", s.static())
	return mux
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil || c.Value == "" {
			writeError(w, http.StatusUnauthorized, "未登录")
			return
		}
		if _, err := s.App.Me(r.Context(), c.Value); err != nil {
			writeError(w, http.StatusUnauthorized, "登录已过期")
			return
		}
		next(w, r)
	}
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
	token, u, err := s.App.Login(r.Context(), body.Username, body.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Expires: time.Now().Add(30 * 24 * time.Hour),
	})
	writeJSON(w, map[string]any{"user": u})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = s.App.Logout(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
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

func (s *Server) listCards(w http.ResponseWriter, r *http.Request) {
	rows, err := s.App.ListCards(r.Context())
	writeJSONOrError(w, rows, err)
}

func (s *Server) createCard(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UpstreamID string `json:"upstream_id"`
		KeyID      string `json:"key_id"`
		Enabled    *bool  `json:"enabled"`
	}
	if !decode(w, r, &body) {
		return
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	card, err := s.App.SaveCard(r.Context(), "", body.UpstreamID, body.KeyID, enabled)
	writeJSONOrError(w, card, err)
}

func (s *Server) updateCard(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UpstreamID string `json:"upstream_id"`
		KeyID      string `json:"key_id"`
		Enabled    *bool  `json:"enabled"`
	}
	if !decode(w, r, &body) {
		return
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	card, err := s.App.SaveCard(r.Context(), r.PathValue("id"), body.UpstreamID, body.KeyID, enabled)
	writeJSONOrError(w, card, err)
}

func (s *Server) deleteCard(w http.ResponseWriter, r *http.Request) {
	writeNoContentOrError(w, s.App.Store.DeleteCard(r.Context(), r.PathValue("id")))
}

func (s *Server) checkCard(w http.ResponseWriter, r *http.Request) {
	writeNoContentOrError(w, s.App.CheckCard(r.Context(), r.PathValue("id")))
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
	w.Header().Set("Content-Disposition", `attachment; filename="ai-upstream-monitor-export.json"`)
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) importData(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<20)
	var in store.ExportData
	if !decode(w, r, &in) {
		return
	}
	writeNoContentOrError(w, s.App.Store.ImportData(r.Context(), in))
}

func (s *Server) monitorStatus(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.MonitorStatus(r.Context(), r.URL.Query().Get("window"))
	writeJSONOrError(w, out, err)
}

func (s *Server) balances(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.BalanceRows(r.Context())
	writeJSONOrError(w, out, err)
}

func (s *Server) refreshBalances(w http.ResponseWriter, r *http.Request) {
	writeNoContentOrError(w, s.App.RefreshBalances(r.Context()))
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
	if err := json.NewDecoder(r.Body).Decode(out); err != nil {
		writeError(w, http.StatusBadRequest, "JSON 格式错误")
		return false
	}
	return true
}

func writeJSONOrError(w http.ResponseWriter, out any, err error) {
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		} else if statusError(err) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, out)
}

func statusError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "required") || strings.Contains(msg, "already") || strings.Contains(msg, "must") || strings.Contains(msg, "belong")
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
