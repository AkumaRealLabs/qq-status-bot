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
