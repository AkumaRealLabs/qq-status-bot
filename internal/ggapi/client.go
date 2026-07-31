package ggapi

import (
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

const (
	maxResponseBytes = 2 << 20
	defaultTimeout   = 15 * time.Second
	pageSize         = 100
)

var ErrNotFound = errors.New("GGAPI 账号不存在")

// Client 只实现 GGAPI 管理端的只读 GET 接口。
type Client struct {
	BaseURL    string
	AdminToken string
	HTTP       *http.Client
	Timeout    time.Duration
}

type User struct {
	ID       string
	Email    string
	Username string
	Role     string
	Status   string
	Deleted  bool
	Quota    float64
}

type Balance struct {
	Amount       float64
	Currency     string
	Quota        float64
	QuotaPerUnit float64
	ExchangeRate float64
}

func (c Client) VerifyEmail(ctx context.Context, email string) (User, error) {
	email = normalizeEmail(email)
	if email == "" {
		return User{}, errors.New("邮箱格式无效")
	}
	users, err := c.searchUsers(ctx, email)
	if err != nil {
		return User{}, err
	}
	matches := make([]User, 0, len(users))
	for _, user := range users {
		if normalizeEmail(user.Email) == email {
			matches = append(matches, user)
		}
	}
	if len(matches) != 1 {
		return User{}, errors.New("GGAPI 邮箱对应的账号数量不符合要求")
	}
	user := matches[0]
	if err := validateUser(user, email); err != nil {
		return User{}, err
	}
	return user, nil
}

func (c Client) GetUser(ctx context.Context, id string) (User, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return User{}, errors.New("GGAPI 用户 ID 为空")
	}
	var raw json.RawMessage
	if err := c.get(ctx, "/api/user/"+url.PathEscape(id), nil, &raw); err != nil {
		return User{}, err
	}
	if responseNotFound(raw) {
		return User{}, ErrNotFound
	}
	user, ok := decodeUser(raw)
	if !ok {
		return User{}, errors.New("GGAPI 用户响应格式无效")
	}
	return user, nil
}

func (c Client) Balance(ctx context.Context, user User) (Balance, error) {
	var raw json.RawMessage
	if err := c.get(ctx, "/api/status", nil, &raw); err != nil {
		return Balance{}, err
	}
	status := decodeStatus(raw)
	if status.QuotaPerUnit <= 0 {
		return Balance{}, errors.New("GGAPI 余额配置无效")
	}
	amountUSD := user.Quota / status.QuotaPerUnit
	amount, currency, rate := amountUSD, "USD", 1.0
	switch strings.ToUpper(strings.TrimSpace(status.DisplayType)) {
	case "USD", "":
	case "CNY":
		if status.USDExchangeRate <= 0 {
			return Balance{}, errors.New("GGAPI 美元汇率无效")
		}
		amount, currency, rate = amountUSD*status.USDExchangeRate, "CNY", status.USDExchangeRate
	case "CUSTOM":
		if status.CustomExchangeRate <= 0 || strings.TrimSpace(status.CustomCurrencySymbol) == "" {
			return Balance{}, errors.New("GGAPI 自定义币种配置无效")
		}
		amount, currency, rate = amountUSD*status.CustomExchangeRate, strings.TrimSpace(status.CustomCurrencySymbol), status.CustomExchangeRate
	case "TOKENS":
		amount, currency = user.Quota, "Tokens"
	default:
		return Balance{}, errors.New("GGAPI 余额显示类型无效")
	}
	return Balance{Amount: amount, Currency: currency, Quota: user.Quota,
		QuotaPerUnit: status.QuotaPerUnit, ExchangeRate: rate}, nil
}

func (c Client) searchUsers(ctx context.Context, email string) ([]User, error) {
	var all []User
	for page := 1; page <= 1000; page++ {
		query := url.Values{}
		query.Set("keyword", email)
		query.Set("page", strconv.Itoa(page))
		query.Set("p", strconv.Itoa(page))
		query.Set("page_size", strconv.Itoa(pageSize))
		var raw json.RawMessage
		if err := c.get(ctx, "/api/user/search", query, &raw); err != nil {
			return nil, err
		}
		users, total, hasMore, ok := decodeUsers(raw)
		if !ok {
			return nil, errors.New("GGAPI 用户搜索响应格式无效")
		}
		all = append(all, users...)
		if len(users) == 0 || (!hasMore && (total <= 0 || len(all) >= total) && len(users) < pageSize) {
			break
		}
	}
	return all, nil
}

