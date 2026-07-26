package monitor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// HTTP 为空时的兜底：宁可超时报错，也不要让上游 hang 住整轮刷新。
var defaultHTTPClient = &http.Client{Timeout: 45 * time.Second}

type Client struct {
	HTTP    *http.Client
	Browser BrowserHTTPClient
}

type BrowserHTTPClient interface {
	Do(ctx context.Context, method, rawURL string, body []byte, headers map[string]string) (status int, responseBody []byte, err error)
}

type upstreamGroup struct {
	ID          string
	Name        string
	Description string
	Ratio       string
}

func (c Client) Check(ctx context.Context, u *Upstream) (CheckResult, error) {
	var out CheckResult
	var err error

	switch u.Type {
	case "newapi":
		out.Balance, err = c.newapiBalance(ctx, u)
		if err != nil {
			return out, err
		}
		out.Keys, err = c.newapiKeys(ctx, u)
	case "sub2api":
		if err = c.sub2apiAuth(ctx, u); err != nil {
			return out, err
		}
		out.Balance, err = c.sub2apiBalance(ctx, u)
		if IsAuthError(err) {
			if err = c.sub2apiForceAuth(ctx, u); err != nil {
				return out, err
			}
			out.Balance, err = c.sub2apiBalance(ctx, u)
		}
		if err != nil {
			return out, err
		}
		out.Keys, err = c.sub2apiKeys(ctx, u)
		if IsAuthError(err) {
			if err = c.sub2apiForceAuth(ctx, u); err != nil {
				return out, err
			}
			out.Keys, err = c.sub2apiKeys(ctx, u)
		}
	default:
		err = fmt.Errorf("unsupported upstream type %q", u.Type)
	}
	if err != nil {
		return out, err
	}
	return out, nil
}

func (c Client) newapiBalance(ctx context.Context, u *Upstream) (Balance, error) {
	var raw map[string]any
	if err := c.doJSON(ctx, http.MethodGet, joinURL(u.BaseURL, "/api/user/self"), nil, newapiHeaders(u), &raw); err != nil {
		return Balance{}, markAuth(err)
	}
	data := obj(raw["data"])
	if len(data) == 0 {
		data = raw
	}
	return Balance{
		Balance:  num(first(data, "quota", "balance", "remain_quota", "remain")),
		Used:     num(first(data, "used_quota", "used", "used_amount")),
		Remain:   num(first(data, "remain_quota", "remain", "quota", "balance")),
		Requests: int(num(first(data, "request_count", "requests", "used_count"))),
	}, nil
}

func (c Client) newapiKeys(ctx context.Context, u *Upstream) ([]APIKey, error) {
	var raw map[string]any
	if err := c.doJSON(ctx, http.MethodGet, joinURL(u.BaseURL, "/api/token/"), nil, newapiHeaders(u), &raw); err != nil {
		return nil, markAuth(err)
	}
	groups := c.newapiGroups(ctx, u)
	items := array(first(raw, "data", "tokens", "items"))
	keys := make([]APIKey, 0, len(items))
	for _, item := range items {
		m := obj(item)
		id := str(first(m, "id", "token_id"))
		key := str(first(m, "key"))
		groupName := str(first(m, "group"))
		group := groups[groupName]
		if group.Name == "" {
			group.Name = groupName
		}
		if id != "" && (key == "" || strings.Contains(key, "*")) {
			var full map[string]any
			if err := c.doJSON(ctx, http.MethodPost, joinURL(u.BaseURL, "/api/token/"+url.PathEscape(id)+"/key"), nil, newapiHeaders(u), &full); err != nil {
				return nil, markAuth(err)
			}
			key = str(first(full, "key", "data"))
			if key == "" {
				key = str(first(obj(full["data"]), "key"))
			}
		}
		keys = append(keys, APIKey{
			RemoteID:    id,
			Name:        str(first(m, "name", "token_name")),
			Key:         key,
			Status:      str(first(m, "status")),
			Description: keyDescription(m, group),
			Group:       group.Name,
			GroupRatio:  group.Ratio,
			Quota:       num(first(m, "quota", "remain_quota")),
			UsedQuota:   num(first(m, "used_quota", "used")),
		})
	}
	return keys, nil
}

