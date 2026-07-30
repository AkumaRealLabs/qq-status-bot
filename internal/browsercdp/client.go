package browsercdp

import (
	"context"
	"encoding/base64"
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

const maxScreenshotBytes = 20 << 20

type Client struct {
	DebugURL   string
	HostHeader string
	Width      int
	Height     int
	Wait       time.Duration
	HTTP       *http.Client
}

type debugTab struct {
	ID                   string `json:"id"`
	Type                 string `json:"type"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

type captureRect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

func (c Client) Capture(ctx context.Context, rawURL, selector string) ([]byte, error) {
	return c.CaptureWithOptions(ctx, rawURL, selector, c.Width, c.Height, c.Wait)
}

func (c Client) CaptureWithOptions(ctx context.Context, rawURL, selector string, width, height int, wait time.Duration) ([]byte, error) {
	tab, err := c.createTab(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	defer c.closeTab(tab.ID)

	ws, err := c.dial(tab.WebSocketDebuggerURL)
	if err != nil {
		return nil, fmt.Errorf("连接 Chromium: %w", err)
	}
	defer ws.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = ws.SetDeadline(deadline)
	}
	session := &cdpSession{conn: ws}
	if _, err := session.call("Page.enable", nil); err != nil {
		return nil, err
	}
	if width <= 0 {
		width = c.Width
	}
	if height <= 0 {
		height = c.Height
	}
	if _, err := session.call("Emulation.setDeviceMetricsOverride", map[string]any{
		"width": viewport(width, 1280), "height": viewport(height, 900), "deviceScaleFactor": 1, "mobile": false,
	}); err != nil {
		return nil, err
	}
	if _, err := session.call("Page.navigate", map[string]any{"url": rawURL}); err != nil {
		return nil, err
	}
	if err := waitUntilReady(ctx, session, selector); err != nil {
		return nil, err
	}
	if err := waitContext(ctx, wait); err != nil {
		return nil, err
	}
	rect, err := elementRect(session, selector)
	if err != nil {
		return nil, err
	}
	if rect.Width < 1 || rect.Height < 1 || rect.Width > 4096 || rect.Height > 8192 {
		return nil, fmt.Errorf("截图区域尺寸异常：%.0fx%.0f", rect.Width, rect.Height)
	}
	result, err := session.call("Page.captureScreenshot", map[string]any{
		"format": "png",
		"clip": map[string]any{
			"x": rect.X, "y": rect.Y, "width": rect.Width, "height": rect.Height, "scale": 1,
		},
		"captureBeyondViewport": true,
		"fromSurface":           true,
	})
	if err != nil {
		return nil, err
	}
	encoded, _ := result["data"].(string)
	image, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(image) == 0 {
		return nil, errors.New("Chromium 返回了无效截图")
	}
	if len(image) > maxScreenshotBytes {
		return nil, errors.New("截图超过 20 MB")
	}
	return image, nil
}

func (c Client) createTab(ctx context.Context, rawURL string) (debugTab, error) {
	endpoint := strings.TrimRight(c.debugURL(), "/") + "/json/new?" + url.QueryEscape(rawURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, nil)
	if err != nil {
		return debugTab{}, err
	}
	req.Host = c.hostHeader()
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return debugTab{}, fmt.Errorf("创建 Chromium 页面: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return debugTab{}, fmt.Errorf("创建 Chromium 页面: HTTP %d", resp.StatusCode)
	}
	var tab debugTab
	if err := json.NewDecoder(resp.Body).Decode(&tab); err != nil {
		return debugTab{}, errors.New("Chromium 调试接口返回了无效 JSON")
	}
	if tab.ID == "" || tab.WebSocketDebuggerURL == "" {
		return debugTab{}, errors.New("Chromium 调试接口未返回页面")
	}
	return tab, nil
}

func (c Client) closeTab(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.debugURL(), "/")+"/json/close/"+url.PathEscape(id), nil)
	if err != nil {
		return
	}
	req.Host = c.hostHeader()
	resp, err := c.httpClient().Do(req)
	if err == nil {
		_ = resp.Body.Close()
	}
}

func waitUntilReady(ctx context.Context, session *cdpSession, selector string) error {
	selectorJSON, _ := json.Marshal(selector)
	expression := "document.readyState === 'complete' && document.querySelector(" + string(selectorJSON) + ") !== null"
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		ready, err := evaluateBool(session, expression)
		if err == nil && ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("等待状态页加载: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func elementRect(session *cdpSession, selector string) (captureRect, error) {
	selectorJSON, _ := json.Marshal(selector)
	expression := `(() => {
const element = document.querySelector(` + string(selectorJSON) + `);
if (!element) return "";
const rect = element.getBoundingClientRect();
return JSON.stringify({x: rect.left + window.scrollX, y: rect.top + window.scrollY, width: rect.width, height: rect.height});
})()`
	result, err := evaluate(session, expression)
	if err != nil {
		return captureRect{}, err
	}
	raw, _ := result.(string)
	if raw == "" {
		return captureRect{}, errors.New("找不到 SCREENSHOT_SELECTOR 指定的区域")
	}
	var rect captureRect
	if err := json.Unmarshal([]byte(raw), &rect); err != nil {
		return captureRect{}, errors.New("无法读取截图区域尺寸")
	}
	return rect, nil
}

func evaluateBool(session *cdpSession, expression string) (bool, error) {
	value, err := evaluate(session, expression)
	if err != nil {
		return false, err
	}
	out, _ := value.(bool)
	return out, nil
}

func evaluate(session *cdpSession, expression string) (any, error) {
	result, err := session.call("Runtime.evaluate", map[string]any{
		"expression": expression, "returnByValue": true,
	})
	if err != nil {
		return nil, err
	}
	if result["exceptionDetails"] != nil {
		return nil, errors.New("状态页脚本执行失败")
	}
	remote := object(result["result"])
	return remote["value"], nil
}

type cdpSession struct {
	conn   *websocket.Conn
	nextID int
}

func (s *cdpSession) call(method string, params map[string]any) (map[string]any, error) {
	s.nextID++
	id := s.nextID
	message := map[string]any{"id": id, "method": method}
	if params != nil {
		message["params"] = params
	}
	if err := json.NewEncoder(s.conn).Encode(message); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(s.conn)
	for {
		var response map[string]any
		if err := decoder.Decode(&response); err != nil {
			return nil, err
		}
		responseID, _ := response["id"].(float64)
		if int(responseID) != id {
			continue
		}
		if response["error"] != nil {
			return nil, fmt.Errorf("Chromium CDP 调用 %s 失败", method)
		}
		return object(response["result"]), nil
	}
}

func (c Client) dial(location string) (*websocket.Conn, error) {
	wsURL, err := url.Parse(location)
	if err != nil {
		return nil, err
	}
	wsURL.Scheme = "ws"
	wsURL.Host = c.hostHeader()
	config, err := websocket.NewConfig(wsURL.String(), "http://"+c.hostHeader())
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

func (c Client) debugURL() string {
	if value := strings.TrimSpace(c.DebugURL); value != "" {
		return value
	}
	return "http://127.0.0.1:9222"
}

func (c Client) hostHeader() string {
	if value := strings.TrimSpace(c.HostHeader); value != "" {
		return value
	}
	debug, err := url.Parse(c.debugURL())
	if err != nil {
		return "127.0.0.1:9222"
	}
	host := debug.Hostname()
	if strings.EqualFold(host, "localhost") || net.ParseIP(host) != nil {
		return hostWithDefaultPort(debug.Host)
	}
	return "127.0.0.1:9222"
}

func (c Client) connectAddress() string {
	debug, err := url.Parse(c.debugURL())
	if err != nil {
		return "127.0.0.1:9222"
	}
	return hostWithDefaultPort(debug.Host)
}

func (c Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func viewport(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func hostWithDefaultPort(host string) string {
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}
	if strings.Contains(host, ":") {
		return host
	}
	return net.JoinHostPort(host, "80")
}

func waitContext(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return nil
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func object(value any) map[string]any {
	if out, ok := value.(map[string]any); ok {
		return out
	}
	return map[string]any{}
}
