//go:build darwin || linux

package main

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func echoOn(t *testing.T, fd int) bool {
	t.Helper()
	tio, err := unix.IoctlGetTermios(fd, ioctlGetTermios)
	if err != nil {
		t.Fatal(err)
	}
	return tio.Lflag&unix.ECHO != 0
}

func openTerminal(t *testing.T, name string) *os.File {
	t.Helper()
	f, err := os.OpenFile(name, os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func TestReadHiddenRestoresEchoOnCancel(t *testing.T) {
	fd := int(openPTY(t).Fd()) //nolint:gosec // fd fits in int
	if !echoOn(t, fd) {
		t.Skip("new pty has echo off")
	}
	pr, pw := io.Pipe()
	defer func() { _ = pw.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := (&cloudFlags{stdin: pr, stderr: io.Discard}).readHidden(ctx, fd)
		done <- err
	}()
	for deadline := time.Now().Add(5 * time.Second); echoOn(t, fd); {
		if time.Now().After(deadline) {
			t.Fatal("echo stays on while the password is read")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("readHidden ignores cancellation")
	}
	if !echoOn(t, fd) {
		t.Fatal("echo left off after cancel")
	}
}
