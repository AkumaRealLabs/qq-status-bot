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

	s := Settings{TelegramBotToken: "bot", EpayKey: "key"}.Public()
	if s.TelegramBotToken != "" || s.EpayKey != "" || !s.TelegramBotTokenSet || !s.EpayKeySet {
		t.Fatalf("settings secrets: %+v", s)
	}

	sc := SchedulerConfig{AccessToken: "tok"}.Public()
	if sc.AccessToken != "" || !sc.AccessTokenSet {
		t.Fatalf("scheduler secrets: %+v", sc)
	}

	c := ModelCard{APIKey: "sk"}.Public()
	if c.APIKey != "" || !c.APIKeySet {
		t.Fatalf("card secrets: %+v", c)
	}
}
