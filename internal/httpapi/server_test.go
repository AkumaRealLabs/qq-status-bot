package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"testing/fstest"

	"qq-status-bot/internal/app"
	"qq-status-bot/internal/domain"
	"qq-status-bot/internal/store"
)

type previewGenerator struct {
	image []byte
	err   error
}

func (g previewGenerator) Generate(context.Context, string, string, string) ([]byte, error) {
	return g.image, g.err
}

type noopReplier struct{}

func (noopReplier) ReplyGroupImage(context.Context, string, string, []byte) error { return nil }
func (noopReplier) ReplyGroupText(context.Context, string, string, string, int) error {
	return nil
}

type activeNoopReplier struct {
	noopReplier
	groups []string
}

func (r *activeNoopReplier) SendGroupText(_ context.Context, group, _ string) error {
	r.groups = append(r.groups, group)
	return nil
}

func TestAlertTestRequiresAuthenticationAndSavedGroup(t *testing.T) {
	defaults := domain.Settings{
		Commands: []string{"状态"}, StatusURL: "https://status.example", StatusPageID: "default",
		StatusPeriod: "1y", ScreenshotTimeout: 90, QueueSize: 3, AlertGroups: []string{"alert-group"},
		AlertFailureSamples: 2, AlertRecoverySamples: 2,
	}
	state, err := store.Open(filepath.Join(t.TempDir(), "state.json"), defaults)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = state.Close() })
	replier := &activeNoopReplier{}
	service := app.New(state, previewGenerator{image: []byte("png")}, replier, 3)
	handler := (&Server{App: service}).Routes()
	setup := httptest.NewRequest(http.MethodPost, "/api/setup", bytes.NewBufferString(`{"username":"admin","password":"password8"}`))
	setup.Header.Set("Content-Type", "application/json")
	setupResponse := httptest.NewRecorder()
	handler.ServeHTTP(setupResponse, setup)
	cookies := setupResponse.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("初始化 Cookie 错误: %v", cookies)
	}
	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodPost, "/api/alerts/test", bytes.NewBufferString(`{"group_openid":"alert-group"}`)))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("测试接口应要求登录: %d", unauthenticated.Code)
	}
	invalid := httptest.NewRequest(http.MethodPost, "/api/alerts/test", bytes.NewBufferString(`{"group_openid":"other"}`))
	invalid.Header.Set("Content-Type", "application/json")
	invalid.AddCookie(cookies[0])
	invalidResponse := httptest.NewRecorder()
	handler.ServeHTTP(invalidResponse, invalid)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("未保存告警群应拒绝: %d %s", invalidResponse.Code, invalidResponse.Body.String())
	}
	valid := httptest.NewRequest(http.MethodPost, "/api/alerts/test", bytes.NewBufferString(`{"group_openid":"alert-group"}`))
	valid.Header.Set("Content-Type", "application/json")
	valid.AddCookie(cookies[0])
	validResponse := httptest.NewRecorder()
	handler.ServeHTTP(validResponse, valid)
	if validResponse.Code != http.StatusOK || len(replier.groups) != 1 || len(state.Logs(10)) != 1 || state.Logs(10)[0].EventType != "ALERT_TEST" {
		t.Fatalf("测试发送错误: code=%d groups=%v logs=%v", validResponse.Code, replier.groups, state.Logs(10))
	}
}

func TestStatusPreviewRequiresAuthentication(t *testing.T) {
	handler, _ := previewHarness(t, previewGenerator{image: []byte("png")})
	request := httptest.NewRequest(http.MethodGet, "/api/status-preview", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || response.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("未鉴权响应错误: code=%d headers=%v", response.Code, response.Header())
	}
}

