package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"miband/internal/cloud"
)

func intp(v int) *int { return &v }

func TestRootFormatDefaultsToMarkdown(t *testing.T) {
	t.Parallel()
	got, err := newRootCmd().PersistentFlags().GetString("format")
	if err != nil {
		t.Fatal(err)
	}
	if got != formatMarkdown {
		t.Errorf("got %q, want %q", got, formatMarkdown)
	}
}

func TestRootRejectsUnknownFormat(t *testing.T) {
	_, _, err := execute(t, "devices", "--format", "xml")
	if err == nil || !strings.Contains(err.Error(), "--format") {
		t.Fatalf("got %v, want --format error", err)
	}
}

func render(t *testing.T, format string, v markdowner) string {
	t.Helper()
	var b bytes.Buffer
	if err := writeOutput(&b, format, v); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestDevicesMarkdownTable(t *testing.T) {
	t.Parallel()
	got := render(t, formatMarkdown, deviceList{{DID: "123456789", Model: "miwear.watch.o66gl", Name: "Band | 10", MAC: "AA:BB:CC:DD:EE:FF"}})
	for _, want := range []string{
		"| DID | Model | Name | MAC | SN |",
		"|---|---|---|---|---|",
		`| 123456789 | miwear.watch.o66gl | Band \| 10 | AA:BB:CC:DD:EE:FF |  |`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in:\n%s", want, got)
		}
	}
}

func TestDevicesJSONKeepsFieldNames(t *testing.T) {
	t.Parallel()
	got := render(t, formatJSON, deviceList{{DID: "1", Model: "m", Name: "n"}})
	var rows []map[string]string
	if err := json.Unmarshal([]byte(got), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["did"] != "1" || rows[0]["model"] != "m" {
		t.Fatalf("got %s", got)
	}
}

func sampleData() *dataResult {
	zone := time.FixedZone("", 3*3600)
	bed := time.Date(2026, 9, 30, 23, 10, 0, 0, zone)
	sleepRec := &cloud.Record{DID: "band", Key: "watch_night_sleep", Time: bed, ZoneOffset: 3 * 3600}
	hrRec := &cloud.Record{DID: "band", Key: "heart_rate", Time: bed, ZoneOffset: 3 * 3600}
	start := time.Date(2026, 10, 1, 7, 0, 0, 0, zone)
	return &dataResult{
		From: time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC),
		Sections: []dataSection{
			{Key: "steps_by_day", Value: []daySteps{{DID: "band", Date: "2026-10-01", Steps: 11898, DistanceM: 8100, CaloriesKcal: 410.5}}},
			{Key: "watch_night_sleep", Value: []any{decodedRecord{DID: "band", Value: &cloud.Sleep{
				BedTime: bed, WakeupTime: bed.Add(8 * time.Hour), Duration: 480,
				DeepDuration: intp(90), LightDuration: intp(300), REMDuration: intp(70), AwakeDuration: intp(20), TotalScore: intp(82),
			}, rec: sleepRec}}},
			{Key: "heart_rate", Value: []any{decodedRecord{DID: "band", Value: &cloud.HeartRate{Time: bed, BPM: 58}, rec: hrRec}}},
			{Key: "workouts", Value: []workoutOut{{
				Workout: cloud.Workout{DID: "band", Key: "outdoor_running", Time: start, ZoneOffset: 3 * 3600, Report: &cloud.WorkoutReport{
					StartTime: start, DurationS: 1830, DistanceM: intp(5200), CaloriesKcal: intp(350), AvgHR: intp(150), MaxHR: intp(172),
				}},
				Unavailable: []string{"recover_rate"},
			}}},
		},
		Partial: true,
	}
}

func TestDataMarkdownSections(t *testing.T) {
	t.Parallel()
	got := render(t, formatMarkdown, sampleData())
	for _, want := range []string{
		"Period: 2026-09-30 00:00 UTC - 2026-10-01 12:00 UTC",
		"## Steps by day",
		"| band | 2026-10-01 | 11898 | 8100 | 410.5 |",
		"## Watch night sleep",
		"| band | 2026-09-30 23:10 | 23:10-07:10, 480 min (deep 90, light 300, REM 70, awake 20), score 82 |",
		"## Heart rate",
		"| band | 2026-09-30 23:10 | 58 bpm |",
		"## Workouts",
		"| band | 2026-10-01 07:00 | outdoor_running | 30:30 | 5200 | 350 | 150 | 172 |",
		"some detail files failed to download",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in:\n%s", want, got)
		}
	}
}

func TestDataJSONShape(t *testing.T) {
	t.Parallel()
	var got map[string]json.RawMessage
	if err := json.Unmarshal([]byte(render(t, formatJSON, sampleData())), &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"from", "to", "steps_by_day", "watch_night_sleep", "heart_rate", "workouts", "partial"} {
		if _, ok := got[key]; !ok {
			t.Errorf("no key %q in %v", key, got)
		}
	}
	var hr []map[string]json.RawMessage
	if err := json.Unmarshal(got["heart_rate"], &hr); err != nil || len(hr) != 1 || string(hr[0]["did"]) != `"band"` {
		t.Fatalf("heart_rate: %s (%v)", got["heart_rate"], err)
	}
}

func TestDataMarkdownRawRecords(t *testing.T) {
	t.Parallel()
	rec := cloud.Record{DID: "band", Key: "steps", Time: time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC), Deleted: true, Value: json.RawMessage(`{"steps":5}`)}
	got := render(t, formatMarkdown, &dataResult{Sections: []dataSection{{Key: "steps", Value: []cloud.Record{rec}}}})
	if want := "| band | 2026-10-01 10:00 | yes | `{\"steps\":5}` |"; !strings.Contains(got, want) {
		t.Errorf("no %q in:\n%s", want, got)
	}
}

func TestDataMarkdownEmptySection(t *testing.T) {
	t.Parallel()
	got := render(t, formatMarkdown, &dataResult{Sections: []dataSection{{Key: "workouts", Value: []workoutOut{}}}})
	if !strings.Contains(got, "## Workouts\n\nNo data.") {
		t.Errorf("got:\n%s", got)
	}
}