func (c Client) newapiGroups(ctx context.Context, u *Upstream) map[string]upstreamGroup {
	if groups, err := c.fetchNewapiGroups(ctx, u, "/api/user/self/groups"); err == nil {
		return groups
	}
	if groups, err := c.fetchNewapiGroups(ctx, u, "/api/user/groups"); err == nil {
		return groups
	}
	return nil
}

func (c Client) fetchNewapiGroups(ctx context.Context, u *Upstream, path string) (map[string]upstreamGroup, error) {
	var raw map[string]any
	if err := c.doJSON(ctx, http.MethodGet, joinURL(u.BaseURL, path), nil, newapiHeaders(u), &raw); err != nil {
		return nil, err
	}
	data := obj(raw["data"])
	groups := make(map[string]upstreamGroup, len(data))
	for name, value := range data {
		group := groupFromMap(obj(value))
		group.Name = name
		if group.Description == "" {
			group.Description = group.Name
		}
		groups[name] = group
	}
	return groups, nil
}

func (c Client) sub2apiAuth(ctx context.Context, u *Upstream) error {
	if u.Sub2APIAccessToken != "" {
		return nil
	}
	return c.sub2apiForceAuth(ctx, u)
}

func (c Client) sub2apiForceAuth(ctx context.Context, u *Upstream) error {
	hadToken := u.Sub2APIAccessToken != "" || u.Sub2APIRefreshToken != ""
	if u.Sub2APIRefreshToken != "" {
		var raw map[string]any
		err := c.doJSON(ctx, http.MethodPost, joinURL(u.BaseURL, "/api/v1/auth/refresh"), map[string]string{
			"refresh_token": u.Sub2APIRefreshToken,
		}, nil, &raw)
		if err == nil && applySub2Tokens(u, raw) {
			return nil
		}
	}
	if strings.TrimSpace(u.Email) == "" || u.Password == "" {
		if hadToken {
			return AuthError{Err: errors.New("sub2api Token 已失效且未配置邮箱密码；请重新配置邮箱密码自动登录，或通过“CF 浏览器登录”完成验证并采集 Token")}
		}
		return AuthError{Err: errors.New("sub2api 未配置可用登录凭据；请配置邮箱密码自动登录，或通过“CF 浏览器登录”完成验证并采集 Token")}
	}

	var raw map[string]any
	err := c.doJSON(ctx, http.MethodPost, joinURL(u.BaseURL, "/api/v1/auth/login"), map[string]string{
		"email":    u.Email,
		"password": u.Password,
	}, nil, &raw)
	if err != nil {
		if requiresSub2APIBrowserLogin(err) {
			return AuthError{Err: fmt.Errorf("sub2api 邮箱密码登录遇到 CF 验证或浏览器绑定，请使用“CF 浏览器登录”完成验证并采集 Token: %w", err)}
		}
		return AuthError{Err: err}
	}
	if !applySub2Tokens(u, raw) {
		return AuthError{Err: errors.New("sub2api login did not return access token")}
	}
	return nil
}

func requiresSub2APIBrowserLogin(err error) bool {
	var statusErr httpStatusError
	if !errors.As(err, &statusErr) {
		return false
	}
	lower := strings.ToLower(statusErr.Message)
	return strings.Contains(lower, "cloudflare") || strings.Contains(lower, "cf-error-details") ||
		strings.Contains(lower, "error code: 1010") || strings.Contains(lower, "cf_chl_") ||
		strings.Contains(lower, "challenge-platform") || strings.Contains(lower, "just a moment") ||
		strings.Contains(lower, "session_binding_mismatch") || strings.Contains(lower, "session network fingerprint changed")
}

func (c Client) sub2apiBalance(ctx context.Context, u *Upstream) (Balance, error) {
	var raw map[string]any
	if err := c.doJSON(ctx, http.MethodGet, joinURL(u.BaseURL, "/api/v1/user/profile"), nil, bearer(u.Sub2APIAccessToken), &raw); err != nil {
		return Balance{}, markAuth(err)
	}
	data := obj(raw["data"])
	if len(data) == 0 {
		data = raw
	}
	return Balance{
		Balance:  num(first(data, "balance", "quota", "remaining_quota")),
		Used:     num(first(data, "used", "used_quota")),
		Remain:   num(first(data, "remaining_quota", "remain", "balance")),
		Requests: int(num(first(data, "request_count", "requests"))),
	}, nil
}

