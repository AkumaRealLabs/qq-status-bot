package monitor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewapiRechargeCapabilitiesAndOrders(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/user/topup/info":
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "success", "data": map[string]any{
				"enable_redemption":            true,
				"enable_online_topup":          true,
				"enable_stripe_topup":          true,
				"enable_creem_topup":           true,
				"enable_muyin_topup":           true,
				"pay_methods":                  []map[string]any{{"type": "alipay", "name": "支付宝", "min_topup": "10"}},
				"creem_products":               `[{"productId":"prod_1","name":"套餐 A","price":8.8}]`,
				"payment_compliance_confirmed": true,
			}})
		case "/api/user/stripe/pay":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["payment_method"] != "stripe" {
				t.Fatalf("body = %#v", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "success", "data": map[string]any{"pay_link": "https://pay.example/stripe", "order_id": "ord_1"}})
		case "/api/user/creem/pay":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["product_id"] != "prod_1" {
				t.Fatalf("body = %#v", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "success", "data": map[string]any{"checkout_url": "https://pay.example/creem", "order_id": "ord_2"}})
		case "/api/user/muyin/pay":
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "success", "data": map[string]any{"qr_code_content": "weixin://qr", "order_id": "ord_3"}})
		case "/api/user/pay":
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "success", "url": "https://pay.example/form", "data": map[string]any{"pid": "1", "sign": "abc"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	c := Client{HTTP: ts.Client()}
	u := &Upstream{Type: "newapi", BaseURL: ts.URL}
	caps, err := c.RechargeCapabilities(t.Context(), u)
	if err != nil {
		t.Fatal(err)
	}
	if !caps.RedeemEnabled || !caps.OnlineEnabled || len(caps.Methods) != 4 {
		t.Fatalf("caps = %+v", caps)
	}
	if got := caps.Methods[1].Type; got != "stripe" {
		t.Fatalf("second method = %q", got)
	}
	stripe, err := c.CreateRechargeOrder(t.Context(), u, RechargeOrderRequest{Amount: 20, PaymentType: "stripe"})
	if err != nil {
		t.Fatal(err)
	}
	if stripe.ResultType != "link" || stripe.URL == "" || stripe.RemoteOrderID != "ord_1" {
		t.Fatalf("stripe = %+v", stripe)
	}
	creem, err := c.CreateRechargeOrder(t.Context(), u, RechargeOrderRequest{PaymentType: "creem:prod_1"})
	if err != nil {
		t.Fatal(err)
	}
	if creem.URL == "" || creem.PaymentType != "creem" {
		t.Fatalf("creem = %+v", creem)
	}
	muyin, err := c.CreateRechargeOrder(t.Context(), u, RechargeOrderRequest{Amount: 20, PaymentType: "muyin"})
	if err != nil {
		t.Fatal(err)
	}
	if muyin.ResultType != "qr" || !strings.Contains(muyin.QRCode, "weixin") {
		t.Fatalf("muyin = %+v", muyin)
	}
	epay, err := c.CreateRechargeOrder(t.Context(), u, RechargeOrderRequest{Amount: 20, PaymentType: "alipay"})
	if err != nil {
		t.Fatal(err)
	}
	if epay.ResultType != "order" || epay.URL != "" || !strings.Contains(epay.Message, "上游站点") {
		t.Fatalf("epay = %+v", epay)
	}
}

func TestRechargeOrderChecksMinimumBeforeCreate(t *testing.T) {
	var created bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/user/topup/info":
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "success", "data": map[string]any{
				"enable_online_topup": true,
				"pay_methods":         []map[string]any{{"type": "alipay", "name": "支付宝", "min_topup": "1"}},
			}})
		case "/api/user/pay":
			created = true
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "success", "data": map[string]any{"order_id": "ord_1"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	c := Client{HTTP: ts.Client()}
	_, err := c.CreateRechargeOrder(t.Context(), &Upstream{Type: "newapi", BaseURL: ts.URL}, RechargeOrderRequest{Amount: 0.1, PaymentType: "alipay"})
	if err == nil || !strings.Contains(err.Error(), "不能低于 1.00") {
		t.Fatalf("err = %v", err)
	}
	if created {
		t.Fatal("order should not be created below minimum")
	}
}

