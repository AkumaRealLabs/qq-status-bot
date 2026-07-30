package domain

import "testing"

func TestSettingsPublicAndMergeKeepsSecret(t *testing.T) {
	old := Settings{QQBotAppID: "old", QQBotAppSecret: "secret", Commands: []string{"状态"}, StatusPageID: "default", StatusPeriod: "1y"}
	updated := Settings{QQBotAppID: "new", Commands: []string{"状态", "status"}}.MergeUpdate(old)
	if updated.QQBotAppID != "new" || updated.QQBotAppSecret != "secret" || len(updated.Commands) != 2 {
		t.Fatalf("合并错误: %+v", updated)
	}
	public := updated.Public()
	if public.QQBotAppSecret != "" || !public.QQBotAppSecretSet {
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