func (c Client) get(ctx context.Context, path string, query url.Values, out *json.RawMessage) error {
	base, err := validateBaseURL(c.BaseURL)
	if err != nil {
		return err
	}
	if strings.TrimSpace(c.AdminToken) == "" {
		return errors.New("GGAPI 管理令牌未配置")
	}
	u := *base
	u.Path = strings.TrimRight(u.Path, "/") + path
	u.RawQuery = query.Encode()
	requestCtx, cancel := context.WithTimeout(ctx, c.timeout())
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.AdminToken))
	req.Header.Set("Accept", "application/json")
	client := *c.httpClient()
	previousRedirect := client.CheckRedirect
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if next.URL.Scheme != "https" {
			return errors.New("GGAPI 重定向到非 HTTPS 地址")
		}
		if previousRedirect != nil {
			return previousRedirect(next, via)
		}
		return nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("GGAPI 查询失败")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return errors.New("GGAPI 查询失败")
	}
	if len(body) > maxResponseBytes {
		return errors.New("GGAPI 响应过大")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		if resp.StatusCode == http.StatusNotFound {
			return ErrNotFound
		}
		return fmt.Errorf("GGAPI 查询失败（HTTP %d）", resp.StatusCode)
	}
	if !json.Valid(body) {
		return errors.New("GGAPI 返回了无效 JSON")
	}
	*out = append((*out)[:0], body...)
	return nil
}

func validateBaseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.Scheme != "https" || u.User != nil {
		return nil, errors.New("GGAPI 地址必须是 HTTPS URL")
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u, nil
}

func (c Client) timeout() time.Duration {
	if c.Timeout > 0 && c.Timeout <= defaultTimeout {
		return c.Timeout
	}
	return defaultTimeout
}

func (c Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: c.timeout()}
}

func normalizeEmail(raw string) string { return strings.ToLower(strings.TrimSpace(raw)) }

func validateUser(user User, expectedEmail string) error {
	if user.ID == "" || normalizeEmail(user.Email) != expectedEmail {
		return errors.New("GGAPI 账号身份不匹配")
	}
	if user.Deleted || !enabledStatus(user.Status) || !normalRole(user.Role) {
		return errors.New("GGAPI 账号不是启用中的普通用户")
	}
	return nil
}

func enabledStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "1", "active", "enabled", "normal", "正常":
		return true
	case "0", "-1", "disabled", "inactive", "deleted", "禁用":
		return false
	default:
		return false
	}
}

func normalRole(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "1", "user", "normal", "普通用户", "common":
		return true
	case "admin", "administrator", "root", "superadmin", "管理员":
		return false
	default:
		return false
	}
}

func decodeUsers(raw []byte) ([]User, int, bool, bool) {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return nil, 0, false, false
	}
	root, _ := value.(map[string]any)
	container := value
	if root != nil {
		if data, ok := root["data"]; ok {
			container = data
		}
	}
	var list []any
	var total int
	var hasMore bool
	switch item := container.(type) {
	case []any:
		list = item
	case map[string]any:
		for _, key := range []string{"items", "list", "users", "data"} {
			if candidate, ok := item[key].([]any); ok {
				list = candidate
				break
			}
		}
		total = intNumber(item["total"])
		if pagination, ok := item["pagination"].(map[string]any); ok && total == 0 {
			total = intNumber(pagination["total"])
		}
		if value, ok := item["has_more"].(bool); ok {
			hasMore = value
		} else if value, ok := item["hasMore"].(bool); ok {
			hasMore = value
		}
	}
	if root != nil {
		if total == 0 {
			total = intNumber(root["total"])
		}
		if !hasMore {
			hasMore, _ = root["has_more"].(bool)
		}
		if !hasMore {
			hasMore, _ = root["hasMore"].(bool)
		}
	}
	users := make([]User, 0, len(list))
	for _, item := range list {
		encoded, _ := json.Marshal(item)
		if user, ok := decodeUser(encoded); ok {
			users = append(users, user)
		}
	}
	return users, total, hasMore, list != nil
}