func (c Client) sub2apiKeys(ctx context.Context, u *Upstream) ([]APIKey, error) {
	var raw map[string]any
	err := c.doJSON(ctx, http.MethodGet, joinURL(u.BaseURL, "/api/v1/api-keys?page_size=200"), nil, bearer(u.Sub2APIAccessToken), &raw)
	if err != nil {
		raw = nil
		if fallbackErr := c.doJSON(ctx, http.MethodGet, joinURL(u.BaseURL, "/api/v1/keys"), nil, bearer(u.Sub2APIAccessToken), &raw); fallbackErr != nil {
			return nil, markAuth(err)
		}
	}
	groups := c.sub2apiGroups(ctx, u)
	items := array(first(raw, "data", "keys", "items"))
	keys := make([]APIKey, 0, len(items))
	for _, item := range items {
		m := obj(item)
		group := groupFromMap(obj(first(m, "group")))
		groupID := str(first(m, "group_id", "groupId"))
		if group.Name == "" && groupID != "" {
			group = groups[groupID]
		}
		keys = append(keys, APIKey{
			RemoteID:    str(first(m, "id")),
			Name:        str(first(m, "name")),
			Key:         str(first(m, "key", "api_key")),
			Status:      str(first(m, "status")),
			Description: keyDescription(m, group),
			Group:       group.Name,
			GroupRatio:  group.Ratio,
			Quota:       num(first(m, "quota", "remaining_quota")),
			UsedQuota:   num(first(m, "used_quota", "used")),
		})
	}
	return keys, nil
}

func (c Client) sub2apiGroups(ctx context.Context, u *Upstream) map[string]upstreamGroup {
	var raw map[string]any
	if err := c.doJSON(ctx, http.MethodGet, joinURL(u.BaseURL, "/api/v1/groups/available"), nil, bearer(u.Sub2APIAccessToken), &raw); err != nil {
		return nil
	}
	items := array(first(raw, "data", "groups", "items"))
	groups := make(map[string]upstreamGroup, len(items)*2)
	for _, item := range items {
		group := groupFromMap(obj(item))
		if group.ID != "" {
			groups[group.ID] = group
		}
		if group.Name != "" {
			groups[group.Name] = group
		}
	}
	return groups
}

func (c Client) doJSON(ctx context.Context, method, rawURL string, body any, headers map[string]string, out any) error {
	var bodyBytes []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyBytes = b
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	hc := c.HTTP
	if hc == nil {
		hc = defaultHTTPClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	status := resp.StatusCode
	if c.Browser != nil && shouldRetryInBrowser(rawURL, status, b) {
		browserHeaders := make(map[string]string, len(headers)+1)
		for key, value := range headers {
			browserHeaders[key] = value
		}
		if len(bodyBytes) > 0 {
			browserHeaders["Content-Type"] = "application/json"
		}
		status, b, err = c.Browser.Do(ctx, method, rawURL, bodyBytes, browserHeaders)
		if err != nil {
			return fmt.Errorf("上游要求浏览器会话，浏览器回退失败: %w", err)
		}
	}
	if status < 200 || status >= 300 {
		return httpStatusError{Status: status, Message: strings.TrimSpace(string(b))}
	}
	if out == nil || len(b) == 0 {
		return nil
	}
	if err := json.Unmarshal(b, out); err != nil {
		if msg := strings.TrimSpace(string(b)); msg != "" {
			return httpStatusError{Status: resp.StatusCode, Message: msg}
		}
		return err
	}
	return nil
}

func shouldRetryInBrowser(rawURL string, status int, body []byte) bool {
	lower := strings.ToLower(string(body))
	cloudflareBlocked := strings.Contains(lower, "error code: 1010") ||
		(strings.Contains(lower, "cloudflare ray id") && strings.Contains(lower, "cf-error-details"))
	browserBoundSession := strings.Contains(lower, "session_binding_mismatch") ||
		strings.Contains(lower, "session network fingerprint changed")
	if !((status == http.StatusForbidden && cloudflareBlocked) ||
		((status == http.StatusUnauthorized || status == http.StatusForbidden) && browserBoundSession)) {
		return false
	}
	u, err := url.Parse(rawURL)
	return err == nil && strings.HasPrefix(u.Path, "/api/v1/")
}

type httpStatusError struct {
	Status  int
	Message string
}

func (e httpStatusError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("http %d: %s", e.Status, http.StatusText(e.Status))
	}
	return fmt.Sprintf("http %d: %s", e.Status, e.Message)
}

