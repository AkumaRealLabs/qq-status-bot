package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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

type cliProxyAuthUpload struct {
	Name    string
	Content string
}

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
	content = strings.TrimSpace(content)
	files, err := normalizeCLIProxyAuthUploads(name, content)
	if err != nil {
		return err
	}
	cfg, err := s.cliProxyConfig(ctx)
	if err != nil {
		return err
	}
	for _, file := range files {
		if err := s.uploadCLIProxyAuthFile(ctx, cfg, file.Name, file.Content); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) uploadCLIProxyAuthFile(ctx context.Context, cfg domain.CLIProxyConfig, name, content string) error {
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

func normalizeCLIProxyAuthJSON(content string) (string, error) {
	files, err := normalizeCLIProxyAuthUploads("", content)
	if err != nil {
		return "", err
	}
	if len(files) != 1 {
		return "", ErrBadRequest("授权文件包含多个账号")
	}
	return files[0].Content, nil
}

func normalizeCLIProxyAuthUploads(name, content string) ([]cliProxyAuthUpload, error) {
	dec := json.NewDecoder(strings.NewReader(content))
	dec.UseNumber()
	var raw any
	if err := dec.Decode(&raw); err != nil {
		return nil, ErrBadRequest("授权文件内容不是有效 JSON")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, ErrBadRequest("授权文件只能包含一个 JSON 值")
	}
	items, err := cliProxyAuthUploadValues(raw)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, ErrBadRequest("授权文件没有可上传的账号")
	}
	files := make([]cliProxyAuthUpload, 0, len(items))
	used := map[string]int{}
	for _, item := range items {
		b, err := json.Marshal(item)
		if err != nil {
			return nil, err
		}
		fileName := ""
		if len(items) == 1 {
			fileName = name
		}
		if strings.TrimSpace(fileName) == "" {
			fileName = defaultCLIProxyAuthFileName(string(b))
		}
		fileName = uniqueCLIProxyAuthFileName(fileName, used)
		if err := domain.ValidateCLIProxyAuthFileName(fileName); err != nil {
			return nil, BadRequest(err)
		}
		files = append(files, cliProxyAuthUpload{Name: fileName, Content: string(b)})
	}
	return files, nil
}

func cliProxyAuthUploadValues(raw any) ([]map[string]any, error) {
	switch v := raw.(type) {
	case map[string]any:
		return cliProxyAuthUploadObjects(v)
	case []any:
		out := make([]map[string]any, 0, len(v))
		for _, item := range v {
			items, err := cliProxyAuthUploadValues(item)
			if err != nil {
				return nil, err
			}
			out = append(out, items...)
		}
		return out, nil
	case string:
		token := strings.TrimSpace(v)
		if token == "" {
			return nil, ErrBadRequest("授权 token 不能为空")
		}
		return []map[string]any{{"type": "codex", "auth_kind": "oauth", "access_token": token}}, nil
	default:
		return nil, ErrBadRequest("授权文件内容必须是 JSON 对象")
	}
}

func cliProxyAuthUploadObjects(raw map[string]any) ([]map[string]any, error) {
	if accounts, ok := raw["accounts"].([]any); ok {
		out := make([]map[string]any, 0, len(accounts))
		for _, item := range accounts {
			account, ok := item.(map[string]any)
			if !ok {
				return nil, ErrBadRequest("sub2api 账号格式无效")
			}
			converted, err := cliProxySub2APIAccountAuth(account)
			if err != nil {
				return nil, err
			}
			out = append(out, converted)
		}
		return out, nil
	}
	if _, ok := raw["credentials"].(map[string]any); ok || cliProxyStringAt(raw, "platform") != "" {
		out, err := cliProxySub2APIAccountAuth(raw)
		return []map[string]any{out}, err
	}
	if cliProxyHasAny(raw, "accessToken", "refreshToken", "idToken", "sessionToken", "tokens") {
		out, err := cliProxySub2APIAccountAuth(raw)
		return []map[string]any{out}, err
	}
	if strings.EqualFold(cliProxyStringAt(raw, "type"), "oauth") {
		out, err := cliProxySub2APIAccountAuth(raw)
		return []map[string]any{out}, err
	}
	return []map[string]any{raw}, nil
}

