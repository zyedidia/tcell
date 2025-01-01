// +build linux solaris

package tcell

import "golang.org/x/sys/unix"

const (
	getTermios   = unix.TCGETS
	setTermios   = unix.TCSETS
	flushTermios = unix.TCSETSF
)
