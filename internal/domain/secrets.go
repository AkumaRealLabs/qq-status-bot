package domain

import "strings"

// KeepSecret：请求密钥为空时保留库中值。
// 空输入表示「不修改」；非空输入替换密钥。
func KeepSecret(in, old string) string {
	in = strings.TrimSpace(in)
	if in == "" {
		return old
	}
	return in
}

func secretSet(v string) bool {
	return strings.TrimSpace(v) != ""
}

func (u Upstream) Public() Upstream {
	out := u
	out.AccessTokenSet = secretSet(u.AccessToken)
	out.PasswordSet = secretSet(u.Password)
	out.Sub2APIAccessTokenSet = secretSet(u.Sub2APIAccessToken)
	out.Sub2APIRefreshTokenSet = secretSet(u.Sub2APIRefreshToken)
	out.AccessToken = ""
	out.Password = ""
	out.Sub2APIAccessToken = ""
	out.Sub2APIRefreshToken = ""
	return out
}

func (k APIKey) Public() APIKey {
	out := k
	out.KeySet = secretSet(k.Key)
	out.Key = ""
	return out
}

func PublicAPIKeys(keys []APIKey) []APIKey {
	out := make([]APIKey, len(keys))
	for i, k := range keys {
		out[i] = k.Public()
	}
	return out
}

func (s Settings) Public() Settings {
	out := s
	out.TelegramBotTokenSet = secretSet(s.TelegramBotToken)
	out.OneBotHTTPTokenSet = secretSet(s.OneBotHTTPToken)
	out.OneBotWebhookTokenSet = secretSet(s.OneBotWebhookToken)
	out.EpayKeySet = secretSet(s.EpayKey)
	out.TelegramBotToken = ""
	out.OneBotHTTPToken = ""
	out.OneBotWebhookToken = ""
	out.EpayKey = ""
	return out
}

func (c SchedulerConfig) Public() SchedulerConfig {
	out := c
	out.AccessTokenSet = secretSet(c.AccessToken)
	out.AccessToken = ""
	return out
}

func (c RevenueCard) Public() RevenueCard {
	out := c
	out.AccessTokenSet = secretSet(c.AccessToken)
	out.AdminAPIKeySet = secretSet(c.AdminAPIKey)
	out.EpayKeySet = secretSet(c.EpayKey)
	out.AccessToken = ""
	out.AdminAPIKey = ""
	out.EpayKey = ""
	return out
}

func PublicRevenueCards(cards []RevenueCard) []RevenueCard {
	out := make([]RevenueCard, len(cards))
	for i, c := range cards {
		out[i] = c.Public()
	}
	return out
}

func (r RevenueRow) Public() RevenueRow {
	out := r
	out.RevenueCard = r.RevenueCard.Public()
	return out
}

func PublicRevenueRows(rows []RevenueRow) []RevenueRow {
	out := make([]RevenueRow, len(rows))
	for i, r := range rows {
		out[i] = r.Public()
	}
	return out
}
