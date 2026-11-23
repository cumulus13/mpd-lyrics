//go:build darwin || freebsd || netbsd || openbsd

package termx

import "syscall"

const (
	ioctlGetTermios = syscall.TIOCGETA
	ioctlSetTermios = syscall.TIOCSETA
)
