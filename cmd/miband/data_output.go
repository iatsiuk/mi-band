package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"miband/internal/cloud"
)

// dataResult is the output of miband data; sections keep the order of --kind.
type dataResult struct {
	From, To time.Time
	Sections []dataSection
	Partial  bool
}

type dataSection struct {
	Key   string
	Value any
}

// decodedRecord is a record with a typed value; rec gives markdown the record time and zone.
type decodedRecord struct {
	DID   string `json:"did"`
	Value any    `json:"value"`
	rec   *cloud.Record
}

func (r *dataResult) MarshalJSON() ([]byte, error) {
	m := map[string]any{"from": r.From.UTC(), "to": r.To.UTC()}
	for _, s := range r.Sections {
		m[s.Key] = s.Value
	}
	if r.Partial {
		m["partial"] = true
	}
	return json.Marshal(m)
}

func (r *dataResult) markdown(b *strings.Builder) {
	b.WriteString("# Xiaomi cloud data\n\n")
	fmt.Fprintf(b, "Period: %s - %s\n", r.From.Format("2006-01-02 15:04 MST"), r.To.Format("2006-01-02 15:04 MST"))
	for _, s := range r.Sections {
		var t *table
		switch v := s.Value.(type) {
		case []daySteps:
			t = stepsTable(v)
		case []workoutOut:
			t = workoutsTable(v)
		case []cloud.Record:
			t = rawTable(v)
		case []any:
			t = recordsTable(v)
		default:
			continue
		}
		heading(b, sectionTitle(s.Key))
		t.write(b)
	}
	if r.Partial {
		b.WriteString("\nNote: some detail files failed to download, see the Files column.\n")
	}
}

// sectionTitle turns a record key like "heart_rate" into "Heart rate".
func sectionTitle(key string) string {
	t := strings.ReplaceAll(key, "_", " ")
	if t == "" {
		return t
	}
	return strings.ToUpper(t[:1]) + t[1:]
}

func stepsTable(days []daySteps) *table {
	t := &table{header: []string{"Device", "Date", "Steps", "Distance, m", "Calories, kcal"}, right: map[int]bool{2: true, 3: true, 4: true}}
	for _, d := range days {
		t.add(d.DID, d.Date, fmt.Sprint(d.Steps), fmt.Sprint(d.DistanceM), fmt.Sprintf("%.1f", d.CaloriesKcal))
	}
	return t
}

func workoutsTable(ws []workoutOut) *table {
	t := &table{
		header: []string{"Device", "Start", "Sport", "Duration", "Distance, m", "Calories, kcal", "Avg HR", "Max HR"},
		right:  map[int]bool{3: true, 4: true, 5: true, 6: true, 7: true},
	}
	details := false
	for i := range ws {
		details = details || ws[i].Files != nil || ws[i].Unavailable != nil
	}
	if details {
		t.header = append(t.header, "Files")
	}
	for i := range ws {
		w := &ws[i]
		start, rep := w.Time, w.Report
		if rep == nil {
			rep = &cloud.WorkoutReport{}
		} else if !rep.StartTime.IsZero() {
			start = rep.StartTime
		}
		row := []string{
			w.DID, localTime(start, w.ZoneOffset), w.Key, duration(rep.DurationS),
			optInt(rep.DistanceM), optInt(rep.CaloriesKcal), optInt(rep.AvgHR), optInt(rep.MaxHR),
		}
		if details {
			row = append(row, workoutFilesCell(w))
		}
		t.add(row...)
	}
	return t
}

// workoutFilesCell names the detail files; their decoded content is printed with --format json.
func workoutFilesCell(w *workoutOut) string {
	var parts []string
	if got := slices.Sorted(maps.Keys(w.Files)); len(got) > 0 {
		parts = append(parts, strings.Join(got, ", "))
	}
	if len(w.Unavailable) > 0 {
		parts = append(parts, "missing: "+strings.Join(w.Unavailable, ", "))
	}
	failed := make([]string, 0, len(w.FileErrors))
	for name, msg := range w.FileErrors {
		failed = append(failed, name+" ("+msg+")")
	}
	slices.Sort(failed)
	if len(failed) > 0 {
		parts = append(parts, "failed: "+strings.Join(failed, ", "))
	}
	return strings.Join(parts, "; ")
}

func rawTable(recs []cloud.Record) *table {
	t := &table{header: []string{"Device", "Time", "Deleted", "Value"}}
	for i := range recs {
		r := &recs[i]
		deleted := ""
		if r.Deleted {
			deleted = "yes"
		}
		t.add(r.DID, localTime(r.Time, r.ZoneOffset), deleted, code(r.Value))
	}
	return t
}

func recordsTable(vals []any) *table {
	t := &table{header: []string{"Device", "Time", "Value"}}
	for _, v := range vals {
		switch r := v.(type) {
		case decodedRecord:
			t.add(r.DID, localTime(r.rec.Time, r.rec.ZoneOffset), valueSummary(r.Value, r.rec.ZoneOffset))
		case cloud.Record:
			t.add(r.DID, localTime(r.Time, r.ZoneOffset), code(r.Value))
		}
	}
	return t
}

func valueSummary(v any, zoneOffset int) string {
	switch v := v.(type) {
	case *cloud.Step:
		return fmt.Sprintf("%d steps, %d m, %.1f kcal", v.Steps, v.DistanceM, v.CaloriesKcal)
	case *cloud.Calories:
		return fmt.Sprintf("%.1f kcal", v.CaloriesKcal)
	case *cloud.HeartRate:
		return fmt.Sprintf("%d bpm", v.BPM)
	case *cloud.SpO2:
		return fmt.Sprintf("%d%%", v.Percent)
	case *cloud.Stress:
		return fmt.Sprintf("stress %d", v.Value)
	case *cloud.Sleep:
		return sleepSummary(v, zoneOffset)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err.Error()
	}
	return code(b)
}

func sleepSummary(s *cloud.Sleep, zoneOffset int) string {
	zone := time.FixedZone("", zoneOffset)
	out := fmt.Sprintf("%s-%s, %d min", s.BedTime.In(zone).Format("15:04"), s.WakeupTime.In(zone).Format("15:04"), s.Duration)
	var stages []string
	for _, st := range []struct {
		name string
		v    *int
	}{{"deep", s.DeepDuration}, {"light", s.LightDuration}, {"REM", s.REMDuration}, {"awake", s.AwakeDuration}} {
		if st.v != nil {
			stages = append(stages, fmt.Sprintf("%s %d", st.name, *st.v))
		}
	}
	if len(stages) > 0 {
		out += " (" + strings.Join(stages, ", ") + ")"
	}
	if s.TotalScore != nil {
		out += fmt.Sprintf(", score %d", *s.TotalScore)
	}
	return out
}
