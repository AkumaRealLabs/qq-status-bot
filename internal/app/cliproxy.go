package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"ai-upstream-monitor/internal/domain"
)

var errCLIProxyNotConfigured = errors.New("请先配置 CLIProxyAPI 管理地址和管理密钥")

func (s *Service) CLIProxyConfig(ctx context.Context) (domain.CLIProxyConfig, error) {
	cfg, err := s.Store.CLIProxyConfig(ctx)
	return cfg.Public(), err
}

func (s *Service) SaveCLIProxyConfig(ctx context.Context, cfg domain.CLIProxyConfig) (domain.CLIProxyConfig, error) {
	cfg.Name = strings.TrimSpace(cfg.Name)
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	cfg.ManagementKey = strings.TrimSpace(cfg.ManagementKey)
	if cfg.Name == "" {
		cfg.Name = "CLIProxyAPI"
	}
	if cfg.BaseURL != "" {
		u, err := url.Parse(cfg.BaseURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return domain.CLIProxyConfig{}, ErrBadRequest("CLIProxyAPI 管理地址不是有效 URL")
		}
	}
	out, err := s.Store.UpdateCLIProxyConfig(ctx, cfg)
	return out.Public(), err
}

func (s *Service) CLIProxyAccounts(ctx context.Context) ([]domain.CLIProxyAuthFile, error) {
	cfg, err := s.cliProxyConfig(ctx)
	if err != nil {
		return nil, err
	}
	var raw any
	if err := s.cliProxyJSON(ctx, cfg, http.MethodGet, "/auth-files", nil, &raw); err != nil {
		return nil, err
	}
	out := cliproxyAuthFiles(raw)
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := cliproxyStatusRank(out[i]), cliproxyStatusRank(out[j])
		if ri != rj {
			return ri < rj
		}
		if !strings.EqualFold(out[i].Name, out[j].Name) {
			return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
		}
		return out[i].ModTime > out[j].ModTime
	})
	return out, nil
}

func (s *Service) UploadCLIProxyAccount(ctx context.Context, name, content string) error {
	name = strings.TrimSpace(name)
	if err := domain.ValidateCLIProxyAuthFileName(name); err != nil {
		return BadRequest(err)
	}
	content = strings.TrimSpace(content)
	if !json.Valid([]byte(content)) {
		return ErrBadRequest("授权文件内容不是有效 JSON")
	}
	cfg, err := s.cliProxyConfig(ctx)
	if err != nil {
		return err
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", name)
	if err != nil {
		return err
	}
	if _, err := io.Copy(fw, strings.NewReader(content)); err != nil {
		return err
	}
	if err := mw.Close(); err != nil {
		return err
	}
	path := "/auth-files?name=" + url.QueryEscape(name)
	_, _, err = s.cliProxyRequest(ctx, cfg, http.MethodPost, path, &body, mw.FormDataContentType())
	return err
}

func (s *Service) DownloadCLIProxyAccount(ctx context.Context, name string) ([]byte, string, error) {
	name = strings.TrimSpace(name)
	if err := domain.ValidateCLIProxyAuthFileName(name); err != nil {
		return nil, "", BadRequest(err)
	}
	cfg, err := s.cliProxyConfig(ctx)
	if err != nil {
		return nil, "", err
	}
	path := "/auth-files/download?name=" + url.QueryEscape(name)
	return s.cliProxyRequest(ctx, cfg, http.MethodGet, path, nil, "")
}

func (s *Service) DeleteCLIProxyAccount(ctx context.Context, name string) error {
	name = strings.TrimSpace(name)
	if err := domain.ValidateCLIProxyAuthFileName(name); err != nil {
		return BadRequest(err)
	}
	cfg, err := s.cliProxyConfig(ctx)
	if err != nil {
		return err
	}
	path := "/auth-files?name=" + url.QueryEscape(name)
	_, _, err = s.cliProxyRequest(ctx, cfg, http.MethodDelete, path, nil, "")
	return err
}

func (s *Service) ResetCLIProxyQuota(ctx context.Context, name string) (domain.CLIProxyResetQuotaResult, error) {
	name = strings.TrimSpace(name)
	if err := domain.ValidateCLIProxyAuthFileName(name); err != nil {
		return domain.CLIProxyResetQuotaResult{}, BadRequest(err)
	}
	cfg, err := s.cliProxyConfig(ctx)
	if err != nil {
		return domain.CLIProxyResetQuotaResult{}, err
	}
	accounts, err := s.CLIProxyAccounts(ctx)
	if err != nil {
		return domain.CLIProxyResetQuotaResult{}, err
	}
	var authIndex string
	for _, account := range accounts {
		if account.Name == name {
			authIndex = account.AuthIndex
			break
		}
	}
	if strings.TrimSpace(authIndex) == "" {
		return domain.CLIProxyResetQuotaResult{}, ErrBadRequest("认证文件缺少 auth_index")
	}
	var raw map[string]any
	if err := s.cliProxyJSON(ctx, cfg, http.MethodPost, "/reset-quota", map[string]string{"auth_index": authIndex}, &raw); err != nil {
		return domain.CLIProxyResetQuotaResult{}, err
	}
	return domain.CLIProxyResetQuotaResult{Status: cliproxyString(firstCLIProxy(raw, "status", "message")), AuthIndex: authIndex, Models: cliproxyStrings(firstCLIProxy(raw, "models"))}, nil
}

func (s *Service) cliProxyConfig(ctx context.Context) (domain.CLIProxyConfig, error) {
	cfg, err := s.Store.CLIProxyConfig(ctx)
	if err != nil {
		return cfg, err
	}
	if !cfg.Enabled {
		return cfg, ErrBadRequest("CLIProxyAPI 号池未启用")
	}
	if strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.ManagementKey) == "" {
		return cfg, ErrBadRequest(errCLIProxyNotConfigured.Error())
	}
	return cfg, nil
}

