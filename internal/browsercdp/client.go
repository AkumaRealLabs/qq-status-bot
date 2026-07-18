package browsercdp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/websocket"
)

const requestTimeout = 45 * time.Second

type Client struct {
	DebugURL   string
	HostHeader string
	HTTP       *http.Client
}

type debugTab struct {
	ID                   string `json:"id"`
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

type fetchResult struct {
	Status int    `json:"status"`
	Body   string `json:"body"`
}

func (c Client) Do(ctx context.Context, method, rawURL string, body []byte, headers map[string]string) (int, []byte, error) {
	tab, err := c.tabForURL(ctx, rawURL)
	if err != nil {
		return 0, nil, err
	}
	ws, err := c.dial(tab.WebSocketDebuggerURL)
	if err != nil {
		return 0, nil, err
	}
	defer ws.Close()
	deadline := time.Now().Add(requestTimeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	_ = ws.SetDeadline(deadline)

	expr, err := fetchExpression(method, rawURL, body, headers)
	if err != nil {
		return 0, nil, err
	}
	msg, err := cdpCall(ws, 1, "Runtime.evaluate", map[string]any{
		"expression":    expr,
		"awaitPromise":  true,
		"returnByValue": true,
	})
	if err != nil {
		return 0, nil, err
	}
	result := object(object(msg["result"])["result"])
	if object(msg["result"])["exceptionDetails"] != nil {
		return 0, nil, errors.New("浏览器请求执行失败")
	}
	raw, _ := result["value"].(string)
	var out fetchResult
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return 0, nil, fmt.Errorf("解析浏览器响应: %w", err)
	}
	return out.Status, []byte(out.Body), nil
}

func fetchExpression(method, rawURL string, body []byte, headers map[string]string) (string, error) {
	requestHeaders := make(map[string]string, len(headers)+1)
	for key, value := range headers {
		requestHeaders[key] = value
	}
	if len(body) > 0 && requestHeaders["Content-Type"] == "" {
		requestHeaders["Content-Type"] = "application/json"
	}
	payload := map[string]any{
		"url": rawURL,
		"options": map[string]any{
			"method":      method,
			"headers":     requestHeaders,
			"credentials": "include",
			"cache":       "no-store",
		},
	}
	if len(body) > 0 {
		payload["options"].(map[string]any)["body"] = string(body)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return `(async () => {
const request = ` + string(raw) + `;
const response = await fetch(request.url, request.options);
const responseBody = await response.text();
if (response.ok && request.url.includes('/auth/refresh')) {
  try {
    const parsed = JSON.parse(responseBody);
    const tokens = parsed.data || parsed;
    if (tokens.access_token) localStorage.setItem('auth_token', tokens.access_token);
    if (tokens.refresh_token) localStorage.setItem('refresh_token', tokens.refresh_token);
    if (tokens.expires_in) localStorage.setItem('token_expires_at', String(Date.now() + tokens.expires_in * 1000));
  } catch (_) {}
}
return JSON.stringify({status: response.status, body: responseBody});
})()`, nil
}

func (c Client) tabForURL(ctx context.Context, rawURL string) (debugTab, error) {
	target, err := url.Parse(rawURL)
	if err != nil || target.Host == "" {
		return debugTab{}, errors.New("无效的浏览器请求地址")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.debugURL(), "/")+"/json", nil)
	if err != nil {
		return debugTab{}, err
	}
	req.Host = c.hostHeader()
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return debugTab{}, err
	}
	defer resp.Body.Close()
	var tabs []debugTab
	if err := json.NewDecoder(resp.Body).Decode(&tabs); err != nil {
		return debugTab{}, err
	}
	for _, tab := range tabs {
		tabURL, _ := url.Parse(tab.URL)
		if tab.Type == "page" && tab.WebSocketDebuggerURL != "" && strings.EqualFold(tabURL.Host, target.Host) {
			return tab, nil
		}
	}
	return debugTab{}, errors.New("找不到该上游已打开的浏览器标签页")
}

func (c Client) dial(location string) (*websocket.Conn, error) {
	wsURL, err := url.Parse(location)
	if err != nil {
		return nil, err
	}
	debug, _ := url.Parse(c.debugURL())
	wsURL.Host = c.hostHeader()
	if debug != nil && debug.Scheme == "https" {
		wsURL.Scheme = "wss"
	} else {
		wsURL.Scheme = "ws"
	}
	originScheme := "http"
	if wsURL.Scheme == "wss" {
		originScheme = "https"
	}
	config, err := websocket.NewConfig(wsURL.String(), originScheme+"://"+c.hostHeader())
	if err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout("tcp", c.connectAddress(), 10*time.Second)
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
				return nil, errors.New("浏览器 CDP 请求失败")
			}
			return msg, nil
		}
	}
}

func (c Client) debugURL() string {
	if value := strings.TrimSpace(c.DebugURL); value != "" {
		return value
	}
	return "http://127.0.0.1:19222"
}

func (c Client) hostHeader() string {
	if value := strings.TrimSpace(c.HostHeader); value != "" {
		return value
	}
	debug, err := url.Parse(c.debugURL())
	if err != nil {
		return "127.0.0.1:19222"
	}
	host := debug.Hostname()
	if strings.EqualFold(host, "localhost") || net.ParseIP(host) != nil {
		return hostWithDefaultPort(debug.Host, debug.Scheme)
	}
	return "127.0.0.1:19222"
}

func (c Client) connectAddress() string {
	debug, err := url.Parse(c.debugURL())
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

func object(value any) map[string]any {
	if out, ok := value.(map[string]any); ok {
		return out
	}
	return map[string]any{}
}
