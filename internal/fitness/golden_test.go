package fitness

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// golden vectors: testdata/*.json is written by research/golden/fitness/GoldenFitness.java, which
// builds synthetic files and decodes them with the Mi Fitness parser classes (FitnessDataParser.parse)

type goldenCase struct {
	Name string          `json:"name"`
	File string          `json:"file"`
	App  json.RawMessage `json:"app"`
}

func loadGolden(t *testing.T, group string) []goldenCase {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", group+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []goldenCase
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatalf("%s: no cases", group)
	}
	return cases
}

func decodeAny(t *testing.T, b []byte) any {
	t.Helper()
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func toAny(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return decodeAny(t, b)
}

// opts describe how app (Java) names and values map to the Go JSON model
type opts struct {
	rename   map[string]string // app key -> go key
	ignore   map[string]bool   // app keys without a Go counterpart
	extra    map[string]bool   // go keys without an app counterpart
	defaults map[string]string // app keys whose primitive default stands for "absent"
	shift    int64             // seconds the app added to times (sport records)
}

func set(keys ...string) map[string]bool {
	m := map[string]bool{}
	for _, k := range keys {
		m[k] = true
	}
	return m
}

func num(v any) (float64, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	f, err := strconv.ParseFloat(string(n), 64)
	return f, err == nil
}

func numEqual(a, b float64) bool {
	if a == b {
		return true
	}
	// float32 values printed by Java vs float64(float32) printed by Go
	if a != math.Trunc(a) || b != math.Trunc(b) {
		return float32(a) == float32(b)
	}
	return false
}

func compare(path string, a, g any, o *opts) []string {
	switch av := a.(type) {
	case map[string]any:
		gm, ok := g.(map[string]any)
		if !ok {
			return []string{fmt.Sprintf("%s: app object, go %T", path, g)}
		}
		var diffs []string
		seen := map[string]bool{}
		for k, x := range av {
			if o.ignore[k] {
				continue
			}
			gk := k
			if r, ok := o.rename[k]; ok {
				gk = r
			}
			seen[gk] = true
			y, ok := gm[gk]
			if !ok {
				if d, has := o.defaults[k]; has && fmt.Sprint(x) == d {
					continue
				}
				diffs = append(diffs, fmt.Sprintf("%s.%s: missing in go (app %v)", path, gk, x))
				continue
			}
			diffs = append(diffs, compare(path+"."+gk, x, y, o)...)
		}
		for k, y := range gm {
			if !seen[k] && !o.extra[k] {
				diffs = append(diffs, fmt.Sprintf("%s.%s: extra in go (%v)", path, k, y))
			}
		}
		return diffs
	case []any:
		gl, ok := g.([]any)
		if !ok && len(av) == 0 && g == nil {
			return nil
		}
		if !ok || len(gl) != len(av) {
			return []string{fmt.Sprintf("%s: app list of %d, go %v", path, len(av), lenOf(g))}
		}
		var diffs []string
		for i := range av {
			diffs = append(diffs, compare(fmt.Sprintf("%s[%d]", path, i), av[i], gl[i], o)...)
			if len(diffs) > 10 {
				break
			}
		}
		return diffs
	case json.Number:
		x, _ := num(av)
		if s, ok := g.(string); ok {
			tm, err := time.Parse(time.RFC3339, s)
			if err != nil || float64(tm.Unix()+o.shift) != x {
				return []string{fmt.Sprintf("%s: app %v, go time %s", path, x, s)}
			}
			return nil
		}
		y, ok := num(g)
		if !ok || !numEqual(x, y) {
			return []string{fmt.Sprintf("%s: app %v, go %v", path, av, g)}
		}
		return nil
	case string:
		if gs, ok := g.(string); !ok || !strings.EqualFold(av, gs) {
			return []string{fmt.Sprintf("%s: app %q, go %v", path, av, g)}
		}
		return nil
	default:
		if a != g {
			return []string{fmt.Sprintf("%s: app %v, go %v", path, a, g)}
		}
		return nil
	}
}

func lenOf(v any) string {
	if l, ok := v.([]any); ok {
		return fmt.Sprintf("list of %d", len(l))
	}
	return fmt.Sprintf("%T", v)
}

func check(t *testing.T, a, g any, o *opts) {
	t.Helper()
	if o.rename == nil {
		o.rename = map[string]string{}
	}
	diffs := compare("", a, toAny(t, g), o)
	sort.Strings(diffs)
	if len(diffs) > 10 {
		diffs = diffs[:10]
	}
	for _, d := range diffs {
		t.Error(d)
	}
}

var (
	minuteOpts = opts{rename: map[string]string{"is_abnormal_hr": "abnormal_hr"}}
	reportOpts = opts{
		rename: map[string]string{
			"hr": "current_hr", "spo2": "current_spo2", "calories": "calories_kcal", "total_cal": "total_calories_kcal",
			"valid_stand": "valid_stand_hours", "total7days_vitality": "total_7days_vitality",
		},
		ignore: set("time", "zone_offset_in_sec"),
	}
	sleepOpts = opts{
		rename: map[string]string{
			"sleep_summary": "summary", "sleep_advice": "advice", "sleep_duration": "duration", "wake_up_time": "wakeup_time",
			"sleep_items": "stages", "start_time": "start", "end_time": "end", "sleep_state": "state",
		},
		ignore:   set("tz_in15_min"),
		extra:    set("name"),
		defaults: map[string]string{"sleep_duration": "0", "bed_time": "0", "wake_up_time": "0"},
	}
	allDayOpts = opts{
		rename: map[string]string{
			"device_bed_time": "bed_time", "device_wakeup_time": "wakeup_time", "sleep_quality": "quality",
			"sleep_efficiency": "efficiency", "lin_bed_duration": "in_bed_duration", "is_sleep_finish": "finished",
			"hr_info": "hr", "spo2_info": "spo2", "snore_decibel_info": "snore_db", "sleep_src_bytes": "sleep_source",
			"first_record_time": "start", "sample_unit": "interval_s",
		},
		ignore: set("assist_info_bytes", "sleep_src_data_len", "count", "avg_value", "max_value", "min_value", "sum_value"),
	}
	measureOpts = opts{
		rename: map[string]string{
			"skin_temp": "skin_temp_c", "body_temp": "body_temp_c", "high_pressure": "systolic", "low_pressure": "diastolic",
			"measurement_status": "status",
		},
		extra: set("type_name"),
	}
	gpsOpts = opts{
		rename: map[string]string{
			"time_stamp": "time", "longitude": "lon", "latitude": "lat", "gps_source": "source", "altitude": "altitude_m",
		},
	}
	recoverOpts = opts{rename: map[string]string{
		"recover_time": "time", "recover_rate": "rate", "recover_rate_radio": "ratio", "recover_rate_list": "samples",
	}}
	coreOpts = opts{
		rename: map[string]string{
			"duration": "duration_s", "valid_duration": "valid_duration_s", "distance": "distance_m", "calories": "calories_kcal",
			"total_cal": "total_calories_kcal", "avg_pace": "avg_pace_s_per_km", "max_pace": "max_pace_s_per_km",
			"min_pace": "min_pace_s_per_km", "avg_speed": "avg_speed_kmh", "max_speed": "max_speed_kmh", "proto_type": "proto_sport_type",
		},
		extra:    set("fields", "sport_name"),
		defaults: map[string]string{"start_time": "0", "end_time": "0", "duration": "0"},
	}
	segmentOpts = opts{
		rename: map[string]string{
			"start_time": "start", "end_time": "end", "record_count": "count", "is_sport_pause": "pause", "i_t_state": "it_state",
			"i_t_total_count": "it_total_count", "i_t_total_distance": "it_total_distance", "record_list": "samples",
			"duration": "duration_s", "distance": "distance_m", "height_change": "height_change_m", "is_whole_km_point": "whole_km",
		},
		// the app keeps the running height on the segment; samples carry the per-second height
		ignore:   set("height"),
		extra:    set("init_height_m", "height_m"),
		defaults: map[string]string{"is_sport_pause": "true"},
	}
)

func TestGolden(t *testing.T) {
	for _, group := range []string{"daily_record", "daily_report", "sleep", "measure", "gps", "recover_rate", "sport_report", "sport_record", "misc"} {
		for _, c := range loadGolden(t, group) {
			t.Run(group+"/"+c.Name, func(t *testing.T) { checkGolden(t, c) })
		}
	}
}

func checkGolden(t *testing.T, c goldenCase) {
	file, err := hex.DecodeString(c.File)
	if err != nil {
		t.Fatal(err)
	}
	f, err := ParseFile(file)
	app, _ := decodeAny(t, c.App).(map[string]any)
	switch {
	case app == nil:
		// the app returns no result: unsupported or unusable file
		if err != nil || f.Raw == nil {
			t.Fatalf("app: no result; go: err=%v raw=%v", err, f != nil && f.Raw != nil)
		}
		return
	case app["error"] != nil:
		if err == nil {
			t.Fatalf("app failed (%v), go decoded %+v", app["error"], f)
		}
		return
	case err != nil:
		t.Fatalf("go: %v", err)
	case app["data_invalid_version"] == true:
		if f.Raw == nil {
			t.Fatalf("app: invalid version; go decoded kind %s", f.Kind)
		}
		return
	}
	if f.Raw != nil {
		if strings.Contains(c.Name, "group layout") && f.Raw.Reason != "" {
			return // grouped rowing/rope/HIIT records are not decoded (known gap)
		}
		if g, ok := app["gps_record"].(map[string]any); ok && len(g) == 0 {
			return // app returns an empty track map, go reports it raw
		}
		t.Fatalf("go returned raw (%s)", f.Raw.Reason)
	}
	switch f.Kind {
	case KindDailyRecord:
		check(t, app["daily_record_list"], f.DailyRecord, &minuteOpts)
	case KindDailyReport:
		a := app["daily_report"].(map[string]any)
		if vs, ok := a["valid_stand"].(string); ok {
			b, _ := hex.DecodeString(vs)
			a["valid_stand"] = toAny(t, standHours(b))
		}
		check(t, a, f.DailyReport, &reportOpts)
	case KindNightSleep:
		check(t, app["night_sleep_report"], f.Sleep, &sleepOpts)
	case KindNap:
		check(t, app["daytime_sleep_report"], f.Sleep, &sleepOpts)
	case KindAllDaySleep:
		a := app["all_day_sleep_report"].(map[string]any)
		if _, ok := a["sleep_src_bytes"]; !ok {
			a["sleep_src_bytes"] = ""
		}
		check(t, a, f.AllDaySleep, &allDayOpts)
	case KindManualMeasure:
		list := app["measure_data_list"].([]any)
		for _, x := range list {
			m := x.(map[string]any)
			if v, ok := m["value"].(map[string]any); ok {
				delete(m, "value")
				for k, y := range v {
					m[k] = y
				}
			}
		}
		check(t, list, f.Measurements, &measureOpts)
	case KindGPS:
		tracks := map[string]any{}
		for _, tr := range f.GPS.Tracks {
			tracks[strconv.Itoa(tr.SportType)] = tr.Points
		}
		a := map[string]any{"tracks": app["gps_record"]}
		g := map[string]any{"tracks": tracks}
		if tgc, ok := app["gps_tgc_data"]; ok {
			a["tgc"] = tgc
		}
		if f.GPS.TGC != nil {
			g["tgc"] = f.GPS.TGC
		}
		check(t, a, g, &gpsOpts)
	case KindRecoverRate:
		check(t, app["recover_rate_data"], f.RecoverRate, &recoverOpts)
	case KindSportReport:
		a := app["sport_report"].(map[string]any)
		check(t, a["core"], f.SportReport, &coreOpts)
		checkReportFields(t, a["fields"].(map[string]any), f.SportReport.Fields)
	case KindSportRecord:
		segs := app["sport_record"].(map[string]any)[strconv.Itoa(int(f.ID.Type))]
		o := segmentOpts
		o.shift = int64(f.ID.TZOffsetMin) * 60
		check(t, segs, f.SportRecord, &o)
	default:
		t.Fatalf("unexpected kind %s", f.Kind)
	}
}

// checkReportFields compares the app engine output (id -> [valid, value]) with the valid fields of the report
func checkReportFields(t *testing.T, app map[string]any, fields map[string]float64) {
	t.Helper()
	valid := 0
	for k, x := range app {
		pair := x.([]any)
		if pair[0] != true {
			continue
		}
		valid++
		id, _ := strconv.ParseInt(k, 10, 16)
		name := reportFieldNames[int16(id)]
		want, _ := num(pair[1])
		got, ok := fields[name]
		if !ok || !numEqual(want, got) {
			t.Errorf("field %d %s: app %v, go %v (present %v)", id, name, want, got, ok)
		}
	}
	if valid != len(fields) {
		t.Errorf("app has %d valid fields, go %d", valid, len(fields))
	}
}
