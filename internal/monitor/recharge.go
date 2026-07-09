package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func (c Client) RechargeCapabilities(ctx context.Context, u *Upstream) (RechargeCapabilities, error) {
	switch u.Type {
	case "newapi":
		return c.newapiRechargeCapabilities(ctx, u)
	case "sub2api":
		if err := c.sub2apiAuth(ctx, u); err != nil {
			return RechargeCapabilities{}, err
		}
		out, err := c.sub2apiRechargeCapabilities(ctx, u)
		if IsAuthError(err) {
			if err = c.sub2apiForceAuth(ctx, u); err != nil {
				return out, err
			}
			out, err = c.sub2apiRechargeCapabilities(ctx, u)
		}
		return out, err
	default:
		return RechargeCapabilities{}, fmt.Errorf("unsupported upstream type %q", u.Type)
	}
}

func (c Client) Redeem(ctx context.Context, u *Upstream, code string) (RechargeOrderResult, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return RechargeOrderResult{}, errors.New("redeem code is required")
	}
	switch u.Type {
	case "newapi":
		return c.newapiRedeem(ctx, u, code)
	case "sub2api":
		if err := c.sub2apiAuth(ctx, u); err != nil {
			return RechargeOrderResult{}, err
		}
		out, err := c.sub2apiRedeem(ctx, u, code)
		if IsAuthError(err) {
			if err = c.sub2apiForceAuth(ctx, u); err != nil {
				return out, err
			}
			out, err = c.sub2apiRedeem(ctx, u, code)
		}
		return out, err
	default:
		return RechargeOrderResult{}, fmt.Errorf("unsupported upstream type %q", u.Type)
	}
}

func (c Client) CreateRechargeOrder(ctx context.Context, u *Upstream, req RechargeOrderRequest) (RechargeOrderResult, error) {
	req.PaymentType = strings.TrimSpace(req.PaymentType)
	if req.PaymentType == "" {
		return RechargeOrderResult{}, errors.New("payment_type is required")
	}
	if err := c.validateRechargeAmount(ctx, u, req); err != nil {
		return RechargeOrderResult{}, err
	}
	switch u.Type {
	case "newapi":
		return c.newapiRechargeOrder(ctx, u, req)
	case "sub2api":
		if err := c.sub2apiAuth(ctx, u); err != nil {
			return RechargeOrderResult{}, err
		}
		out, err := c.sub2apiRechargeOrder(ctx, u, req)
		if IsAuthError(err) {
			if err = c.sub2apiForceAuth(ctx, u); err != nil {
				return out, err
			}
			out, err = c.sub2apiRechargeOrder(ctx, u, req)
		}
		return out, err
	default:
		return RechargeOrderResult{}, fmt.Errorf("unsupported upstream type %q", u.Type)
	}
}

func (c Client) RefreshRechargeOrder(ctx context.Context, u *Upstream, orderID string) (RechargeOrderResult, error) {
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return RechargeOrderResult{}, errors.New("order id is required")
	}
	switch u.Type {
	case "newapi":
		return c.newapiRechargeOrderStatus(ctx, u, orderID)
	case "sub2api":
		if err := c.sub2apiAuth(ctx, u); err != nil {
			return RechargeOrderResult{}, err
		}
		out, err := c.sub2apiRechargeOrderStatus(ctx, u, orderID)
		if IsAuthError(err) {
			if err = c.sub2apiForceAuth(ctx, u); err != nil {
				return out, err
			}
			out, err = c.sub2apiRechargeOrderStatus(ctx, u, orderID)
		}
		return out, err
	default:
		return RechargeOrderResult{}, fmt.Errorf("unsupported upstream type %q", u.Type)
	}
}

func (c Client) validateRechargeAmount(ctx context.Context, u *Upstream, req RechargeOrderRequest) error {
	if strings.HasPrefix(req.PaymentType, "creem:") {
		return nil
	}
	if req.Amount <= 0 {
		return errors.New("amount is required")
	}
	caps, err := c.RechargeCapabilities(ctx, u)
	if err != nil {
		return err
	}
	for _, method := range caps.Methods {
		if method.Type != req.PaymentType {
			continue
		}
		if method.MinAmount > 0 && req.Amount < method.MinAmount {
			return fmt.Errorf("充值金额不能低于 %.2f", method.MinAmount)
		}
		if method.MaxAmount > 0 && req.Amount > method.MaxAmount {
			return fmt.Errorf("充值金额不能高于 %.2f", method.MaxAmount)
		}
		return nil
	}
	return nil
}

