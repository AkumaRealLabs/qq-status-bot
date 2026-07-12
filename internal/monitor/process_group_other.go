//go:build !(aix || darwin || dragonfly || freebsd || illumos || ios || linux || netbsd || openbsd || solaris || windows)

package monitor

import (
	"os/exec"
	"time"
)

func configureProbeCommand(cmd *exec.Cmd) {
	cmd.WaitDelay = 2 * time.Second
}
