//go:build aix || darwin || dragonfly || freebsd || illumos || ios || linux || netbsd || openbsd || solaris

package monitor

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// configureProbeCommand 让 Codex 及其派生进程处于独立进程组，超时时整体终止。
func configureProbeCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			if errors.Is(err, syscall.ESRCH) {
				return os.ErrProcessDone
			}
			return err
		}
		return nil
	}
	// 防止异常子进程继续持有 CombinedOutput 的管道而无限等待。
	cmd.WaitDelay = 2 * time.Second
}
