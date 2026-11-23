//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package running_test

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func configureGleamShopProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// The Gleam launcher waits for the BEAM, which still owns the log pipe.
	command.Cancel = func() error {
		cause := syscall.Kill(-command.Process.Pid, syscall.SIGTERM)
		if errors.Is(cause, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return cause
	}
}
