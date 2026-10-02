//go:build !darwin && !linux

package main

import (
	"context"

	"golang.org/x/term"
)

// readHidden falls back to term.ReadPassword, which switches echo inside the reading goroutine,
// so a cancel that lands as the read starts can leave echo off.
func (c *cloudFlags) readHidden(ctx context.Context, fd int) (string, error) {
	st, err := term.GetState(fd)
	if err != nil {
		return "", err
	}
	defer func() { _ = term.Restore(fd, st) }()
	return readCtx(ctx, func() (string, error) {
		b, err := term.ReadPassword(fd)
		return string(b), err
	})
}
