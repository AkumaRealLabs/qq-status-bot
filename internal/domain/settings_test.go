package domain

import "testing"

func TestSettingsPublicAndMergeKeepsSecret(t *testing.T) {
	old := Settings{QQBotAppID: "old", QQBotAppSecret: "secret", Commands: []string{"状态"}, StatusPageID: "default", StatusPeriod: "1y",
		GGAPIAdminToken: "admin-token", GGAPISmtpPassword: "smtp-password"}
	updated := Settings{QQBotAppID: "new", Commands: []string{"状态", "status"}}.MergeUpdate(old)
	if updated.QQBotAppID != "new" || updated.QQBotAppSecret != "secret" || updated.GGAPIAdminToken != "admin-token" || updated.GGAPISmtpPassword != "smtp-password" || len(updated.Commands) != 2 {
		t.Fatalf("合并错误: %+v", updated)
	}
	public := updated.Public()
	if public.QQBotAppSecret != "" || public.GGAPIAdminToken != "" || public.GGAPISmtpPassword != "" ||
		!public.QQBotAppSecretSet || !public.GGAPIAdminTokenSet || !public.GGAPISmtpPasswordSet {
		t.Fatalf("公开配置泄露密钥: %+v", public)
	}
}

func TestSettingsPublicUsesEmptyListsInsteadOfNull(t *testing.T) {
	public := (Settings{}).Public()
	if public.AllowedGroups == nil || public.Commands == nil {
		t.Fatalf("公开配置中的列表不能为 nil: %+v", public)
	}
}

func TestSettingsValidateStatusImageOptions(t *testing.T) {
	valid := Settings{StatusURL: "https://status.example", StatusPageID: "default", StatusPeriod: "1y", ScreenshotTimeout: 90, QueueSize: 3}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	invalid := valid
	invalid.StatusURL = "file:///tmp/status"
	if err := invalid.Validate(); err == nil {
		t.Fatal("应拒绝非 HTTP/HTTPS 数据源")
	}
}

func TestSettingsValidateGGAPIWhenEnabled(t *testing.T) {
	valid := Settings{StatusURL: "https://status.example", StatusPageID: "default", StatusPeriod: "1y", ScreenshotTimeout: 90, QueueSize: 3,
		GGAPIBalanceEnabled: true, GGAPIBaseURL: "https://www.ggapi.cc", GGAPIAdminToken: "token", GGAPISmtpHost: "smtp.example.com", GGAPISmtpPort: 587,
		GGAPISmtpUsername: "user", GGAPISmtpPassword: "password", GGAPISmtpFrom: "bot@example.com", GGAPISmtpTLSMode: "starttls"}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	invalid := valid
	invalid.GGAPIBaseURL = "http://www.ggapi.cc"
	if err := invalid.Validate(); err == nil {
		t.Fatal("启用余额时应拒绝 HTTP GGAPI 地址")
	}
	public := valid.Public()
	if public.GGAPIAdminToken != "" || public.GGAPISmtpPassword != "" || !public.GGAPIAdminTokenSet || !public.GGAPISmtpPasswordSet {
		t.Fatalf("GGAPI 密钥未正确脱敏: %+v", public)
	}
}
