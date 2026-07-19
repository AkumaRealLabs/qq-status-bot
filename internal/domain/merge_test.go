package domain

import (
	"strings"
	"testing"
	"time"
)

func TestUpstreamMergeUpdate(t *testing.T) {
	old := Upstream{
		ID: "u1", AccessToken: "tok", Password: "pw",
		Sub2APIAccessToken: "a", Sub2APIRefreshToken: "r",
		LastError: "e", FailureCount: 3, CreatedAt: time.Unix(100, 0).UTC(),
	}
	in := Upstream{Name: "N", Type: "newapi", BaseURL: "https://x", AccessToken: "", Password: "new"}
	got := in.MergeUpdate(old)
	if got.ID != "u1" || got.AccessToken != "tok" || got.Password != "new" {
		t.Fatalf("secrets/id = %+v", got)
	}
	if got.Sub2APIAccessToken != "a" || got.Sub2APIRefreshToken != "r" {
		t.Fatalf("sub2 tokens = %+v", got)
	}
	if got.LastError != "e" || got.FailureCount != 3 || !got.CreatedAt.Equal(old.CreatedAt) {
		t.Fatalf("runtime = %+v", got)
	}
}

func TestModelCardMergeUpdate(t *testing.T) {
	old := ModelCard{
		ID: "c1", BaseURL: "https://api", APIKey: "sk-old", UpstreamID: "", KeyID: "",
		SchedulerGroup: "g1", SchedulerChannelID: "ch1", SchedulerAutoDisabled: true,
		LastError: "err", FailureCount: 2, SortOrder: 5, CreatedAt: time.Unix(1, 0).UTC(),
	}
	// 空来源保留旧绑定 + 密钥
	in := ModelCard{Name: "Card", APIKey: "", SchedulerGroup: "g1", SchedulerChannelID: "ch1", SchedulerAutoDisabled: true, PoolEnabled: true}
	got := in.MergeUpdate(old)
	if got.BaseURL != "https://api" || got.APIKey != "sk-old" || got.ID != "c1" {
		t.Fatalf("source/secret = %+v", got)
	}
	if got.FailureCount != 2 || got.SortOrder != 5 {
		t.Fatalf("runtime = %+v", got)
	}
	// 渠道变更清除自动关渠
	in2 := ModelCard{Name: "Card", BaseURL: "https://api", APIKey: "sk", SchedulerGroup: "g1", SchedulerChannelID: "ch2", SchedulerAutoDisabled: true, PoolEnabled: true}
	got2 := in2.MergeUpdate(old)
	if got2.SchedulerAutoDisabled {
		t.Fatal("channel change should clear auto-disabled")
	}
}

func TestApplySchedulerPatches(t *testing.T) {
	g, ch, name, auto := ApplySchedulerGroupPatch("g1", "ch1", "n1", true, "g2")
	if g != "g2" || ch != "" || name != "" || auto {
		t.Fatalf("group change = %q %q %q %v", g, ch, name, auto)
	}
	g, ch, name, auto = ApplySchedulerGroupPatch("g1", "ch1", "n1", true, "g1")
	if g != "g1" || ch != "ch1" || name != "n1" || !auto {
		t.Fatalf("same group = %q %q %q %v", g, ch, name, auto)
	}
	ch, auto = ApplySchedulerChannelPatch("ch1", true, "ch2")
	if ch != "ch2" || auto {
		t.Fatalf("channel change = %q %v", ch, auto)
	}
	ch, auto = ApplySchedulerChannelPatch("ch1", true, "ch1")
	if ch != "ch1" || !auto {
		t.Fatalf("same channel = %q %v", ch, auto)
	}
}

func TestRevenueCardMergeUpdate(t *testing.T) {
	old := RevenueCard{
		ID: "r1", SourceType: "newapi_orders", BaseURL: "https://old", UserID: "u",
		AccessToken: "tok", UpstreamID: "up1", SortOrder: 2, CreatedAt: time.Unix(2, 0).UTC(),
	}
	in := RevenueCard{Name: "R", SourceType: "newapi_orders", BaseURL: "", UserID: "", AccessToken: ""}
	got := in.MergeUpdate(old)
	if got.BaseURL != "https://old" || got.UserID != "u" || got.AccessToken != "tok" || got.UpstreamID != "up1" {
		t.Fatalf("keep = %+v", got)
	}
	if got.ID != "r1" || got.SortOrder != 2 {
		t.Fatalf("id/sort = %+v", got)
	}
}

