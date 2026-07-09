package app

import (
	"context"
	"strings"
	"time"

	"ai-upstream-monitor/internal/domain"
	"ai-upstream-monitor/internal/epay"
	"ai-upstream-monitor/internal/monitor"
)

func (s *Service) SaveRevenueCard(ctx context.Context, id string, in domain.RevenueCard) (domain.RevenueCard, error) {
	if id != "" {
		old, err := s.Store.RevenueCard(ctx, id)
		if err != nil {
			return domain.RevenueCard{}, err
		}
		in.AccessToken = domain.KeepSecret(in.AccessToken, old.AccessToken)
		in.AdminAPIKey = domain.KeepSecret(in.AdminAPIKey, old.AdminAPIKey)
		in.EpayKey = domain.KeepSecret(in.EpayKey, old.EpayKey)
		if strings.TrimSpace(in.BaseURL) == "" {
			in.BaseURL = old.BaseURL
		}
		if strings.TrimSpace(in.UserID) == "" {
			in.UserID = old.UserID
		}
		if strings.TrimSpace(in.EpayPID) == "" {
			in.EpayPID = old.EpayPID
		}
		if strings.TrimSpace(in.UpstreamID) == "" && strings.TrimSpace(in.SourceType) == old.SourceType {
			in.UpstreamID = old.UpstreamID
		}
		in.ID = old.ID
		in.SortOrder = old.SortOrder
		in.CreatedAt = old.CreatedAt
	}
	card, err := s.normalizeRevenueCard(ctx, in)
	if err != nil {
		return domain.RevenueCard{}, err
	}
	if id == "" {
		out, err := s.Store.CreateRevenueCard(ctx, card)
		return out.Public(), err
	}
	out, err := s.Store.UpdateRevenueCard(ctx, card)
	return out.Public(), err
}

func (s *Service) normalizeRevenueCard(ctx context.Context, in domain.RevenueCard) (domain.RevenueCard, error) {
	card := domain.RevenueCard{
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
	}
	switch card.SourceType {
	case "epay_total":
		if card.Name == "" {
			card.Name = "今日收入"
		}
		card.UpstreamID = ""
		if card.BaseURL == "" || card.EpayPID == "" || card.EpayKey == "" {
			cfg, _ := s.Store.Settings(ctx)
			if card.BaseURL == "" {
				card.BaseURL = cfg.EpayBaseURL
			}
			if card.EpayPID == "" {
				card.EpayPID = cfg.EpayPID
			}
			if card.EpayKey == "" {
				card.EpayKey = cfg.EpayKey
			}
		}
		if card.BaseURL == "" || card.EpayPID == "" || card.EpayKey == "" {
			return card, ErrBadRequest("易支付 Base URL、PID、Key 是必填")
		}
	case "newapi_orders", "sub2api_orders":
		want := strings.TrimSuffix(card.SourceType, "_orders")
		if card.UpstreamID != "" && card.BaseURL == "" {
			u, err := s.Store.Upstream(ctx, card.UpstreamID)
			if err != nil {
				return card, err
			}
			if u.Type != want {
				return card, ErrBadRequest("upstream type does not match revenue card")
			}
			if card.Name == "" {
				card.Name = u.Name
			}
		}
		if card.BaseURL == "" && card.UpstreamID == "" {
			return card, ErrBadRequest("Base URL 是必填")
		}
		if want == "newapi" && card.UpstreamID == "" && (card.UserID == "" || card.AccessToken == "") {
			return card, ErrBadRequest("new-api 用户 ID 和 Access Token 是必填")
		}
		if want == "sub2api" && card.AdminAPIKey == "" && card.UpstreamID == "" {
			return card, ErrBadRequest("sub2api 管理员 API Key 是必填")
		}
		if card.Name == "" {
			card.Name = sourceTypeLabel(card.SourceType)
		}
	default:
		return card, ErrBadRequest("source_type must be epay_total, newapi_orders or sub2api_orders")
	}
	return card, nil
}

func (s *Service) ListRevenueCards(ctx context.Context) ([]domain.RevenueCard, error) {
	cards, err := s.listRevenueCardsRaw(ctx)
	if err != nil {
		return nil, err
	}
	return domain.PublicRevenueCards(cards), nil
}

func (s *Service) listRevenueCardsRaw(ctx context.Context) ([]domain.RevenueCard, error) {
	cards, err := s.Store.ListRevenueCards(ctx)
	if err != nil {
		return nil, err
	}
	return s.enrichRevenueCards(ctx, cards), nil
}

