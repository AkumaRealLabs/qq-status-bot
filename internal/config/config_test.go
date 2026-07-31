package config

import "testing"

func TestLoadDefaultsAndLists(t *testing.T) {
	env := map[string]string{
		"QQBOT_APP_ID":         " 123 ",
		"QQBOT_APP_SECRET":     " secret ",
		"QQBOT_ALLOWED_GROUPS": "g1, g2,g1",
		"STATUS_COMMANDS":      "状态, STATUS, status",
	}
	cfg, err := load(func(key string) string { return env[key] })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.QQBotAppID != "123" || len(cfg.AllowedGroups) != 2 || len(cfg.Commands) != 2 {
		t.Fatalf("配置未规范化: %+v", cfg)
	}
	if cfg.StatusPageID != "default" || cfg.StatusPeriod != "1y" || cfg.StatusURL != "https://status.ggapi.cc" {
		t.Fatalf("默认状态图配置错误: %+v", cfg)
	}
}

func TestLoadAllowsFrontendSetupWithoutCredentials(t *testing.T) {
	if _, err := load(func(string) string { return "" }); err != nil {
		t.Fatal(err)
	}
}

func TestLoadValidatesEnabledGGAPISettings(t *testing.T) {
	env := map[string]string{
		"GGAPI_BALANCE_ENABLED": "true", "GGAPI_BASE_URL": "https://www.ggapi.cc", "GGAPI_ADMIN_TOKEN": "token",
		"GGAPI_SMTP_HOST": "smtp.example.com", "GGAPI_SMTP_PORT": "587", "GGAPI_SMTP_USERNAME": "user",
		"GGAPI_SMTP_PASSWORD": "password", "GGAPI_SMTP_FROM": "bot@example.com", "GGAPI_SMTP_TLS_MODE": "starttls",
	}
	cfg, err := load(func(key string) string { return env[key] })
	if err != nil || !cfg.GGAPIBalanceEnabled || cfg.GGAPISmtpPort != 587 {
		t.Fatalf("GGAPI 环境配置错误: cfg=%+v err=%v", cfg, err)
	}
	env["GGAPI_BASE_URL"] = "http://www.ggapi.cc"
	if _, err := load(func(key string) string { return env[key] }); err == nil {
		t.Fatal("启用 GGAPI 时应拒绝 HTTP 地址")
	}
}
