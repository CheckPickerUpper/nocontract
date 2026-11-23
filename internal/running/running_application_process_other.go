//go:build !(aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris)

package running

import "os/exec"

func configureApplicationProcess(_ *exec.Cmd) {}