func TestSettingsMergeUpdate(t *testing.T) {
	old := Settings{
		CheckIntervalMinutes: 5, TelegramBotToken: "bot", EpayKey: "ek",
		OneBotHTTPToken: "http-old", OneBotWebhookToken: "webhook-old", OneBotGroupIDs: []string{"100"},
		NotificationRules: DefaultNotificationRules(), SiteName: "Old",
	}
	in := Settings{CheckIntervalMinutes: 0, TelegramBotToken: "", EpayKey: "new", SiteName: ""}
	got := in.MergeUpdate(old)
	if got.CheckIntervalMinutes != 5 {
		t.Fatalf("interval = %d", got.CheckIntervalMinutes)
	}
	if got.TelegramBotToken != "bot" || got.EpayKey != "new" {
		t.Fatalf("secrets = %+v", got)
	}
	if got.SiteName != DefaultSiteName {
		t.Fatalf("site = %q", got.SiteName)
	}
	if !got.NotificationRules.Enabled || got.NotificationRules.FailureThreshold != 2 {
		t.Fatalf("rules kept = %+v", got.NotificationRules)
	}
	if got.OneBotHTTPToken != "http-old" || got.OneBotWebhookToken != "webhook-old" {
		t.Fatalf("onebot secrets should be kept: %+v", got)
	}
	in2 := Settings{CheckIntervalMinutes: 10, NotificationRules: NotificationRules{Enabled: false, FailureThreshold: 3, EventTypes: map[string]bool{"probe_failed": false}}}
	got2 := in2.MergeUpdate(old)
	if got2.NotificationRules.Enabled || got2.NotificationRules.FailureThreshold != 3 {
		t.Fatalf("rules updated = %+v", got2.NotificationRules)
	}
}

func TestNormalizeAndValidateOneBotSettings(t *testing.T) {
	groups := NormalizeOneBotGroupIDs([]string{" 100 ", "", "200", "100", " 300"})
	if got := strings.Join(groups, ","); got != "100,200,300" {
		t.Fatalf("groups = %q", got)
	}
	cfg := Settings{OneBotEnabled: true, OneBotBaseURL: "http://llbot:3000", OneBotHTTPToken: "http", OneBotWebhookToken: "webhook", OneBotGroupIDs: groups}
	if err := ValidateOneBotSettings(cfg); err != nil {
		t.Fatal(err)
	}
	cfg.OneBotGroupIDs = []string{"0"}
	if err := ValidateOneBotSettings(cfg); err == nil {
		t.Fatal("expected invalid group error")
	}
	cfg.OneBotGroupIDs = []string{"100"}
	cfg.OneBotBaseURL = "ftp://llbot"
	if err := ValidateOneBotSettings(cfg); err == nil {
		t.Fatal("expected invalid URL error")
	}
}

func TestSchedulerAndCLIProxyMergeUpdate(t *testing.T) {
	oldS := SchedulerConfig{BaseURL: "https://s", UserID: "1", AccessToken: "tok", UnassignedGroup: "old"}
	gotS := SchedulerConfig{BaseURL: " https://s2/ ", UserID: " 2 ", AccessToken: "", UnassignedGroup: " unassigned "}.MergeUpdate(oldS)
	if gotS.BaseURL != "https://s2" || gotS.UserID != "2" || gotS.AccessToken != "tok" || gotS.UnassignedGroup != "unassigned" {
		t.Fatalf("scheduler = %+v", gotS)
	}
	oldC := CLIProxyConfig{Name: "CPA", BaseURL: "https://c", ManagementKey: "mk"}
	gotC := CLIProxyConfig{Name: "", BaseURL: " https://c2/ ", ManagementKey: ""}.MergeUpdate(oldC)
	if gotC.Name != DefaultCLIProxyName || gotC.BaseURL != "https://c2" || gotC.ManagementKey != "mk" {
		t.Fatalf("cliproxy = %+v", gotC)
	}
}
