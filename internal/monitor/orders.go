package monitor

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const revenueOrderPageSize = 50

func (c Client) TodayOrderRevenue(ctx context.Context, u *Upstream, start time.Time) (RevenueOrderTotal, error) {
	switch u.Type {
	case "newapi":
		return c.newapiTodayOrderRevenue(ctx, u, start)
	case "sub2api":
		if strings.TrimSpace(u.AdminAPIKey) != "" {
			return c.sub2apiAdminTodayOrderRevenue(ctx, u, start)
		}
		if u.Sub2APIAccessToken == "" && u.Sub2APIRefreshToken == "" && (u.Email == "" || u.Password == "") {
			return RevenueOrderTotal{}, fmt.Errorf("sub2api 收入卡片需要配置管理员 API Key")
		}
		return c.sub2apiUserTodayOrderRevenue(ctx, u, start)
	default:
		return RevenueOrderTotal{}, fmt.Errorf("unsupported upstream type %q", u.Type)
	}
}

func (c Client) newapiTodayOrderRevenue(ctx context.Context, u *Upstream, start time.Time) (RevenueOrderTotal, error) {
	return c.sumTodayOrders(start, func(page int, raw *map[string]any) error {
		path := fmt.Sprintf("/api/user/topup/self?p=%d&page_size=%d", page, revenueOrderPageSize)
		return c.doJSON(ctx, http.MethodGet, joinURL(u.BaseURL, path), nil, newapiHeaders(u), raw)
	})
}

func (c Client) sub2apiAdminTodayOrderRevenue(ctx context.Context, u *Upstream, start time.Time) (RevenueOrderTotal, error) {
	return c.sumTodayOrders(start, func(page int, raw *map[string]any) error {
		path := fmt.Sprintf("/api/v1/admin/payment/orders?page=%d&page_size=%d", page, revenueOrderPageSize)
		return c.doJSON(ctx, http.MethodGet, joinURL(u.BaseURL, path), nil, map[string]string{"x-api-key": strings.TrimSpace(u.AdminAPIKey)}, raw)
	})
}

func (c Client) sub2apiUserTodayOrderRevenue(ctx context.Context, u *Upstream, start time.Time) (RevenueOrderTotal, error) {
	if err := c.sub2apiAuth(ctx, u); err != nil {
		return RevenueOrderTotal{}, err
	}
	out, err := c.sub2apiUserTodayOrderRevenueWithToken(ctx, u, start)
	if IsAuthError(err) {
		if err = c.sub2apiForceAuth(ctx, u); err != nil {
			return out, err
		}
		out, err = c.sub2apiUserTodayOrderRevenueWithToken(ctx, u, start)
	}
	return out, err
}

func (c Client) sub2apiUserTodayOrderRevenueWithToken(ctx context.Context, u *Upstream, start time.Time) (RevenueOrderTotal, error) {
	return c.sumTodayOrders(start, func(page int, raw *map[string]any) error {
		path := fmt.Sprintf("/api/v1/payment/orders?limit=%d&page=%d", revenueOrderPageSize, page)
		return markAuth(c.doJSON(ctx, http.MethodGet, joinURL(u.BaseURL, path), nil, bearer(u.Sub2APIAccessToken), raw))
	})
}

func (c Client) sumTodayOrders(start time.Time, fetch func(page int, raw *map[string]any) error) (RevenueOrderTotal, error) {
	out := RevenueOrderTotal{CheckedAt: time.Now().UTC()}
	for page := 1; page <= 20; page++ {
		var raw map[string]any
		if err := fetch(page, &raw); err != nil {
			return out, err
		}
		if err := apiPayloadError(raw); err != nil {
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
			if !ok || !successfulOrder(row) {
				continue
			}
			out.Revenue += orderAmount(row)
		}
		if stop {
			return out, nil
		}
		if len(items) < revenueOrderPageSize {
			return out, nil
		}
	}
	return out, nil
}

func orderItems(raw map[string]any) []map[string]any {
	var out []map[string]any
	for _, item := range findOrderArray(payload(raw)) {
		if row := obj(item); len(row) != 0 {
			out = append(out, row)
		}
	}
	return out
}

func findOrderArray(v any) []any {
	if items := array(v); len(items) != 0 {
		return items
	}
	m := obj(v)
	if len(m) == 0 {
		return nil
	}
	for _, key := range []string{"items", "records", "list", "orders", "topups", "data", "rows", "result"} {
		if items := findOrderArray(m[key]); len(items) != 0 {
			return items
		}
	}
	return nil
}

func successfulOrder(row map[string]any) bool {
	switch strings.ToLower(strings.TrimSpace(str(first(row, "status", "state", "trade_status", "payment_status")))) {
	case "success", "paid", "completed":
		return true
	default:
		return false
	}
}

func orderAmount(row map[string]any) float64 {
	return num(first(row, "money", "total_amount", "pay_amount", "actual_amount", "price", "value", "topup_amount", "amount"))
}

func orderTime(row map[string]any) (time.Time, bool) {
	for _, key := range []string{"completed_at", "completedAt", "complete_time", "completeTime", "paid_at", "paidAt", "pay_time", "updated_at", "updatedAt", "created_at", "createdAt", "created_time", "create_time", "created", "time"} {
		if t, ok := parseOrderTime(row[key]); ok {
			return t, true
		}
	}
	return time.Time{}, false
}

func parseOrderTime(v any) (time.Time, bool) {
	switch vv := v.(type) {
	case float64:
		return unixTime(vv)
	case int:
		return unixTime(float64(vv))
	case int64:
		return unixTime(float64(vv))
	case string:
		s := strings.TrimSpace(vv)
		if s == "" {
			return time.Time{}, false
		}
		if n, err := strconv.ParseFloat(s, 64); err == nil {
			return unixTime(n)
		}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02 15:04:05.000Z", "2006-01-02"} {
			if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
				return t, true
			}
		}
	}
	return time.Time{}, false
}

func unixTime(v float64) (time.Time, bool) {
	if v <= 0 {
		return time.Time{}, false
	}
	if v > 1e12 {
		v /= 1000
	}
	return time.Unix(int64(v), 0), true
}
