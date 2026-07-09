package httpapi

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/net/websocket"
)

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
