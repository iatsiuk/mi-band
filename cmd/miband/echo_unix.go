//go:build darwin || linux

package main

import (
	"context"

	"golang.org/x/sys/unix"
)

// readHidden reads a line with echo off. Echo is switched here rather than in the reading
// goroutine, so the restore always runs after it, even when ctx stops a read that is still blocked.
func (c *cloudFlags) readHidden(ctx context.Context, fd int) (string, error) {
	restore, err := noEcho(fd)
	if err != nil {
		return "", err
	}
	defer restore()
	return readCtx(ctx, c.readLine)
}

// noEcho sets the line mode of term.ReadPassword: canonical input and signals, no echo.
func noEcho(fd int) (func(), error) {
	tio, err := unix.IoctlGetTermios(fd, ioctlGetTermios)
	if err != nil {
		return nil, err
	}
	old := *tio
	tio.Lflag &^= unix.ECHO
	tio.Lflag |= unix.ICANON | unix.ISIG
	tio.Iflag |= unix.ICRNL
	if err := unix.IoctlSetTermios(fd, ioctlSetTermios, tio); err != nil {
		return nil, err
	}
	return func() { _ = unix.IoctlSetTermios(fd, ioctlSetTermios, &old) }, nil
}