func (s *Service) cliProxyJSON(ctx context.Context, cfg domain.CLIProxyConfig, method, path string, body any, out any) error {
	var r io.Reader
	contentType := ""
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
		contentType = "application/json"
	}
	b, _, err := s.cliProxyRequest(ctx, cfg, method, path, r, contentType)
	if err != nil || len(b) == 0 || out == nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func (s *Service) cliProxyRequest(ctx context.Context, cfg domain.CLIProxyConfig, method, path string, body io.Reader, contentType string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, method, cliproxyManagementURL(cfg.BaseURL, path), body)
	if err != nil {
		return nil, "", err
	}
	key := strings.TrimSpace(cfg.ManagementKey)
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("X-Management-Key", key)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	hc := s.Client.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", cliproxyHTTPError(resp.StatusCode, b)
	}
	return b, resp.Header.Get("Content-Type"), nil
}

func cliproxyManagementURL(baseURL, path string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if u, err := url.Parse(baseURL); err == nil && u.Scheme != "" && u.Host != "" {
		if !strings.HasSuffix(strings.TrimRight(u.Path, "/"), "/v0/management") {
			u.Path = strings.TrimRight(u.Path, "/") + "/v0/management"
		}
		return strings.TrimRight(u.String(), "/") + "/" + strings.TrimLeft(path, "/")
	}
	if !strings.HasSuffix(strings.TrimRight(baseURL, "/"), "/v0/management") {
		baseURL += "/v0/management"
	}
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(path, "/")
}

func cliproxyHTTPError(status int, body []byte) error {
	msg := strings.TrimSpace(string(body))
	var raw map[string]any
	if json.Unmarshal(body, &raw) == nil {
		msg = cliproxyString(firstCLIProxy(raw, "error", "message", "msg"))
		if nested, ok := raw["error"].(map[string]any); ok {
			msg = cliproxyString(firstCLIProxy(nested, "message", "error", "msg"))
		}
	}
	if msg == "" {
		msg = http.StatusText(status)
	}
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		msg = "CLIProxyAPI 管理密钥无效或远程管理未开启: " + msg
	case http.StatusNotFound:
		msg = "CLIProxyAPI 管理接口不存在: " + msg
	case http.StatusServiceUnavailable:
		msg = "CLIProxyAPI 管理服务不可用: " + msg
	}
	return ErrStatus(http.StatusBadGateway, fmt.Sprintf("CLIProxyAPI HTTP %d: %s", status, msg))
}

