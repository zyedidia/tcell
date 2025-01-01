// +build freebsd netbsd openbsd dragonfly darwin

package tcell

import "golang.org/x/sys/unix"

const (
	getTermios   = unix.TIOCGETA
	setTermios   = unix.TIOCSETA
	flushTermios = unix.TIOCSETAF
)