func decodeUser(raw []byte) (User, bool) {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return User{}, false
	}
	if root, ok := value.(map[string]any); ok {
		for _, key := range []string{"data", "user"} {
			if data, exists := root[key]; exists {
				if nested, ok := data.(map[string]any); ok && (nested["email"] != nil || nested["id"] != nil || nested["user_id"] != nil) {
					value = nested
					break
				}
			}
		}
	}
	item, ok := value.(map[string]any)
	if !ok {
		return User{}, false
	}
	user := User{ID: stringValue(item, "id", "user_id", "userId"), Email: stringValue(item, "email", "mail"),
		Username: stringValue(item, "username", "user_name", "userName", "name"), Role: stringValue(item, "role", "user_role"),
		Status: stringValue(item, "status", "state")}
	user.Deleted = boolValue(item, "deleted", "is_deleted") || strings.TrimSpace(stringValue(item, "deleted_at", "deletedAt", "DeletedAt")) != ""
	user.Quota = floatValue(item, "quota", "balance")
	if user.ID == "" && item["id"] != nil {
		user.ID = fmt.Sprint(item["id"])
	}
	return user, user.ID != "" || user.Email != ""
}

type decodedStatus struct {
	QuotaPerUnit         float64
	DisplayType          string
	USDExchangeRate      float64
	CustomCurrencySymbol string
	CustomExchangeRate   float64
}

func decodeStatus(raw []byte) decodedStatus {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return decodedStatus{}
	}
	if root, ok := value.(map[string]any); ok {
		if data, exists := root["data"]; exists {
			if nested, ok := data.(map[string]any); ok {
				value = nested
			}
		}
	}
	item, _ := value.(map[string]any)
	if item == nil {
		return decodedStatus{}
	}
	status := decodedStatus{
		QuotaPerUnit:         floatValue(item, "quota_per_unit", "quotaPerUnit", "quota_unit"),
		DisplayType:          stringValue(item, "quota_display_type", "quotaDisplayType"),
		USDExchangeRate:      floatValue(item, "usd_exchange_rate", "usdExchangeRate"),
		CustomCurrencySymbol: stringValue(item, "custom_currency_symbol", "customCurrencySymbol"),
		CustomExchangeRate:   floatValue(item, "custom_currency_exchange_rate", "customCurrencyExchangeRate"),
	}
	return status
}

func responseNotFound(raw []byte) bool {
	var response struct {
		Success *bool  `json:"success"`
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &response) != nil || response.Success == nil || *response.Success {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(response.Message))
	for _, marker := range []string{"not found", "no such", "不存在", "未找到"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func stringValue(item map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := item[key]; ok && value != nil {
			switch typed := value.(type) {
			case string:
				return strings.TrimSpace(typed)
			default:
				return strings.TrimSpace(fmt.Sprint(typed))
			}
		}
	}
	return ""
}

func floatValue(item map[string]any, keys ...string) float64 {
	for _, key := range keys {
		if value, ok := item[key]; ok {
			switch typed := value.(type) {
			case float64:
				return typed
			case json.Number:
				parsed, _ := typed.Float64()
				return parsed
			case string:
				parsed, _ := strconv.ParseFloat(strings.TrimSpace(typed), 64)
				return parsed
			}
		}
	}
	return 0
}

func intNumber(value any) int {
	if value == nil {
		return 0
	}
	return int(floatValue(map[string]any{"value": value}, "value"))
}

func boolValue(item map[string]any, keys ...string) bool {
	for _, key := range keys {
		if value, ok := item[key]; ok {
			if typed, ok := value.(bool); ok {
				return typed
			}
			return strings.EqualFold(strings.TrimSpace(fmt.Sprint(value)), "true") || fmt.Sprint(value) == "1"
		}
	}
	return false
}
