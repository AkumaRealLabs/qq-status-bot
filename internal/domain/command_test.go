package domain

import "testing"

func TestIsCommandMatchesExactTrimmedText(t *testing.T) {
	commands := []string{"状态", "status"}
	for _, input := range []string{" 状态 ", "STATUS", " status\n"} {
		if !IsCommand(input, commands) {
			t.Fatalf("应匹配命令 %q", input)
		}
	}
	for _, input := range []string{"查状态", "status now", ""} {
		if IsCommand(input, commands) {
			t.Fatalf("不应匹配命令 %q", input)
		}
	}
}

func TestNormalizeContentRemovesLeadingBotMention(t *testing.T) {
	for _, input := range []string{"@机器人 绑定", "<@bot> 绑定", "<@!123> 状态"} {
		if got := NormalizeContent(input); got == "" || (got != "绑定" && got != "状态") {
			t.Fatalf("提及正文规范化错误: input=%q got=%q", input, got)
		}
	}
}

func TestHelpCommandAliases(t *testing.T) {
	for _, input := range []string{"帮助", "菜单", "怎么用", "聊天帮助", "@机器人 菜单"} {
		if !IsHelpCommand(input) || !IsAccountCommand(input) {
			t.Fatalf("帮助别名未识别: %q", input)
		}
	}
}
