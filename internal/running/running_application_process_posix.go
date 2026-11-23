//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package running

import (
	"os"
	"os/exec"
	"syscall"
)

func configureApplicationProcess(command *exec.Cmd) {
	attributes := new(syscall.SysProcAttr)
	attributes.Setpgid = true
	command.SysProcAttr = attributes
	// The terminal signals the CLI's group. Only the CLI forwards cancellation to the app.
	command.Cancel = func() error { return command.Process.Signal(os.Interrupt) }
}
