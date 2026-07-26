//go:build darwin

package main

import (
	"os"
	"syscall"
	"unsafe"
)

func isTerminal(file *os.File) bool {
	if file == nil {
		return false
	}

	var termios syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), uintptr(syscall.TIOCGETA), uintptr(unsafe.Pointer(&termios)))
	return errno == 0
}