func cliProxySub2APIAccountAuth(raw map[string]any) (map[string]any, error) {
	if platform := strings.ToLower(cliProxyStringAt(raw, "platform")); platform != "" && platform != "openai" {
		return nil, ErrBadRequest("只支持 sub2api OpenAI 账号")
	}
	if typ := strings.ToLower(cliProxyStringAt(raw, "type")); typ != "" && typ != "oauth" && typ != "codex" {
		return nil, ErrBadRequest("只支持 sub2api OpenAI OAuth 账号")
	}
	creds, _ := raw["credentials"].(map[string]any)
	out := make(map[string]any, len(creds)+8)
	for key, value := range creds {
		out[key] = value
	}
	out["type"] = "codex"
	out["auth_kind"] = "oauth"
	cliProxySetString(out, "access_token", cliProxyFirstString(creds, raw, []string{"access_token"}, []string{"accessToken"}, []string{"tokens", "access_token"}, []string{"tokens", "accessToken"}, []string{"token"}))
	cliProxySetString(out, "refresh_token", cliProxyFirstString(creds, raw, []string{"refresh_token"}, []string{"refreshToken"}, []string{"tokens", "refresh_token"}, []string{"tokens", "refreshToken"}))
	cliProxySetString(out, "id_token", cliProxyFirstString(creds, raw, []string{"id_token"}, []string{"idToken"}, []string{"tokens", "id_token"}, []string{"tokens", "idToken"}))
	cliProxySetString(out, "email", cliProxyFirstString(creds, raw, []string{"email"}, []string{"user", "email"}))
	cliProxySetString(out, "account_id", cliProxyFirstString(creds, raw, []string{"account_id"}, []string{"chatgpt_account_id"}, []string{"accountId"}, []string{"chatgptAccountId"}, []string{"account", "id"}, []string{"account", "account_id"}, []string{"account", "chatgpt_account_id"}))
	cliProxySetString(out, "plan_type", cliProxyFirstString(creds, raw, []string{"plan_type"}, []string{"planType"}, []string{"account", "plan_type"}, []string{"account", "planType"}))
	cliProxySetString(out, "expired", cliProxyFirstString(creds, raw, []string{"expired"}, []string{"expires_at"}, []string{"expiresAt"}, []string{"tokens", "expires_at"}, []string{"tokens", "expiresAt"}))
	delete(out, "session_token")
	delete(out, "sessionToken")
	if cliProxyStringAt(out, "access_token") == "" && cliProxyStringAt(out, "refresh_token") == "" {
		return nil, ErrBadRequest("sub2api 账号缺少 access_token 或 refresh_token")
	}
	return out, nil
}

func cliProxyFirstString(creds, raw map[string]any, paths ...[]string) string {
	for _, source := range []map[string]any{creds, raw} {
		for _, path := range paths {
			if s := cliProxyStringPath(source, path...); s != "" {
				return s
			}
		}
	}
	return ""
}

func cliProxyStringPath(m map[string]any, path ...string) string {
	if len(path) == 0 || m == nil {
		return ""
	}
	var cur any = m
	for _, key := range path {
		next, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = next[key]
	}
	return cliproxyString(cur)
}

func cliProxyStringAt(m map[string]any, key string) string {
	return cliProxyStringPath(m, key)
}

func cliProxySetString(m map[string]any, key, value string) {
	if strings.TrimSpace(value) != "" {
		m[key] = value
	}
}

func cliProxyHasAny(m map[string]any, keys ...string) bool {
	for _, key := range keys {
		if _, ok := m[key]; ok {
			return true
		}
	}
	return false
}

func defaultCLIProxyAuthFileName(content string) string {
	var raw map[string]any
	_ = json.Unmarshal([]byte(content), &raw)
	provider := cliProxyFileSlug(cliproxyString(firstCLIProxy(raw, "type", "provider")))
	if provider == "" && cliProxyHasAny(raw, "access_token", "refresh_token", "id_token") {
		provider = "codex"
	}
	if provider == "" {
		provider = "auth"
	}
	parts := make([]string, 0, 2)
	for _, key := range []string{"email", "account_id", "chatgpt_account_id"} {
		if part := cliProxyFileSlug(cliproxyString(firstCLIProxy(raw, key))); part != "" {
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		for _, key := range []string{"name", "label"} {
			if part := cliProxyFileSlug(cliproxyString(firstCLIProxy(raw, key))); part != "" {
				parts = append(parts, part)
				break
			}
		}
	}
	if len(parts) > 0 {
		return provider + "-" + strings.Join(parts, "-") + ".json"
	}
	sum := sha256.Sum256([]byte(content))
	return provider + "-" + hex.EncodeToString(sum[:])[:10] + ".json"
}

func cliProxyFileSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	dash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case r == '.', r == '_', r == '-', r == '@':
			if !dash {
				b.WriteByte('-')
				dash = true
			}
		}
		if b.Len() >= 64 {
			break
		}
	}
	return strings.Trim(b.String(), "-")
}

func uniqueCLIProxyAuthFileName(name string, used map[string]int) string {
	base := name
	if strings.HasSuffix(strings.ToLower(base), ".json") {
		base = base[:len(base)-len(".json")]
	}
	ext := ".json"
	key := strings.ToLower(name)
	used[key]++
	if used[key] == 1 {
		return name
	}
	return fmt.Sprintf("%s-%d%s", base, used[key], ext)
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
