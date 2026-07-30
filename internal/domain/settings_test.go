package domain

import "testing"

func TestSettingsPublicAndMergeKeepsSecret(t *testing.T) {
	old := Settings{QQBotAppID: "old", QQBotAppSecret: "secret", Commands: []string{"状态"}}
	updated := Settings{QQBotAppID: "new", Commands: []string{"状态", "status"}}.MergeUpdate(old)
	if updated.QQBotAppID != "new" || updated.QQBotAppSecret != "secret" || len(updated.Commands) != 2 {
		t.Fatalf("合并错误: %+v", updated)
	}
	public := updated.Public()
	if public.QQBotAppSecret != "" || !public.QQBotAppSecretSet {
		t.Fatalf("公开配置泄露密钥: %+v", public)
	}
}
