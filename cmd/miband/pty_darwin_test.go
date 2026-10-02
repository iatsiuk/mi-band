package main

import (
	"fmt"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

// openPTY returns the terminal side of a new pseudo terminal.
func openPTY(t *testing.T) *os.File {
	t.Helper()
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pty: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })
	fd := int(m.Fd()) //nolint:gosec // fd fits in int
	if err := unix.IoctlSetInt(fd, unix.TIOCPTYGRANT, 0); err != nil {
		t.Fatal(err)
	}
	if err := unix.IoctlSetInt(fd, unix.TIOCPTYUNLK, 0); err != nil {
		t.Fatal(err)
	}
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		t.Fatal(err)
	}
	// the terminal side of a ptmx clone is /dev/ttys<minor>, as ptsname reports it
	return openTerminal(t, fmt.Sprintf("/dev/ttys%03d", unix.Minor(uint64(st.Rdev)))) //nolint:gosec // rdev is a device number
}