func TestSetupAndLoginOnlyRequirePassword(t *testing.T) {
	defaults := domain.Settings{Commands: []string{"状态"}, StatusURL: "https://status.example", StatusPageID: "default", StatusPeriod: "1y", ScreenshotTimeout: 90, QueueSize: 3}
	state, err := store.Open(filepath.Join(t.TempDir(), "state.json"), defaults)
	if err != nil {
		t.Fatal(err)
	}
	service := app.New(state, previewGenerator{}, noopReplier{}, 3)
	handler := (&Server{App: service}).Routes()
	setup := httptest.NewRequest(http.MethodPost, "/api/setup", bytes.NewBufferString(`{"password":"password8"}`))
	setup.Header.Set("Content-Type", "application/json")
	setupResponse := httptest.NewRecorder()
	handler.ServeHTTP(setupResponse, setup)
	if setupResponse.Code != http.StatusOK {
		t.Fatalf("仅密码初始化失败: %d %s", setupResponse.Code, setupResponse.Body.String())
	}
	login := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"password":"password8"}`))
	login.Header.Set("Content-Type", "application/json")
	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, login)
	if loginResponse.Code != http.StatusOK || len(loginResponse.Result().Cookies()) == 0 {
		t.Fatalf("仅密码登录失败: %d %s", loginResponse.Code, loginResponse.Body.String())
	}
}

func TestDiscoveredGroupsRequiresAuthentication(t *testing.T) {
	defaults := domain.Settings{Commands: []string{"状态"}, StatusURL: "https://status.example", StatusPageID: "default", StatusPeriod: "1y", ScreenshotTimeout: 90, QueueSize: 3}
	state, err := store.Open(filepath.Join(t.TempDir(), "state.json"), defaults)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.AppendLog(domain.EventLog{Direction: "receive", GroupOpenID: "observed-group"}); err != nil {
		t.Fatal(err)
	}
	service := app.New(state, previewGenerator{}, noopReplier{}, 3)
	handler := (&Server{App: service}).Routes()
	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/groups/discovered", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("已发现群接口应要求登录: %d", unauthenticated.Code)
	}
	setup := httptest.NewRequest(http.MethodPost, "/api/setup", bytes.NewBufferString(`{"password":"password8"}`))
	setup.Header.Set("Content-Type", "application/json")
	setupResponse := httptest.NewRecorder()
	handler.ServeHTTP(setupResponse, setup)
	cookies := setupResponse.Result().Cookies()
	request := httptest.NewRequest(http.MethodGet, "/api/groups/discovered", nil)
	request.AddCookie(cookies[0])
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var groups []string
	if err := json.Unmarshal(response.Body.Bytes(), &groups); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || len(groups) != 1 || groups[0] != "observed-group" {
		t.Fatalf("已发现群响应错误: code=%d groups=%v", response.Code, groups)
	}
}

func TestAdminRootServesIndexWithoutRedirect(t *testing.T) {
	server := &Server{Static: fstest.MapFS{"index.html": {Data: []byte("admin")}}}
	handler := server.Routes()
	request := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "admin" || response.Header().Get("Location") != "" {
		t.Fatalf("管理端入口响应错误: code=%d location=%q body=%q", response.Code, response.Header().Get("Location"), response.Body.String())
	}
}

func TestStatusPreviewReturnsPNGWithoutCaching(t *testing.T) {
	want := []byte("\x89PNG\r\n\x1a\nimage")
	handler, cookie := previewHarness(t, previewGenerator{image: want})
	request := httptest.NewRequest(http.MethodGet, "/api/status-preview", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("PNG 响应错误: code=%d headers=%v", response.Code, response.Header())
	}
	if response.Header().Get("Cache-Control") != "no-store" || !bytes.Equal(response.Body.Bytes(), want) {
		t.Fatalf("PNG 内容或缓存头错误: headers=%v body=%q", response.Header(), response.Body.Bytes())
	}
}

func TestStatusPreviewReturnsJSONForGeneratorError(t *testing.T) {
	handler, cookie := previewHarness(t, previewGenerator{err: errors.New("上游不可用")})
	request := httptest.NewRequest(http.MethodGet, "/api/status-preview", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusBadGateway || body["error"] != "上游不可用" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("错误响应不正确: code=%d headers=%v body=%v", response.Code, response.Header(), body)
	}
}

func previewHarness(t *testing.T, generator previewGenerator) (http.Handler, *http.Cookie) {
	t.Helper()
	defaults := domain.Settings{
		Commands: []string{"状态"}, StatusURL: "https://status.example", StatusPageID: "default",
		StatusPeriod: "1y", ScreenshotTimeout: 90, QueueSize: 3,
	}
	state, err := store.Open(filepath.Join(t.TempDir(), "state.json"), defaults)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = state.Close() })
	service := app.New(state, generator, noopReplier{}, 3)
	handler := (&Server{App: service}).Routes()
	setup := httptest.NewRequest(http.MethodPost, "/api/setup", bytes.NewBufferString(`{"username":"admin","password":"password8"}`))
	setup.Header.Set("Content-Type", "application/json")
	setupResponse := httptest.NewRecorder()
	handler.ServeHTTP(setupResponse, setup)
	if setupResponse.Code != http.StatusOK {
		t.Fatalf("初始化失败: code=%d body=%s", setupResponse.Code, setupResponse.Body.String())
	}
	cookies := setupResponse.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("初始化未返回会话 Cookie")
	}
	return handler, cookies[0]
}
