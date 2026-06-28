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

func (c Client) MerchantBalance(ctx context.Context, cfg Config) BalanceResult {
	out := BalanceResult{CheckedAt: time.Now().UTC()}
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	cfg.PID = strings.TrimSpace(cfg.PID)
	cfg.Key = strings.TrimSpace(cfg.Key)
	if cfg.BaseURL == "" || cfg.PID == "" || cfg.Key == "" {
		out.Error = "请先在设置里填写易支付 v1 配置"
		return out
	}
	if balance, err := c.balance(ctx, cfg); err != nil {
		out.Error = err.Error()
	} else {
		out.Balance = balance
	}
	return out
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
		hc = http.DefaultClient
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