func (c Client) newapiRechargeCapabilities(ctx context.Context, u *Upstream) (RechargeCapabilities, error) {
	var raw map[string]any
	if err := c.doJSON(ctx, http.MethodGet, joinURL(u.BaseURL, "/api/user/topup/info"), nil, newapiHeaders(u), &raw); err != nil {
		return RechargeCapabilities{}, markAuth(err)
	}
	data := payload(raw)
	out := RechargeCapabilities{
		OnlineEnabled: anyTruthy(data, "enable_online_topup", "enable_stripe_topup", "enable_creem_topup", "enable_waffo_topup", "enable_waffo_pancake_topup", "enable_muyin_topup"),
		RedeemEnabled: truthy(data["enable_redemption"]) && data["payment_compliance_confirmed"] != false,
		ExternalURL:   str(first(data, "topup_link", "topup_url", "recharge_url")),
	}
	methods := map[string]RechargeMethod{}
	for _, item := range array(data["pay_methods"]) {
		m := obj(item)
		t := normalizeNewapiPaymentType(str(first(m, "type", "payment_method")))
		if t == "" || t == "creem" {
			continue
		}
		methods[t] = RechargeMethod{Type: t, Name: nonEmpty(str(first(m, "name", "label")), paymentName(t)), MinAmount: num(first(m, "min_topup", "min_amount")), Extra: m, Direct: true}
	}
	addFlagMethod(methods, data, "enable_stripe_topup", "stripe", "Stripe", "stripe_min_topup")
	if truthy(data["enable_creem_topup"]) {
		for _, product := range parseMaybeArray(data["creem_products"]) {
			m := obj(product)
			productID := str(first(m, "product_id", "productId", "id"))
			if productID == "" {
				continue
			}
			methods["creem:"+productID] = RechargeMethod{
				Type: "creem:" + productID, Name: nonEmpty(str(first(m, "name")), "Creem"), MinAmount: num(first(m, "price")), Direct: true,
				Extra: map[string]any{"provider": "creem", "product_id": productID, "product": m},
			}
		}
	}
	addFlagMethod(methods, data, "enable_waffo_topup", "waffo", "Waffo", "waffo_min_topup")
	addFlagMethod(methods, data, "enable_waffo_pancake_topup", "waffo-pancake", "Waffo Pancake", "waffo_pancake_min_topup")
	addFlagMethod(methods, data, "enable_muyin_topup", "muyin", "Muyin", "muyin_min_topup")
	if truthy(data["enable_online_topup"]) && len(methods) == 0 {
		methods["epay"] = RechargeMethod{Type: "epay", Name: "在线支付", MinAmount: num(data["min_topup"]), Direct: true}
	}
	out.Methods = sortedMethods(methods)
	return out, nil
}

func addFlagMethod(methods map[string]RechargeMethod, data map[string]any, flag, typ, name, minKey string) {
	if !truthy(data[flag]) {
		return
	}
	if m, ok := methods[typ]; ok {
		if m.MinAmount == 0 {
			m.MinAmount = num(data[minKey])
			methods[typ] = m
		}
		return
	}
	methods[typ] = RechargeMethod{Type: typ, Name: name, MinAmount: num(data[minKey]), Direct: true}
}

func (c Client) newapiRedeem(ctx context.Context, u *Upstream, code string) (RechargeOrderResult, error) {
	var raw map[string]any
	if err := c.doJSON(ctx, http.MethodPost, joinURL(u.BaseURL, "/api/user/topup"), map[string]string{"key": code}, newapiHeaders(u), &raw); err != nil {
		return RechargeOrderResult{}, markAuth(err)
	}
	if err := apiPayloadError(raw); err != nil {
		return RechargeOrderResult{ResultType: "redeem", Raw: raw}, err
	}
	return RechargeOrderResult{ResultType: "redeem", Message: "兑换成功", Raw: payload(raw)}, nil
}

