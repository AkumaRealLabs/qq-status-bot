//go:build aix || darwin || dragonfly || freebsd || illumos || ios || linux || netbsd || openbsd || solaris

package monitor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProbeCodexCLITimeoutStopsProcessGroup(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "child-survived")
	t.Setenv("AUM_CHILD_MARKER", marker)
	fake := fakeCodex(t, `#!/bin/sh
(
  sleep 0.4
  printf leaked > "$AUM_CHILD_MARKER"
) >/dev/null 2>&1 &
while :; do
  sleep 30
done
`)

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	got := (Client{ProbeMode: ProbeModeCLI, CodexPath: fake}).Probe(ctx, "https://codex.example.test", "sk-card-secret", "gpt-5.6-sol")
	if got.Success || got.Status != StatusFailed || !strings.Contains(got.Error, "探测超时") {
		t.Fatalf("got=%+v", got)
	}
	time.Sleep(500 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("Codex 子进程在超时后仍在运行")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
