package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"miband/internal/cloud"
)

// seedSession saves a session under a fresh HOME and returns the session file path.
func seedSession(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	if err := (&cloudFlags{region: "de"}).save(&cloud.Session{UserID: "u", ServiceToken: "st", PassToken: "pt"}); err != nil {
		t.Fatal(err)
	}
	p, err := sessionPath()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLogoutRemovesOnlySavedSession(t *testing.T) {
	p := seedSession(t)
	other := filepath.Join(filepath.Dir(p), "other.json")
	if err := os.WriteFile(other, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, err := run("", "logout")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("session file still there: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("logout removed a neighbour file: %v", err)
	}
	if !strings.Contains(stderr, p) {
		t.Errorf("stderr does not name %s:\n%s", p, stderr)
	}
}

func TestLogoutWithoutSessionSucceeds(t *testing.T) {
	_, stderr, err := execute(t, "logout")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr, "not logged in") {
		t.Errorf("stderr: %q", stderr)
	}
}

func TestLogoutReportsRemovalError(t *testing.T) {
	p := seedSession(t)
	// a non-empty directory in place of the file cannot be removed
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(p, "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, stderr, err := run("", "logout")
	if err == nil || strings.Contains(stderr, "not logged in") {
		t.Fatalf("got %v, stderr %q; want removal error", err, stderr)
	}
}
