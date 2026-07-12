//go:build windows

package monitor

import (
	"os"
	"os/exec"
	"strconv"
	"time"
)

// configureProbeCommand 在 Windows 上通过 taskkill 终止整个派生进程树。
func configureProbeCommand(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		return exec.Command("taskkill", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F").Run()
	}
	cmd.WaitDelay = 2 * time.Second
}
