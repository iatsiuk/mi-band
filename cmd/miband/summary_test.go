package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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
	if retried, err := cl.retry(context.Background(), errors.New("other")); retried || err == nil || err.Error() != "other" {
		t.Fatalf("non-401: retried=%v err=%v", retried, err)
	}
}

func TestLiveRecordsKeepsTombstonesForRaw(t *testing.T) {
	day := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	recs := []cloud.Record{stepRecord("band", day, 1, true), stepRecord("x", day, 1, false)}
	if got := deviceRecords(recs, "band"); len(got) != 1 || !got[0].Deleted {
		t.Fatalf("got %+v", got)
	}
}
