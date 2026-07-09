package domain

import (
	"errors"
	"strings"
)

// Revenue card source types.
const (
	RevenueEpayTotal     = "epay_total"
	RevenueNewAPIOrders  = "newapi_orders"
	RevenueSub2APIOrders = "sub2api_orders"
)

// NormalizeRevenueCard trims fields and applies type-specific field clears.
// Does not fill credentials from global settings (that is app orchestration).
func NormalizeRevenueCard(in RevenueCard) RevenueCard {
	out := RevenueCard{
		ID:          in.ID,
		Name:        strings.TrimSpace(in.Name),
		SourceType:  strings.TrimSpace(in.SourceType),
		BaseURL:     strings.TrimRight(strings.TrimSpace(in.BaseURL), "/"),
		UserID:      strings.TrimSpace(in.UserID),
		AccessToken: strings.TrimSpace(in.AccessToken),
		AdminAPIKey: strings.TrimSpace(in.AdminAPIKey),
		EpayPID:     strings.TrimSpace(in.EpayPID),
		EpayKey:     strings.TrimSpace(in.EpayKey),
		UpstreamID:  strings.TrimSpace(in.UpstreamID),
		Enabled:     in.Enabled,
		SortOrder:   in.SortOrder,
		CreatedAt:   in.CreatedAt,
		UpdatedAt:   in.UpdatedAt,
	}
	if out.SourceType == RevenueEpayTotal {
		out.UpstreamID = ""
		if out.Name == "" {
			out.Name = DefaultRevenueName(out.SourceType)
		}
	}
	return out
}

// DefaultRevenueName returns a display name for a source type.
func DefaultRevenueName(sourceType string) string {
	switch strings.TrimSpace(sourceType) {
	case RevenueNewAPIOrders:
		return "new-api 订单"
	case RevenueSub2APIOrders:
		return "sub2api 订单"
	default:
		return "今日收入"
	}
}

// RevenueUpstreamType maps *_orders source types to upstream type names.
func RevenueUpstreamType(sourceType string) (string, bool) {
	switch strings.TrimSpace(sourceType) {
	case RevenueNewAPIOrders:
		return "newapi", true
	case RevenueSub2APIOrders:
		return "sub2api", true
	default:
		return "", false
	}
}

// ValidateRevenueCard checks required fields after settings fill-in and optional
// upstream binding. upstreamType is only used when UpstreamID is set for order cards.
func ValidateRevenueCard(card RevenueCard, upstreamType string) error {
	card = NormalizeRevenueCard(card)
	switch card.SourceType {
	case RevenueEpayTotal:
		if card.BaseURL == "" || card.EpayPID == "" || card.EpayKey == "" {
			return errors.New("易支付 Base URL、PID、Key 是必填")
		}
	case RevenueNewAPIOrders, RevenueSub2APIOrders:
		want, _ := RevenueUpstreamType(card.SourceType)
		if card.UpstreamID != "" && upstreamType != "" && upstreamType != want {
			return errors.New("upstream type does not match revenue card")
		}
		if card.BaseURL == "" && card.UpstreamID == "" {
			return errors.New("Base URL 是必填")
		}
		if want == "newapi" && card.UpstreamID == "" && (card.UserID == "" || card.AccessToken == "") {
			return errors.New("new-api 用户 ID 和 Access Token 是必填")
		}
		if want == "sub2api" && card.AdminAPIKey == "" && card.UpstreamID == "" {
			return errors.New("sub2api 管理员 API Key 是必填")
		}
	default:
		return errors.New("source_type must be epay_total, newapi_orders or sub2api_orders")
	}
	return nil
}

// ApplyRevenueDefaults sets empty Name from upstream name or source-type label.
func ApplyRevenueDefaults(card RevenueCard, upstreamName string) RevenueCard {
	card = NormalizeRevenueCard(card)
	if card.Name != "" {
		return card
	}
	if name := strings.TrimSpace(upstreamName); name != "" {
		card.Name = name
		return card
	}
	card.Name = DefaultRevenueName(card.SourceType)
	return card
}
