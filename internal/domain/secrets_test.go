package domain

import "testing"

func TestKeepSecret(t *testing.T) {
	if got := KeepSecret("", "stored"); got != "stored" {
		t.Fatalf("empty should keep old, got %q", got)
	}
	if got := KeepSecret("  new  ", "stored"); got != "new" {
		t.Fatalf("non-empty should replace, got %q", got)
	}
}

func TestPublicRedactsSecrets(t *testing.T) {
	u := Upstream{
		Name: "u", AccessToken: "tok", Password: "pw",
		Sub2APIAccessToken: "a", Sub2APIRefreshToken: "r",
	}.Public()
	if u.AccessToken != "" || u.Password != "" || u.Sub2APIAccessToken != "" || u.Sub2APIRefreshToken != "" {
		t.Fatalf("secrets leaked: %+v", u)
	}
	if !u.AccessTokenSet || !u.PasswordSet || !u.Sub2APIAccessTokenSet || !u.Sub2APIRefreshTokenSet {
		t.Fatalf("set flags missing: %+v", u)
	}

	s := Settings{TelegramBotToken: "bot", EpayKey: "key", OneBotHTTPToken: "http", OneBotWebhookToken: "webhook"}.Public()
	if s.TelegramBotToken != "" || s.EpayKey != "" || s.OneBotHTTPToken != "" || s.OneBotWebhookToken != "" || !s.TelegramBotTokenSet || !s.EpayKeySet || !s.OneBotHTTPTokenSet || !s.OneBotWebhookTokenSet {
		t.Fatalf("settings secrets: %+v", s)
	}

	sc := SchedulerConfig{AccessToken: "tok"}.Public()
	if sc.AccessToken != "" || !sc.AccessTokenSet {
		t.Fatalf("scheduler secrets: %+v", sc)
	}

	axon := AxonHubConfig{BaseURL: "http://axonhub", AdminEmail: "admin@example.com", AdminPassword: "password"}.Public()
	if axon.AdminPassword != "" || !axon.AdminPasswordSet || axon.AdminEmail != "admin@example.com" {
		t.Fatalf("AxonHub secrets: %+v", axon)
	}
}

func TestAxonHubConfigMergeKeepsEmptyPassword(t *testing.T) {
	old := AxonHubConfig{BaseURL: "http://axonhub", AdminEmail: "admin@example.com", AdminPassword: "stored", ControlMode: AxonHubControlOff}
	next := AxonHubConfig{BaseURL: "http://axonhub/", AdminEmail: " admin@example.com ", ControlMode: AxonHubControlActive}.MergeUpdate(old)
	if next.BaseURL != "http://axonhub" || next.AdminEmail != "admin@example.com" || next.AdminPassword != "stored" || next.ControlMode != AxonHubControlActive {
		t.Fatalf("merged=%+v", next)
	}
}