func (c Client) newapiRechargeOrder(ctx context.Context, u *Upstream, req RechargeOrderRequest) (RechargeOrderResult, error) {
	if req.Amount <= 0 && !strings.HasPrefix(req.PaymentType, "creem:") {
		return RechargeOrderResult{}, errors.New("amount is required")
	}
	paymentType := normalizeNewapiPaymentType(req.PaymentType)
	body := map[string]any{"amount": int64(req.Amount), "payment_method": paymentType}
	path := "/api/user/pay"
	switch {
	case strings.HasPrefix(req.PaymentType, "creem:"):
		path = "/api/user/creem/pay"
		paymentType = "creem"
		body = map[string]any{"payment_method": "creem", "product_id": strings.TrimPrefix(req.PaymentType, "creem:")}
	case paymentType == "stripe":
		path = "/api/user/stripe/pay"
	case paymentType == "waffo":
		path = "/api/user/waffo/pay"
	case paymentType == "waffo-pancake":
		path = "/api/user/waffo-pancake/pay"
		body["payment_method"] = "waffo_pancake"
	case paymentType == "muyin":
		path = "/api/user/muyin/pay"
	default:
		body["payment_method"] = req.PaymentType
	}
	var raw map[string]any
	if err := c.doJSON(ctx, http.MethodPost, joinURL(u.BaseURL, path), body, newapiHeaders(u), &raw); err != nil {
		return RechargeOrderResult{}, markAuth(err)
	}
	if err := apiPayloadError(raw); err != nil {
		return RechargeOrderResult{ResultType: "order", PaymentType: paymentType, Raw: raw}, err
	}
	out := orderResult(paymentType, paymentPayload(raw))
	out.PaymentType = paymentType
	out.RemoteOrderID = nonEmpty(str(first(out.Raw, "out_trade_no")), out.RemoteOrderID)
	return out, nil
}

func (c Client) newapiRechargeOrderStatus(ctx context.Context, u *Upstream, orderID string) (RechargeOrderResult, error) {
	var raw map[string]any
	path := "/api/user/topup/self?p=1&page_size=20&keyword=" + url.QueryEscape(orderID)
	if err := c.doJSON(ctx, http.MethodGet, joinURL(u.BaseURL, path), nil, newapiHeaders(u), &raw); err != nil {
		return RechargeOrderResult{}, markAuth(err)
	}
	if err := apiPayloadError(raw); err != nil {
		return RechargeOrderResult{ResultType: "order", RemoteOrderID: orderID, Raw: raw}, err
	}
	item := findNewapiTopup(payload(raw), orderID)
	if len(item) == 0 {
		return RechargeOrderResult{}, errors.New("order not found")
	}
	return RechargeOrderResult{
		ResultType:    "order",
		PaymentType:   str(first(item, "payment_method", "payment_provider")),
		RemoteOrderID: str(first(item, "trade_no", "order_id", "id")),
		Status:        str(first(item, "status")),
		Message:       str(first(item, "status")),
		Raw:           item,
	}, nil
}

func (c Client) sub2apiRechargeCapabilities(ctx context.Context, u *Upstream) (RechargeCapabilities, error) {
	var cfgRaw, checkoutRaw map[string]any
	if err := c.doJSON(ctx, http.MethodGet, joinURL(u.BaseURL, "/api/v1/payment/config"), nil, bearer(u.Sub2APIAccessToken), &cfgRaw); err != nil {
		return RechargeCapabilities{}, markAuth(err)
	}
	if err := c.doJSON(ctx, http.MethodGet, joinURL(u.BaseURL, "/api/v1/payment/checkout-info"), nil, bearer(u.Sub2APIAccessToken), &checkoutRaw); err != nil {
		return RechargeCapabilities{}, markAuth(err)
	}
	cfg, checkout := payload(cfgRaw), payload(checkoutRaw)
	enabled := truthy(first(cfg, "enabled", "payment_enabled")) && !truthy(first(cfg, "balance_disabled")) && !truthy(first(checkout, "balance_disabled"))
	methods := map[string]RechargeMethod{}
	if enabled {
		for typ, item := range obj(checkout["methods"]) {
			t := normalizeSub2PaymentType(typ)
			if t == "" {
				continue
			}
			m := obj(item)
			if v, ok := m["available"]; ok && !truthy(v) {
				continue
			}
			methods[t] = RechargeMethod{Type: t, Name: paymentName(t), MinAmount: num(first(m, "single_min", "min")), MaxAmount: num(first(m, "single_max", "max")), Extra: m, Direct: t == "alipay" || t == "wxpay"}
		}
	}
	return RechargeCapabilities{OnlineEnabled: enabled && len(methods) > 0, RedeemEnabled: true, Methods: sortedMethods(methods)}, nil
}

