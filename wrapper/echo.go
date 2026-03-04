package main

import "golang.org/x/sys/unix"

// disableEcho turns off echo on the PTY slave fd. Must be called on the slave
// (tty), not the master — the master has no termios on Linux (ENOTTY).
func disableEcho(fd int) {
	termios, err := unix.IoctlGetTermios(fd, ioctlReadTermios)
	if err != nil {
		return
	}
	termios.Lflag &^= unix.ECHO | unix.ECHOE | unix.ECHOK | unix.ECHONL
	_ = unix.IoctlSetTermios(fd, ioctlWriteTermios, termios)
}

func enableEcho(fd int) {
	termios, err := unix.IoctlGetTermios(fd, ioctlReadTermios)
	if err != nil {
		return
	}
	termios.Lflag |= unix.ECHO | unix.ECHOE | unix.ECHOK
	_ = unix.IoctlSetTermios(fd, ioctlWriteTermios, termios)
}