func (s *Service) TodayRevenue(ctx context.Context) ([]domain.RevenueRow, error) {
	cards, err := s.Store.ListRevenueCards(ctx)
	if err != nil {
		return nil, err
	}
	cfg, err := s.Store.Settings(ctx)
	if err != nil {
		return nil, err
	}
	cards = s.enrichRevenueCards(ctx, cards)
	out := []domain.RevenueRow{}
	start := todayStart()
	for _, card := range cards {
		row := domain.RevenueRow{RevenueCard: card}
		if !card.Enabled {
			out = append(out, row)
			continue
		}
		row.CheckedAt = time.Now().UTC()
		switch card.SourceType {
		case "epay_total":
			orders, err := (epay.Client{HTTP: s.Client.HTTP}).TodayOrders(ctx, epay.Config{BaseURL: firstNonEmpty(card.BaseURL, cfg.EpayBaseURL), PID: firstNonEmpty(card.EpayPID, cfg.EpayPID), Key: firstNonEmpty(card.EpayKey, cfg.EpayKey)}, start)
			for _, order := range orders {
				row.Revenue += order.Amount
			}
			if err != nil {
				row.Revenue = 0
				row.Error = err.Error()
			}
		case "newapi_orders", "sub2api_orders":
			mu, upstreamID, err := s.revenueMonitorUpstream(ctx, card)
			if err != nil {
				row.Error = err.Error()
				break
			}
			total, err := s.Client.TodayOrderRevenue(ctx, &mu, start)
			if upstreamID != "" {
				_ = s.Store.SaveUpstreamTokens(ctx, upstreamID, mu.Sub2APIAccessToken, mu.Sub2APIRefreshToken)
			}
			row.Revenue, row.CheckedAt = total.Revenue, total.CheckedAt
			if err != nil {
				row.Revenue = 0
				row.Error = err.Error()
			}
		default:
			row.Error = "unsupported revenue card type"
		}
		_ = s.Store.SaveRevenueSnapshot(ctx, domain.RevenueSnapshot{
			SourceID: card.ID, SourceName: card.Name, SourceType: card.SourceType,
			CheckedAt: row.CheckedAt, Revenue: row.Revenue, Error: row.Error,
		})
		out = append(out, row.Public())
	}
	return out, nil
}

func (s *Service) RevenueCardOrders(ctx context.Context, id string) ([]monitor.RevenueOrder, error) {
	card, err := s.Store.RevenueCard(ctx, id)
	if err != nil {
		return nil, err
	}
	if !card.Enabled {
		return []monitor.RevenueOrder{}, nil
	}
	if card.SourceType == "epay_total" {
		cfg, err := s.Store.Settings(ctx)
		if err != nil {
			return nil, err
		}
		orders, err := (epay.Client{HTTP: s.Client.HTTP}).TodayOrders(ctx, epay.Config{BaseURL: firstNonEmpty(card.BaseURL, cfg.EpayBaseURL), PID: firstNonEmpty(card.EpayPID, cfg.EpayPID), Key: firstNonEmpty(card.EpayKey, cfg.EpayKey)}, todayStart())
		out := make([]monitor.RevenueOrder, 0, len(orders))
		for _, order := range orders {
			out = append(out, monitor.RevenueOrder{
				RemoteID:    order.RemoteID,
				Amount:      order.Amount,
				Status:      order.Status,
				PaymentType: order.PaymentType,
				PaidAt:      order.PaidAt,
			})
		}
		return out, err
	}
	mu, upstreamID, err := s.revenueMonitorUpstream(ctx, card)
	if err != nil {
		return nil, err
	}
	orders, err := s.Client.TodayRevenueOrders(ctx, &mu, todayStart())
	if upstreamID != "" {
		_ = s.Store.SaveUpstreamTokens(ctx, upstreamID, mu.Sub2APIAccessToken, mu.Sub2APIRefreshToken)
	}
	return orders, err
}

func (s *Service) SortRevenueCards(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return ErrBadRequest("ids are required")
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			return ErrBadRequest("card id is required")
		}
		if _, ok := seen[id]; ok {
			return ErrBadRequest("duplicate card id")
		}
		seen[id] = struct{}{}
	}
	return s.Store.UpdateRevenueCardOrder(ctx, ids)
}

func (s *Service) enrichRevenueCards(ctx context.Context, cards []domain.RevenueCard) []domain.RevenueCard {
	cfg, _ := s.Store.Settings(ctx)
	for i := range cards {
		if cards[i].SourceType == "epay_total" {
			if cards[i].BaseURL == "" {
				cards[i].BaseURL = cfg.EpayBaseURL
			}
			if cards[i].EpayPID == "" {
				cards[i].EpayPID = cfg.EpayPID
			}
			if cards[i].EpayKey == "" {
				cards[i].EpayKey = cfg.EpayKey
			}
		}
		if cards[i].UpstreamID != "" {
			if u, err := s.Store.Upstream(ctx, cards[i].UpstreamID); err == nil {
				cards[i].UpstreamName = u.Name
			}
		}
	}
	return cards
}

func (s *Service) revenueMonitorUpstream(ctx context.Context, card domain.RevenueCard) (monitor.Upstream, string, error) {
	typ := strings.TrimSuffix(card.SourceType, "_orders")
	if card.UpstreamID != "" && card.BaseURL == "" {
		u, err := s.Store.Upstream(ctx, card.UpstreamID)
		if err != nil {
			return monitor.Upstream{}, "", err
		}
		return toMonitorUpstream(u), u.ID, nil
	}
	if card.BaseURL == "" {
		return monitor.Upstream{}, "", ErrBadRequest("Base URL 是必填")
	}
	return monitor.Upstream{
		Name:        card.Name,
		Type:        typ,
		BaseURL:     card.BaseURL,
		UserID:      card.UserID,
		AccessToken: card.AccessToken,
		AdminAPIKey: card.AdminAPIKey,
	}, "", nil
}

func sourceTypeLabel(sourceType string) string {
	switch sourceType {
	case "newapi_orders":
		return "new-api 订单"
	case "sub2api_orders":
		return "sub2api 订单"
	default:
		return "今日收入"
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func todayStart() time.Time {
	now := time.Now().In(appLocation())
	y, m, d := now.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, now.Location())
}
