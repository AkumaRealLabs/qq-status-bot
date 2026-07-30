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
	if cfg.ScreenshotSelector != defaultScreenshotSelector || cfg.StatusURL != "https://status.ggapi.cc" {
		t.Fatalf("默认截图配置错误: %+v", cfg)
	}
}

func TestLoadAllowsFrontendSetupWithoutCredentials(t *testing.T) {
	if _, err := load(func(string) string { return "" }); err != nil {
		t.Fatal(err)
	}
}
