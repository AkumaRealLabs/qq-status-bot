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
