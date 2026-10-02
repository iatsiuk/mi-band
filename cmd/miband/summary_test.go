package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"miband/internal/cloud"
)

func stepRecord(did string, t time.Time, steps int, deleted bool) cloud.Record {
	v, _ := json.Marshal(map[string]any{"time": t.Unix(), "steps": steps, "distance": steps / 2, "calories": 1})
	return cloud.Record{DID: did, Key: "steps", Time: t, ZoneOffset: 3 * 3600, Deleted: deleted, Value: v}
}

func TestStepsByDayGroupsByDeviceAndSkipsDeleted(t *testing.T) {
	day := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	got := stepsByDay([]cloud.Record{
		stepRecord("band", day, 100, false),
		stepRecord("band", day.Add(time.Hour), 50, false),
		stepRecord("phone", day, 70, false),
		stepRecord("band", day.Add(2*time.Hour), 999, true),
		// 22:30 UTC is already the next day at +03:00
		stepRecord("band", time.Date(2026, 10, 1, 22, 30, 0, 0, time.UTC), 5, false),
	}, "")
	want := []daySteps{
		{DID: "band", Date: "2026-10-01", Steps: 150, DistanceM: 75, CaloriesKcal: 2},
		{DID: "band", Date: "2026-10-02", Steps: 5, DistanceM: 2, CaloriesKcal: 1},
		{DID: "phone", Date: "2026-10-01", Steps: 70, DistanceM: 35, CaloriesKcal: 1},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d: got %+v want %+v", i, got[i], want[i])
		}
	}
}

func TestStepsByDayFiltersDevice(t *testing.T) {
	day := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	got := stepsByDay([]cloud.Record{stepRecord("band", day, 100, false), stepRecord("phone", day, 70, false)}, "band")
	if len(got) != 1 || got[0].DID != "band" || got[0].Steps != 100 {
		t.Fatalf("got %+v", got)
	}
}

func TestLiveRecordsDropsTombstonesAndOtherDevices(t *testing.T) {
	day := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	got := liveRecords([]cloud.Record{stepRecord("band", day, 1, false), stepRecord("band", day, 1, true), stepRecord("x", day, 1, false)}, "band")
	if len(got) != 1 {
		t.Fatalf("got %d records", len(got))
	}
}

func TestWriteSecretTightensExistingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cloud.json")
	if err := os.WriteFile(p, []byte("old"), 0o644); err != nil { //nolint:gosec // the test needs a world-readable file
		t.Fatal(err)
	}
	if err := writeSecret(p, []byte("new")); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if st.Mode().Perm() != 0o600 || string(b) != "new" {
		t.Fatalf("mode %v content %q", st.Mode().Perm(), b)
	}
}

func TestRetrySurfacesRefreshError(t *testing.T) {
	want := errors.New("verification needed")
	cl := &client{
		flags: &cloudFlags{},
		sess:  &cloud.Session{UserID: "1", PassToken: "pt"},
		relogin: func(context.Context, string, string) (*cloud.Session, error) {
			return nil, want
		},
	}
	retried, err := cl.retry(context.Background(), cloud.ErrUnauthorized)
	if retried || !errors.Is(err, want) {
		t.Fatalf("retried=%v err=%v", retried, err)
	}
}

func TestRetryIgnoresNonAuthErrors(t *testing.T) {
	cl := &client{
		flags: &cloudFlags{},
		sess:  &cloud.Session{UserID: "1", PassToken: "pt"},
		relogin: func(context.Context, string, string) (*cloud.Session, error) {
			t.Fatal("relogin on a non-401 error")
			return nil, nil
		},
	}
	if retried, err := cl.retry(context.Background(), errors.New("other")); retried || err == nil || err.Error() != "other" {
		t.Fatalf("retried=%v err=%v", retried, err)
	}
}