func TestRechargeCapabilitiesHideUnavailableMethods(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/newapi/api/user/topup/info":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"enable_redemption":            true,
				"enable_online_topup":          false,
				"payment_compliance_confirmed": false,
				"topup_link":                   "https://example.com/redeem",
			}})
		case "/sub2api/api/v1/payment/config":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"enabled": true}})
		case "/sub2api/api/v1/payment/checkout-info":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"methods": map[string]any{
				"alipay_direct": map[string]any{"available": true},
				"easypay":       map[string]any{"available": true},
				"stripe":        map[string]any{"available": true},
			}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	c := Client{HTTP: ts.Client()}
	newapiCaps, err := c.RechargeCapabilities(t.Context(), &Upstream{Type: "newapi", BaseURL: ts.URL + "/newapi"})
	if err != nil {
		t.Fatal(err)
	}
	if newapiCaps.OnlineEnabled || newapiCaps.RedeemEnabled || newapiCaps.ExternalURL != "https://example.com/redeem" {
		t.Fatalf("newapi caps = %+v", newapiCaps)
	}

	sub2Caps, err := c.RechargeCapabilities(t.Context(), &Upstream{Type: "sub2api", BaseURL: ts.URL + "/sub2api", Sub2APIAccessToken: "token"})
	if err != nil {
		t.Fatal(err)
	}
	if len(sub2Caps.Methods) != 2 || sub2Caps.Methods[0].Type != "alipay" || sub2Caps.Methods[1].Type != "stripe" {
		t.Fatalf("sub2api caps = %+v", sub2Caps)
	}
	if _, err := c.CreateRechargeOrder(t.Context(), &Upstream{Type: "sub2api", BaseURL: ts.URL + "/sub2api", Sub2APIAccessToken: "token"}, RechargeOrderRequest{Amount: 10, PaymentType: "easypay"}); err == nil {
		t.Fatal("expected unsupported payment_type error")
	}
}

func TestSub2apiRechargeRefreshRetry(t *testing.T) {
	var token string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/refresh":
			token = "new-token"
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"access_token": token, "refresh_token": "new-refresh"}})
		case "/api/v1/payment/config":
			if r.Header.Get("Authorization") != "Bearer new-token" {
				http.Error(w, "expired", http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"enabled": true, "balance_disabled": false}})
		case "/api/v1/payment/checkout-info":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"methods": map[string]any{"alipay_direct": map[string]any{"available": true, "single_min": 1}}}})
		case "/api/v1/payment/orders":
			if r.Header.Get("Authorization") != "Bearer new-token" {
				http.Error(w, "expired", http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"order_id": 7, "out_trade_no": "SUB2-7", "qr_code": "qr-content"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	c := Client{HTTP: ts.Client()}
	u := &Upstream{Type: "sub2api", BaseURL: ts.URL, Sub2APIAccessToken: "old-token", Sub2APIRefreshToken: "refresh"}
	caps, err := c.RechargeCapabilities(t.Context(), u)
	if err != nil {
		t.Fatal(err)
	}
	if u.Sub2APIAccessToken != "new-token" || !caps.OnlineEnabled || caps.Methods[0].Type != "alipay" {
		t.Fatalf("token=%q caps=%+v", u.Sub2APIAccessToken, caps)
	}
	order, err := c.CreateRechargeOrder(t.Context(), u, RechargeOrderRequest{Amount: 10, PaymentType: "alipay"})
	if err != nil {
		t.Fatal(err)
	}
	if order.ResultType != "qr" || order.RemoteOrderID != "SUB2-7" {
		t.Fatalf("order = %+v", order)
	}
}

func TestRefreshRechargeOrderStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/newapi/api/user/topup/self":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"items": []map[string]any{{
				"trade_no": "USR1NO1", "payment_method": "alipay", "status": "success",
			}}}})
		case "/sub2api/api/v1/payment/orders/verify":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["out_trade_no"] != "SUB2-1" {
				t.Fatalf("body = %#v", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"out_trade_no": "SUB2-1", "payment_type": "alipay", "status": "COMPLETED"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	c := Client{HTTP: ts.Client()}
	newapi, err := c.RefreshRechargeOrder(t.Context(), &Upstream{Type: "newapi", BaseURL: ts.URL + "/newapi"}, "USR1NO1")
	if err != nil {
		t.Fatal(err)
	}
	if newapi.Status != "success" || newapi.RemoteOrderID != "USR1NO1" {
		t.Fatalf("newapi = %+v", newapi)
	}
	sub2, err := c.RefreshRechargeOrder(t.Context(), &Upstream{Type: "sub2api", BaseURL: ts.URL + "/sub2api", Sub2APIAccessToken: "token"}, "SUB2-1")
	if err != nil {
		t.Fatal(err)
	}
	if sub2.Status != "COMPLETED" || sub2.RemoteOrderID != "SUB2-1" {
		t.Fatalf("sub2 = %+v", sub2)
	}
}
