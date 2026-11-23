//go:build !(aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris)

package running_test

import "os/exec"

func configureGleamShopProcess(_ *exec.Cmd) {}