func TestWithRetryRefreshesOnceAndSavesSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	logins := 0
	cl := &client{
		flags: &cloudFlags{},
		sess:  &cloud.Session{UserID: "1", PassToken: "old"},
		relogin: func(_ context.Context, userID, passToken string) (*cloud.Session, error) {
			logins++
			if userID != "1" || passToken != "old" {
				t.Errorf("relogin with %q/%q", userID, passToken)
			}
			return &cloud.Session{UserID: "1", PassToken: "new"}, nil
		},
	}
	var seen []string
	call := func(s *cloud.Session) (int, error) {
		seen = append(seen, s.PassToken)
		if s.PassToken == "old" {
			return 0, cloud.ErrUnauthorized
		}
		return 42, nil
	}
	v, err := withRetry(context.Background(), cl, call)
	if err != nil || v != 42 {
		t.Fatalf("got %d, %v", v, err)
	}
	if len(seen) != 2 || seen[1] != "new" {
		t.Fatalf("calls with %q, want old then new", seen)
	}
	p, err := sessionPath()
	if err != nil {
		t.Fatal(err)
	}
	var saved savedSession
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &saved); err != nil || saved.Session.PassToken != "new" {
		t.Fatalf("saved %s, %v", b, err)
	}

	// a second 401 in the same run fails instead of logging in again
	if _, err := withRetry(context.Background(), cl, func(*cloud.Session) (int, error) { return 0, cloud.ErrUnauthorized }); !errors.Is(err, cloud.ErrUnauthorized) {
		t.Fatalf("second 401: %v", err)
	}
	if logins != 1 {
		t.Fatalf("%d logins, want 1", logins)
	}
}

func TestSavedSessionUsesSnakeCaseKeys(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	sess := &cloud.Session{UserID: "u", CUserID: "c", ServiceToken: "st", Ssecurity: "ss", PassToken: "pt"}
	if err := (&cloudFlags{region: "de"}).save(sess); err != nil {
		t.Fatal(err)
	}
	p, err := sessionPath()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		Session map[string]string `json:"session"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"user_id": "u", "c_user_id": "c", "service_token": "st", "ssecurity": "ss", "pass_token": "pt"}
	if len(raw.Session) != len(want) {
		t.Fatalf("session keys %v", raw.Session)
	}
	for k, v := range want {
		if raw.Session[k] != v {
			t.Errorf("%s: got %q, want %q", k, raw.Session[k], v)
		}
	}
	cf := &cloudFlags{}
	saved, ok, err := cf.loadSaved()
	if err != nil || !ok || saved.Session.PassToken != "pt" || cf.region != "de" {
		t.Fatalf("load: %+v %v %v", saved, ok, err)
	}
}

func TestReadPasswordStopsOnCancel(t *testing.T) {
	t.Setenv("MIBAND_PASSWORD", "")
	pr, pw := io.Pipe()
	defer func() { _ = pw.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	cf := &cloudFlags{stdin: pr, stderr: io.Discard}
	done := make(chan error, 1)
	go func() {
		_, err := cf.readPassword(ctx)
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("readPassword ignores cancellation")
	}
}

func TestReadPasswordTakesFirstStdinLine(t *testing.T) {
	t.Setenv("MIBAND_PASSWORD", "")
	cf := &cloudFlags{stdin: strings.NewReader("secret\r\nrest\n"), stderr: io.Discard}
	if got, err := cf.readPassword(context.Background()); err != nil || got != "secret" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestDeviceRecordsKeepsTombstones(t *testing.T) {
	day := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	recs := []cloud.Record{stepRecord("band", day, 1, true), stepRecord("x", day, 1, false)}
	if got := deviceRecords(recs, "band"); len(got) != 1 || !got[0].Deleted {
		t.Fatalf("got %+v", got)
	}
}

func TestReadPasswordCanceledBeforePrompt(t *testing.T) {
	t.Setenv("MIBAND_PASSWORD", "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cf := &cloudFlags{stdin: strings.NewReader("secret\n"), stderr: io.Discard}
	if got, err := cf.readPassword(ctx); !errors.Is(err, context.Canceled) || got != "" {
		t.Fatalf("got %q, %v; want context.Canceled", got, err)
	}
}

func TestLoginRejectsSavedSessionWithoutToken(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	p, err := sessionPath()
	if err != nil {
		t.Fatal(err)
	}
	// keys of a session saved before Session had json tags
	old := `{"region":"de","device_id":"android_x","session":{"UserID":"1","ServiceToken":"st","Ssecurity":"ss","PassToken":"pt"}}`
	if err := writeSecret(p, []byte(old)); err != nil {
		t.Fatal(err)
	}
	_, err = (&cloudFlags{}).login(context.Background())
	if !errors.Is(err, errNotLoggedIn) || exitCode(err) != exitAuth {
		t.Fatalf("got %v, want errNotLoggedIn", err)
	}
}
