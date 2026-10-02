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

func TestLogoutEndsServerSessionAndRemovesOnlySavedSession(t *testing.T) {
	fa := &fakeAccount{}
	newFakeAccount(t, fa)
	p := seedSession(t)
	other := filepath.Join(filepath.Dir(p), "other.json")
	if err := os.WriteFile(other, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, err := run("", "logout")
	if err != nil {
		t.Fatal(err)
	}
	if len(fa.logouts) != 1 || fa.logouts[0] != "passToken=pt" {
		t.Errorf("server logouts: %v", fa.logouts)
	}
	if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("session file still there: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("logout removed a neighbour file: %v", err)
	}
	if !strings.Contains(stderr, p) || !strings.Contains(stderr, "account session ended") {
		t.Errorf("stderr:\n%s", stderr)
	}
}

func TestLogoutRemovesSessionWhenServerFails(t *testing.T) {
	newFakeAccount(t, &fakeAccount{logoutFails: true})
	p := seedSession(t)
	_, _, err := run("", "logout")
	if err == nil || !strings.Contains(err.Error(), "server") || strings.Contains(err.Error(), "pt") {
		t.Fatalf("got %v, want a server logout error without the token", err)
	}
	if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("session file still there: %v", err)
	}
}

func TestLogoutWithoutSessionSucceeds(t *testing.T) {
	fa := &fakeAccount{}
	newFakeAccount(t, fa)
	_, stderr, err := execute(t, "logout")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr, "not logged in") || len(fa.logouts) != 0 {
		t.Errorf("stderr %q, server logouts %v", stderr, fa.logouts)
	}
}

func TestLogoutRemovesBrokenSessionLocally(t *testing.T) {
	fa := &fakeAccount{}
	newFakeAccount(t, fa)
	p := seedSession(t)
	if err := writeSecret(p, []byte("{broken")); err != nil {
		t.Fatal(err)
	}
	_, stderr, err := run("", "logout")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) || len(fa.logouts) != 0 || !strings.Contains(stderr, "local only") {
		t.Fatalf("file %v, server logouts %v, stderr %q", err, fa.logouts, stderr)
	}
}

func TestLogoutReportsRemovalError(t *testing.T) {
	newFakeAccount(t, &fakeAccount{})
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
