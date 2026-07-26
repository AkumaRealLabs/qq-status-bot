package epay

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

// HTTP 为空时的兜底，避免易支付无响应时无限期挂住收入查询。
var defaultHTTPClient = &http.Client{Timeout: 30 * time.Second}

type Config struct {
	BaseURL string
	PID     string
	Key     string
}

type Client struct {
	HTTP *http.Client
}

type BalanceResult struct {
	Balance   float64
	CheckedAt time.Time
	Error     string
}

type Order struct {
	RemoteID    string
	Amount      float64
	Status      string
	PaymentType string
	PaidAt      time.Time
}

func (c Client) MerchantBalance(ctx context.Context, cfg Config) BalanceResult {
	out := BalanceResult{CheckedAt: time.Now().UTC()}
	cfg = normalizeConfig(cfg)
	if cfg.BaseURL == "" || cfg.PID == "" || cfg.Key == "" {
		out.Error = "请先在收入卡片里填写易支付 Base URL、PID、Key"
		return out
	}
	if balance, err := c.balance(ctx, cfg); err != nil {
		out.Error = err.Error()
	} else {
		out.Balance = balance
	}
	return out
}

func (c Client) TodayOrders(ctx context.Context, cfg Config, start time.Time) ([]Order, error) {
	cfg = normalizeConfig(cfg)
	if cfg.BaseURL == "" || cfg.PID == "" || cfg.Key == "" {
		return nil, errors.New("请先在收入卡片里填写易支付 Base URL、PID、Key")
	}
	var out []Order
	for page := 1; page <= 20; page++ {
		raw, err := c.get(ctx, cfg, url.Values{"act": {"orders"}, "limit": {"50"}, "page": {strconv.Itoa(page)}})
		if err != nil {
			return out, err
		}
		if err := apiError(raw); err != nil {
			return out, err
		}
		items := orderItems(raw)
		if len(items) == 0 {
			return out, nil
		}
		stop := false
		for _, row := range items {
			at, ok := orderTime(row)
			if ok && at.Before(start) {
				stop = true
				continue
			}
			if !ok || text(first(row, "status")) != "1" {
				continue
			}
			amount, err := number(first(row, "money", "amount"))
			if err != nil {
				continue
			}
			out = append(out, Order{
				RemoteID:    strings.TrimSpace(text(first(row, "trade_no", "out_trade_no", "api_trade_no"))),
				Amount:      amount,
				Status:      "success",
				PaymentType: text(first(row, "type", "payment_type")),
				PaidAt:      at,
			})
		}
		if stop || len(items) < 50 {
			return out, nil
		}
	}
	return out, nil
}

func (c Client) balance(ctx context.Context, cfg Config) (float64, error) {
	raw, err := c.get(ctx, cfg, url.Values{"act": {"query"}})
	if err != nil {
		return 0, err
	}
	if err := apiError(raw); err != nil {
		return 0, err
	}
	return number(first(raw, "money", "balance", "merchant_balance"))
}

func (c Client) get(ctx context.Context, cfg Config, q url.Values) (map[string]any, error) {
	q.Set("pid", cfg.PID)
	q.Set("key", cfg.Key)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.BaseURL+"/api.php?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	hc := c.HTTP
	if hc == nil {
		hc = defaultHTTPClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求易支付失败: %s", safeError(err))
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var out map[string]any
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.UseNumber()
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func normalizeConfig(cfg Config) Config {
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	cfg.PID = strings.TrimSpace(cfg.PID)
	cfg.Key = strings.TrimSpace(cfg.Key)
	return cfg
}

func apiError(raw map[string]any) error {
	code := strings.TrimSpace(text(first(raw, "code")))
	if code == "" || code == "1" {
		return nil
	}
	if msg := strings.TrimSpace(text(first(raw, "msg", "message", "error"))); msg != "" {
		return errors.New(msg)
	}
	return errors.New("易支付返回 code=" + code)
}

func number(v any) (float64, error) {
	switch vv := v.(type) {
	case json.Number:
		return vv.Float64()
	case float64:
		return vv, nil
	case int:
		return float64(vv), nil
	case string:
		return strconv.ParseFloat(strings.TrimSpace(vv), 64)
	default:
		return 0, fmt.Errorf("invalid number %v", v)
	}
}

func orderItems(raw map[string]any) []map[string]any {
	var out []map[string]any
	for _, item := range array(raw) {
		if row := object(item); len(row) != 0 {
			out = append(out, row)
		}
	}
	return out
}

func orderTime(row map[string]any) (time.Time, bool) {
	for _, key := range []string{"endtime", "pay_time", "paid_at", "addtime", "created_at"} {
		if t, ok := parseTime(row[key]); ok {
			return t, true
		}
	}
	return time.Time{}, false
}

func parseTime(v any) (time.Time, bool) {
	s := strings.TrimSpace(text(v))
	if s == "" || s == "0000-00-00 00:00:00" {
		return time.Time{}, false
	}
	if n, err := strconv.ParseFloat(s, 64); err == nil {
		if n > 1e12 {
			n /= 1000
		}
		return time.Unix(int64(n), 0), true
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func array(v any) []any {
	if vv, ok := v.([]any); ok {
		return vv
	}
	if m := object(v); len(m) != 0 {
		for _, key := range []string{"data", "items", "list", "orders", "rows"} {
			if items := array(m[key]); len(items) != 0 {
				return items
			}
		}
	}
	return nil
}

func object(v any) map[string]any {
	if vv, ok := v.(map[string]any); ok {
		return vv
	}
	return nil
}

func first(m map[string]any, keys ...string) any {
	for _, key := range keys {
		if v, ok := m[key]; ok {
			return v
		}
	}
	return nil
}

func text(v any) string {
	switch vv := v.(type) {
	case string:
		return vv
	case json.Number:
		return vv.String()
	case float64:
		return strconv.FormatFloat(vv, 'f', -1, 64)
	case int:
		return strconv.Itoa(vv)
	default:
		return ""
	}
}

func safeError(err error) string {
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		return urlErr.Err.Error()
	}
	return err.Error()
}
