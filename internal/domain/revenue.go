package domain

import (
	"errors"
	"strings"
)

// 收入卡片来源类型。
const (
	RevenueEpayTotal     = "epay_total"
	RevenueNewAPIOrders  = "newapi_orders"
	RevenueSub2APIOrders = "sub2api_orders"
)

// NormalizeRevenueCard 裁剪字段并按类型清空无关字段。
// 不从全局设置回填凭证（那是 app 编排职责）。
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

// DefaultRevenueName 返回某来源类型的默认展示名。
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

// RevenueUpstreamType 将 *_orders 来源类型映射为上游类型名。
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

// ValidateRevenueCard 在设置回填与可选上游绑定之后校验必填字段。
// upstreamType 仅在订单类卡片设置了 UpstreamID 时使用。
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

// ApplyRevenueDefaults：Name 为空时用上游名或来源类型标签填充。
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