func (c Client) sub2apiRedeem(ctx context.Context, u *Upstream, code string) (RechargeOrderResult, error) {
	var raw map[string]any
	if err := c.doJSON(ctx, http.MethodPost, joinURL(u.BaseURL, "/api/v1/redeem"), map[string]string{"code": code}, bearer(u.Sub2APIAccessToken), &raw); err != nil {
		return RechargeOrderResult{}, markAuth(err)
	}
	if err := apiPayloadError(raw); err != nil {
		return RechargeOrderResult{ResultType: "redeem", Raw: raw}, err
	}
	return RechargeOrderResult{ResultType: "redeem", Message: "兑换成功", Raw: payload(raw)}, nil
}

func (c Client) sub2apiRechargeOrder(ctx context.Context, u *Upstream, req RechargeOrderRequest) (RechargeOrderResult, error) {
	if req.Amount <= 0 {
		return RechargeOrderResult{}, errors.New("amount is required")
	}
	paymentType := normalizeSub2PaymentType(req.PaymentType)
	if paymentType == "" {
		return RechargeOrderResult{}, fmt.Errorf("unsupported payment_type %q", req.PaymentType)
	}
	var raw map[string]any
	err := c.doJSON(ctx, http.MethodPost, joinURL(u.BaseURL, "/api/v1/payment/orders"), map[string]any{
		"amount": req.Amount, "payment_type": paymentType, "order_type": "balance",
	}, bearer(u.Sub2APIAccessToken), &raw)
	if err != nil {
		return RechargeOrderResult{}, markAuth(err)
	}
	if err := apiPayloadError(raw); err != nil {
		return RechargeOrderResult{ResultType: "order", PaymentType: paymentType, Raw: raw}, err
	}
	out := orderResult(paymentType, paymentPayload(raw))
	out.PaymentType = paymentType
	out.RemoteOrderID = nonEmpty(str(first(out.Raw, "out_trade_no")), out.RemoteOrderID)
	return out, nil
}

func (c Client) sub2apiRechargeOrderStatus(ctx context.Context, u *Upstream, orderID string) (RechargeOrderResult, error) {
	var raw map[string]any
	err := c.doJSON(ctx, http.MethodPost, joinURL(u.BaseURL, "/api/v1/payment/orders/verify"), map[string]string{"out_trade_no": orderID}, bearer(u.Sub2APIAccessToken), &raw)
	if err != nil {
		return RechargeOrderResult{}, markAuth(err)
	}
	if err := apiPayloadError(raw); err != nil {
		return RechargeOrderResult{ResultType: "order", RemoteOrderID: orderID, Raw: raw}, err
	}
	data := paymentPayload(raw)
	out := orderResult(str(first(data, "payment_type")), data)
	out.RemoteOrderID = nonEmpty(str(first(data, "out_trade_no", "order_id", "id")), orderID)
	out.Status = str(first(data, "status"))
	out.Message = out.Status
	return out, nil
}

func orderResult(paymentType string, data map[string]any) RechargeOrderResult {
	out := RechargeOrderResult{
		ResultType:    nonEmpty(str(first(data, "result_type")), "order"),
		PaymentType:   paymentType,
		RemoteOrderID: str(first(data, "order_id", "id", "out_trade_no", "trade_no", "session_id")),
		Status:        str(first(data, "status")),
		Raw:           data,
	}
	out.URL = str(first(data, "pay_url", "checkout_url", "payment_url", "paymentUrl", "pay_link", "qr_code_url", "qrCodeUrl"))
	out.QRCode = str(first(data, "qr_code", "qr_code_content", "qrCodeContent"))
	if out.QRCode == "" {
		out.QRCode = str(first(obj(first(data, "qrCode", "qr_code")), "qrCodeContent", "content", "url"))
	}
	if out.URL != "" {
		out.ResultType = "link"
	} else if out.QRCode != "" {
		out.ResultType = "qr"
	} else if data["client_secret"] != nil || data["oauth"] != nil || data["jsapi"] != nil || data["jsapi_payload"] != nil {
		out.ResultType = "sdk"
		out.Message = "该支付方式需要在上游站点完成"
	}
	if out.Message == "" {
		out.Message = str(first(data, "message", "status"))
	}
	return out
}

