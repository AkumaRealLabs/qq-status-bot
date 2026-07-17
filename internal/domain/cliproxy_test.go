package domain

import "testing"

func TestValidateCLIProxyAuthFileName(t *testing.T) {
	tests := map[string]bool{
		"codex.json":     true,
		" account.JSON ": true,
		"":               false,
		"../codex.json":  false,
		"dir/codex.json": false,
		`dir\codex.json`: false,
		"codex.txt":      false,
		"codex.json.bak": false,
	}
	for name, ok := range tests {
		err := ValidateCLIProxyAuthFileName(name)
		if (err == nil) != ok {
			t.Fatalf("ValidateCLIProxyAuthFileName(%q) err=%v ok=%v", name, err, ok)
		}
	}
}

func TestIsCodexCLIProxyAccount(t *testing.T) {
	tests := []struct {
		name    string
		account CLIProxyAuthFile
		want    bool
	}{
		{name: "provider", account: CLIProxyAuthFile{Provider: "codex"}, want: true},
		{name: "type", account: CLIProxyAuthFile{Type: "CODEX"}, want: true},
		{name: "filename fallback", account: CLIProxyAuthFile{Name: "codex-user.json"}, want: true},
		{name: "xai provider", account: CLIProxyAuthFile{Name: "codex-xai.json", Provider: "xai"}, want: false},
		{name: "xai filename", account: CLIProxyAuthFile{Name: "xai-user.json"}, want: false},
		{name: "unknown", account: CLIProxyAuthFile{Name: "user.json"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsCodexCLIProxyAccount(tt.account); got != tt.want {
				t.Fatalf("IsCodexCLIProxyAccount(%+v) = %v, want %v", tt.account, got, tt.want)
			}
		})
	}
}
