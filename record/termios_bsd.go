//go:build darwin || freebsd || netbsd || openbsd || dragonfly

package record

import "golang.org/x/sys/unix"

const ioctlGetTermios = unix.TIOCGETA