func apiPayloadError(raw map[string]any) error {
	if code := num(raw["code"]); code != 0 {
		return errors.New(nonEmpty(str(first(raw, "message", "reason", "error")), "upstream request failed"))
	}
	msg := strings.TrimSpace(str(raw["message"]))
	if msg != "" && msg != "success" {
		return errors.New(nonEmpty(str(raw["data"]), msg))
	}
	if raw["success"] == false {
		return errors.New(nonEmpty(str(raw["error"]), "upstream request failed"))
	}
	if e := strings.TrimSpace(str(first(raw, "error", "detail"))); e != "" {
		return errors.New(e)
	}
	return nil
}

func payload(raw map[string]any) map[string]any {
	data := obj(raw["data"])
	if len(data) != 0 {
		return data
	}
	return raw
}

func paymentPayload(raw map[string]any) map[string]any {
	data := payload(raw)
	if rawURL := strings.TrimSpace(str(raw["url"])); rawURL != "" && str(first(data, "pay_url", "checkout_url", "payment_url", "paymentUrl", "pay_link", "qr_code_url", "qrCodeUrl")) == "" {
		data = cloneMap(data)
		if len(data) == 0 {
			data["payment_url"] = rawURL
		} else {
			// new-api 易支付返回 form action + POST 参数。运维台 v1 不内嵌表单支付。
			data["message"] = "该支付方式需要在上游站点完成"
		}
	}
	return data
}

func findNewapiTopup(data map[string]any, orderID string) map[string]any {
	for _, parent := range []string{"items", "records", "list", "topups"} {
		for _, key := range []string{"data", "rows", "result"} {
			for _, item := range array(obj(data[key])[parent]) {
				row := obj(item)
				if str(first(row, "trade_no", "order_id", "id")) == orderID {
					return row
				}
			}
		}
	}
	for _, key := range []string{"items", "records", "list", "topups"} {
		for _, item := range array(data[key]) {
			row := obj(item)
			if str(first(row, "trade_no", "order_id", "id")) == orderID {
				return row
			}
		}
	}
	for _, item := range array(data["data"]) {
		row := obj(item)
		if str(first(row, "trade_no", "order_id", "id")) == orderID {
			return row
		}
	}
	if str(first(data, "trade_no", "order_id", "id")) == orderID {
		return data
	}
	return nil
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in)+1)
	for k, v := range in {
		out[k] = v
	}
	return out
}

func parseMaybeArray(v any) []any {
	if s := strings.TrimSpace(str(v)); strings.HasPrefix(s, "[") {
		var out []any
		if json.Unmarshal([]byte(s), &out) == nil {
			return out
		}
	}
	return array(v)
}

func sortedMethods(methods map[string]RechargeMethod) []RechargeMethod {
	order := []string{"alipay", "wxpay", "stripe", "airwallex", "epay", "waffo", "waffo-pancake", "muyin"}
	out := []RechargeMethod{}
	seen := map[string]bool{}
	for _, key := range order {
		if m, ok := methods[key]; ok {
			out = append(out, m)
			seen[key] = true
		}
	}
	for key, m := range methods {
		if !seen[key] {
			out = append(out, m)
		}
	}
	return out
}

func normalizeNewapiPaymentType(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "waffo_pancake":
		return "waffo-pancake"
	default:
		return v
	}
}

func normalizeSub2PaymentType(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "alipay_direct":
		return "alipay"
	case "wxpay_direct":
		return "wxpay"
	case "alipay", "wxpay", "stripe", "airwallex":
		return v
	default:
		return ""
	}
}

func paymentName(v string) string {
	switch strings.TrimPrefix(v, "creem:") {
	case "alipay":
		return "支付宝"
	case "wxpay":
		return "微信支付"
	case "stripe":
		return "Stripe"
	case "airwallex":
		return "Airwallex"
	case "waffo":
		return "Waffo"
	case "waffo-pancake":
		return "Waffo Pancake"
	case "muyin":
		return "Muyin"
	case "epay":
		return "在线支付"
	case "creem":
		return "Creem"
	default:
		return v
	}
}

func nonEmpty(v, fallback string) string {
	if strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return fallback
}

func truthy(v any) bool {
	switch vv := v.(type) {
	case bool:
		return vv
	case string:
		b, _ := strconv.ParseBool(strings.TrimSpace(vv))
		return b || strings.EqualFold(vv, "1") || strings.EqualFold(vv, "yes")
	case float64:
		return vv != 0
	case int:
		return vv != 0
	default:
		return false
	}
}

func anyTruthy(m map[string]any, keys ...string) bool {
	for _, key := range keys {
		if truthy(m[key]) {
			return true
		}
	}
	return false
}