func cliproxyAuthFiles(raw any) []domain.CLIProxyAuthFile {
	items := cliproxyArray(raw)
	out := make([]domain.CLIProxyAuthFile, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		file := domain.CLIProxyAuthFile{
			Name:           cliproxyString(firstCLIProxy(m, "name", "filename", "file")),
			Provider:       cliproxyString(firstCLIProxy(m, "provider")),
			Type:           cliproxyString(firstCLIProxy(m, "type")),
			Status:         cliproxyString(firstCLIProxy(m, "status")),
			StatusMessage:  cliproxyString(firstCLIProxy(m, "status_message", "statusMessage", "message", "error")),
			Email:          cliproxyString(firstCLIProxy(m, "email", "account_email", "user_email")),
			AccountType:    cliproxyString(firstCLIProxy(m, "account_type", "accountType", "plan_type", "planType")),
			Account:        cliproxyString(firstCLIProxy(m, "account", "account_id", "accountId")),
			Source:         cliproxyString(firstCLIProxy(m, "source")),
			AuthIndex:      cliproxyString(firstCLIProxy(m, "auth_index", "authIndex")),
			Size:           cliproxyInt(firstCLIProxy(m, "size")),
			ModTime:        cliproxyTime(firstCLIProxy(m, "modtime", "modified", "mtime", "updated_at", "updatedAt")),
			CreatedAt:      cliproxyTime(firstCLIProxy(m, "created_at", "createdAt")),
			UpdatedAt:      cliproxyTime(firstCLIProxy(m, "updated_at", "updatedAt")),
			LastRefresh:    cliproxyTime(firstCLIProxy(m, "last_refresh", "lastRefresh")),
			Success:        cliproxyInt(firstCLIProxy(m, "success", "success_count", "successCount", "successful")),
			Failed:         cliproxyInt(firstCLIProxy(m, "failed", "failure", "fail_count", "failCount", "error_count", "errorCount")),
			RecentRequests: firstCLIProxy(m, "recent_requests", "recentRequests"),
			RuntimeOnly:    cliproxyBool(firstCLIProxy(m, "runtime_only", "runtimeOnly")),
			Disabled:       cliproxyBool(firstCLIProxy(m, "disabled")),
			Unavailable:    cliproxyBool(firstCLIProxy(m, "unavailable")),
		}
		if file.Name != "" {
			out = append(out, file)
		}
	}
	return out
}

func cliproxyArray(v any) []any {
	if a, ok := v.([]any); ok {
		return a
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	for _, key := range []string{"files", "auth_files", "data", "items"} {
		switch x := m[key].(type) {
		case []any:
			return x
		case map[string]any:
			if a := cliproxyArray(x); len(a) > 0 {
				return a
			}
		}
	}
	return nil
}

func firstCLIProxy(m map[string]any, keys ...string) any {
	for _, key := range keys {
		if v, ok := m[key]; ok {
			return v
		}
	}
	return nil
}

func cliproxyString(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case json.Number:
		return x.String()
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case bool:
		return strconv.FormatBool(x)
	default:
		return strings.TrimSpace(fmt.Sprint(x))
	}
}

func cliproxyInt(v any) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case int:
		return int64(x)
	case int64:
		return x
	case json.Number:
		n, _ := x.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		return n
	default:
		return 0
	}
}

func cliproxyBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return strings.EqualFold(strings.TrimSpace(x), "true") || strings.TrimSpace(x) == "1"
	case float64:
		return x != 0
	case int:
		return x != 0
	default:
		return false
	}
}

func cliproxyTime(v any) string {
	if s := cliproxyString(v); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 0 {
			if n > 1e12 {
				return time.UnixMilli(n).UTC().Format(time.RFC3339)
			}
			if n > 1e9 {
				return time.Unix(n, 0).UTC().Format(time.RFC3339)
			}
		}
		return s
	}
	return ""
}

func cliproxyStrings(v any) []string {
	items := cliproxyArray(v)
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s := cliproxyString(item); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func cliproxyStatusRank(file domain.CLIProxyAuthFile) int {
	status := strings.ToLower(file.Status + " " + file.StatusMessage)
	switch {
	case file.Unavailable || strings.Contains(status, "error") || strings.Contains(status, "fail"):
		return 0
	case file.Disabled || strings.Contains(status, "disabled"):
		return 1
	case status == "" || strings.Contains(status, "unknown"):
		return 2
	default:
		return 3
	}
}
