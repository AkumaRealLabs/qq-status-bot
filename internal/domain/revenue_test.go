package domain

import "testing"

func TestNormalizeRevenueCardEpay(t *testing.T) {
	got := NormalizeRevenueCard(RevenueCard{
		Name: "  ", SourceType: RevenueEpayTotal, BaseURL: " https://pay/ ",
		UpstreamID: "u1", EpayPID: " p ", EpayKey: " k ",
	})
	if got.Name != "今日收入" || got.UpstreamID != "" || got.BaseURL != "https://pay" || got.EpayPID != "p" {
		t.Fatalf("got=%+v", got)
	}
}

func TestValidateRevenueCard(t *testing.T) {
	if err := ValidateRevenueCard(RevenueCard{SourceType: "bad"}, ""); err == nil {
		t.Fatal("bad source should fail")
	}
	if err := ValidateRevenueCard(RevenueCard{SourceType: RevenueEpayTotal}, ""); err == nil {
		t.Fatal("epay missing fields should fail")
	}
	if err := ValidateRevenueCard(RevenueCard{
		SourceType: RevenueEpayTotal, BaseURL: "https://pay", EpayPID: "1", EpayKey: "k",
	}, ""); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRevenueCard(RevenueCard{
		SourceType: RevenueNewAPIOrders, BaseURL: "https://api", UserID: "1", AccessToken: "t",
	}, ""); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRevenueCard(RevenueCard{
		SourceType: RevenueNewAPIOrders, UpstreamID: "u1",
	}, "sub2api"); err == nil {
		t.Fatal("type mismatch should fail")
	}
	if err := ValidateRevenueCard(RevenueCard{
		SourceType: RevenueSub2APIOrders, BaseURL: "https://api", AdminAPIKey: "k",
	}, ""); err != nil {
		t.Fatal(err)
	}
}

func TestRevenueUpstreamTypeAndDefaults(t *testing.T) {
	if typ, ok := RevenueUpstreamType(RevenueNewAPIOrders); !ok || typ != "newapi" {
		t.Fatalf("typ=%s ok=%v", typ, ok)
	}
	if got := DefaultRevenueName(RevenueSub2APIOrders); got != "sub2api 订单" {
		t.Fatalf("name=%q", got)
	}
	card := ApplyRevenueDefaults(RevenueCard{SourceType: RevenueNewAPIOrders}, "上游A")
	if card.Name != "上游A" {
		t.Fatalf("name=%q", card.Name)
	}
	card = ApplyRevenueDefaults(RevenueCard{SourceType: RevenueNewAPIOrders}, "")
	if card.Name != "new-api 订单" {
		t.Fatalf("name=%q", card.Name)
	}
}