type AuthError struct {
	Err error
}

func (e AuthError) Error() string { return e.Err.Error() }
func (e AuthError) Unwrap() error { return e.Err }

func IsAuthError(err error) bool {
	var ae AuthError
	return errors.As(err, &ae)
}

func markAuth(err error) error {
	var he httpStatusError
	if errors.As(err, &he) && (he.Status == http.StatusUnauthorized || he.Status == http.StatusForbidden) {
		return AuthError{Err: err}
	}
	return err
}

func newapiHeaders(u *Upstream) map[string]string {
	return map[string]string{
		"Authorization": u.AccessToken,
		"New-Api-User":  u.UserID,
	}
}

func bearer(token string) map[string]string {
	if token == "" {
		return nil
	}
	if strings.HasPrefix(strings.ToLower(token), "bearer ") {
		return map[string]string{"Authorization": token}
	}
	return map[string]string{"Authorization": "Bearer " + token}
}

func applySub2Tokens(u *Upstream, raw map[string]any) bool {
	data := obj(raw["data"])
	if len(data) == 0 {
		data = raw
	}
	access := str(first(data, "access_token", "accessToken", "token"))
	if access == "" {
		return false
	}
	u.Sub2APIAccessToken = access
	if refresh := str(first(data, "refresh_token", "refreshToken")); refresh != "" {
		u.Sub2APIRefreshToken = refresh
	}
	return true
}

func joinURL(base, path string) string { return strings.TrimRight(base, "/") + path }

func first(m map[string]any, keys ...string) any {
	for _, key := range keys {
		if v, ok := m[key]; ok {
			return v
		}
	}
	return nil
}

func groupFromMap(m map[string]any) upstreamGroup {
	if len(m) == 0 {
		return upstreamGroup{}
	}
	return upstreamGroup{
		ID:          str(first(m, "id", "group_id", "groupId")),
		Name:        str(first(m, "name", "group", "group_name", "groupName")),
		Description: str(first(m, "description", "desc", "remark", "memo", "note")),
		Ratio:       ratioText(first(m, "rate_multiplier", "rateMultiplier", "ratio", "group_ratio", "groupRatio")),
	}
}

func keyDescription(m map[string]any, group upstreamGroup) string {
	if desc := str(first(m, "description", "desc", "remark", "memo", "note")); desc != "" {
		return desc
	}
	if group.Description != "" {
		return group.Description
	}
	return group.Name
}

func ratioText(v any) string {
	switch vv := v.(type) {
	case string:
		return strings.TrimSuffix(strings.TrimSpace(vv), "x")
	case float64:
		return strconv.FormatFloat(vv, 'f', -1, 64)
	case int:
		return strconv.Itoa(vv)
	case int64:
		return strconv.FormatInt(vv, 10)
	case json.Number:
		return vv.String()
	default:
		return ""
	}
}

func obj(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func array(v any) []any {
	switch vv := v.(type) {
	case []any:
		return vv
	case map[string]any:
		return array(first(vv, "items", "records", "list"))
	default:
		return nil
	}
}

func str(v any) string {
	switch vv := v.(type) {
	case string:
		return vv
	case float64:
		return strconv.FormatFloat(vv, 'f', -1, 64)
	case int:
		return strconv.Itoa(vv)
	case int64:
		return strconv.FormatInt(vv, 10)
	case json.Number:
		return vv.String()
	default:
		return ""
	}
}

func num(v any) float64 {
	switch vv := v.(type) {
	case float64:
		return vv
	case int:
		return float64(vv)
	case json.Number:
		f, _ := vv.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(vv, 64)
		return f
	default:
		return 0
	}
}
