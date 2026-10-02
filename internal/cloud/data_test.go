package cloud

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"testing"
	"time"

	"miband/internal/fitness"
)

// golden_data.json is produced by research/golden/cloud/run.sh (CloudDataGolden.java) from the
// Mi Fitness classes
type goldenData struct {
	FitnessByTime []struct {
		Key, Start, End string
		NextKey         string `json:"next_key"`
		Out             string
	} `json:"fitness_by_time"`
	SportByTime []struct {
		Start, End string
		NextKey    string `json:"next_key"`
		Out        string
	} `json:"sport_by_time"`
	FDSItem []struct {
		SID, Timestamp, TZ, Version, Type, Suffix, Request string
		DataType                                           string `json:"data_type"`
		FileType                                           string `json:"file_type"`
		IDHex                                              string `json:"id_hex"`
		ServerIDHex                                        string `json:"server_id_hex"`
		KeyHex                                             string `json:"key_hex"`
		ServerKey                                          string `json:"server_key"`
	} `json:"fds_item"`
	FDSPayload []struct {
		Body         string
		ObjKey       string `json:"obj_key"`
		PlainHex     string `json:"plain_hex"`
		DecryptedHex string `json:"decrypted_hex"`
	} `json:"fds_payload"`
	RoundTrip []struct {
		Class, In, Out string
	} `json:"round_trip"`
}

func loadGoldenData(t *testing.T) goldenData {
	t.Helper()
	raw, err := os.ReadFile("testdata/golden_data.json")
	if err != nil {
		t.Fatal(err)
	}
	var g goldenData
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	return g
}

func optInt64(s string) *int64 {
	if s == "null" {
		return nil
	}
	v, _ := strconv.ParseInt(s, 10, 64)
	return &v
}

func optString(s string) string {
	if s == "null" {
		return ""
	}
	return s
}

func atoi(t *testing.T, s string) int {
	t.Helper()
	v, err := strconv.Atoi(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func atou8(t *testing.T, s string) uint8 {
	t.Helper()
	v, err := strconv.ParseUint(s, 10, 8)
	if err != nil {
		t.Fatal(err)
	}
	return uint8(v)
}

func marshal(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDataRequestBodiesMatchApp(t *testing.T) {
	g := loadGoldenData(t)
	if len(g.FitnessByTime) == 0 || len(g.SportByTime) == 0 {
		t.Fatal("no golden request cases")
	}
	for _, c := range g.FitnessByTime {
		req := fitnessByTimeRequest{EndTime: optInt64(c.End), Key: optString(c.Key), NextKey: optString(c.NextKey), Reverse: true, StartTime: optInt64(c.Start)}
		if got := marshal(t, req); got != c.Out {
			t.Errorf("fitness request\n got %s\nwant %s", got, c.Out)
		}
	}
	for _, c := range g.SportByTime {
		req := sportByTimeRequest{EndTime: optInt64(c.End), Limit: sportPageSize, NextKey: optString(c.NextKey), Reverse: true, StartTime: optInt64(c.Start)}
		if got := marshal(t, req); got != c.Out {
			t.Errorf("sport request\n got %s\nwant %s", got, c.Out)
		}
	}
}

func goldenID(t *testing.T, ts, tz, version, dataType, typ, fileType string) fitness.ID {
	t.Helper()
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return fitness.ID{
		Time: time.Unix(sec, 0).UTC(), TZOffsetMin: atoi(t, tz) * 15, Version: atou8(t, version),
		DataType: atou8(t, dataType), Type: atou8(t, typ), FileType: atou8(t, fileType),
	}
}

func TestFDSObjectNameMatchesApp(t *testing.T) {
	g := loadGoldenData(t)
	if len(g.FDSItem) == 0 {
		t.Fatal("no golden fds items")
	}
	for _, c := range g.FDSItem {
		id := goldenID(t, c.Timestamp, c.TZ, c.Version, c.DataType, c.Type, c.FileType)
		if got := hex.EncodeToString(id.Bytes()); got != c.IDHex {
			t.Errorf("id bytes %s, want %s", got, c.IDHex)
		}
		if got := hex.EncodeToString(fdsKeyBytes(id)); got != c.KeyHex {
			t.Errorf("key bytes %s, want %s", got, c.KeyHex)
		}
		it := fdsItemFor(c.SID, id)
		if it.Suffix != c.Suffix || it.serverKey() != c.ServerKey {
			t.Errorf("fds item %+v (%s), want %s / %s", it, it.serverKey(), c.Suffix, c.ServerKey)
		}
		if got := marshal(t, fdsRequest{Items: []fdsItem{it}, DID: c.SID}); got != c.Request {
			t.Errorf("fds request\n got %s\nwant %s", got, c.Request)
		}
	}
}

func TestFDSPayloadDecryptsLikeApp(t *testing.T) {
	g := loadGoldenData(t)
	if len(g.FDSPayload) == 0 {
		t.Fatal("no golden payloads")
	}
	for _, c := range g.FDSPayload {
		if c.PlainHex != c.DecryptedHex {
			t.Fatalf("app round trip failed: %+v", c)
		}
		got, err := decryptFDS(c.Body, c.ObjKey)
		if err != nil {
			t.Fatalf("%s: %v", c.Body, err)
		}
		if hex.EncodeToString(got) != c.PlainHex {
			t.Errorf("decrypted %x, want %s", got, c.PlainHex)
		}
	}
	if _, err := decryptFDS(`"AAAA"`, g.FDSPayload[0].ObjKey); err == nil {
		t.Error("short ciphertext accepted")
	}
}

// the app's Gson round trip keeps exactly the keys its class knows; every key our wire structs
// produce must be among them with the same value
func TestRecordValueKeysMatchApp(t *testing.T) {
	wires := map[string]func() any{
		"StepItem":            func() any { return &wireStep{} },
		"CaloriesItem":        func() any { return &wireCalories{} },
		"HrItem":              func() any { return &wireHR{} },
		"Spo2Item":            func() any { return &wireSpO2{} },
		"StressItem":          func() any { return &wireStress{} },
		"SleepSegmentReport":  func() any { return &wireSleepSegment{} },
		"DayNightSleepReport": func() any { return &wireDayNightSleep{} },
		"SportBasicReport":    func() any { return &wireSportReport{} },
		"FitnessDataResultByTime": func() any {
			return &struct {
				DataList []wireRecord `json:"data_list"`
				HasMore  bool         `json:"has_more"`
				NextKey  string       `json:"next_key"`
			}{}
		},
		"SportReportResultByTime": func() any {
			return &struct {
				SportRecords []wireRecord `json:"sport_records"`
				HasMore      bool         `json:"has_more"`
				NextKey      string       `json:"next_key"`
			}{}
		},
		"FDSResultValue": func() any { return &fdsResult{} },
	}
	unmodelled := map[string]bool{"SleepSegmentReport.protoTime": true, "SleepSegmentReport.device_bedtime": true, "SleepSegmentReport.device_wake_up_time": true}
	g := loadGoldenData(t)
	seen := map[string]bool{}
	for _, c := range g.RoundTrip {
		mk, ok := wires[c.Class]
		if !ok {
			t.Errorf("no wire struct for %s", c.Class)
			continue
		}
		seen[c.Class] = true
		w := mk()
		if err := json.Unmarshal([]byte(c.In), w); err != nil {
			t.Fatalf("%s: %v", c.Class, err)
		}
		var ours, app map[string]any
		if err := json.Unmarshal([]byte(marshal(t, w)), &ours); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(c.Out), &app); err != nil {
			t.Fatal(err)
		}
		for k, v := range ours {
			if fmt.Sprint(v) != fmt.Sprint(app[k]) {
				t.Errorf("%s.%s: ours %v, app %v", c.Class, k, v, app[k])
			}
		}
		// fixtures only use modelled keys, so a missing key is a misspelled nullable tag
		for k := range app {
			if _, ok := ours[k]; !ok && !unmodelled[c.Class+"."+k] {
				t.Errorf("%s.%s: kept by the app, missing in ours", c.Class, k)
			}
		}
	}
	for c := range wires {
		if !seen[c] {
			t.Errorf("no golden round trip for %s", c)
		}
	}
}

func TestRecordDecodeTypedValues(t *testing.T) {
	ts := func(s int64) time.Time { return time.Unix(s, 0).UTC() }
	ip := func(v int) *int { return &v }
	cases := []struct {
		key, value string
		want       any
	}{
		{"steps", `{"time":1760000000,"steps":123,"distance":95,"calories":4.5}`, &Step{Time: ts(1760000000), Steps: 123, DistanceM: 95, CaloriesKcal: 4.5}},
		{"calories", `{"time":1760000060,"calories":12.25}`, &Calories{Time: ts(1760000060), CaloriesKcal: 12.25}},
		{"heart_rate", `{"time":1760000120,"bpm":72,"type":1}`, &HeartRate{Time: ts(1760000120), BPM: 72, Type: ip(1)}},
		{"single_spo2", `{"time":1760000180,"spo2":97}`, &SpO2{Time: ts(1760000180), Percent: 97}},
		{"stress", `{"time":1760000240,"stress":35}`, &Stress{Time: ts(1760000240), Value: 35}},
		{
			"sleep", `{"bedtime":1759970000,"wake_up_time":1759998800,"duration":420,"sleep_deep_duration":90,"sleep_light_duration":240,` +
				`"sleep_rem_duration":60,"sleep_awake_duration":30,"awake_count":2,"avg_hr":58,"timezone":32,` +
				`"items":[{"start_time":1759970000,"end_time":1759971800,"state":3},{"start":1759971800,"end":1759973600,"state":2}]}`,
			&Sleep{
				BedTime: ts(1759970000), WakeupTime: ts(1759998800), TZOffsetMin: 480, Duration: 420, DeepDuration: ip(90),
				LightDuration: ip(240), REMDuration: ip(60), AwakeDuration: ip(30), AwakeCount: ip(2), AvgHR: ip(58),
				Stages: []fitness.Stage{
					{Start: ts(1759970000), End: ts(1759971800), State: 3, Name: "light"},
					{Start: ts(1759971800), End: ts(1759973600), State: 2, Name: "deep"},
				},
			},
		},
		{
			"watch_night_sleep", `{"date_time":1759998800,"bedtime":1759970000,"wake_up_time":1759998800,"duration":420,"total_score":81,"timezone":-4,` +
				`"items":[{"start_time":1759970000,"end_time":1759971800,"state":4}]}`,
			&Sleep{
				BedTime: ts(1759970000), WakeupTime: ts(1759998800), TZOffsetMin: -60, Duration: 420, TotalScore: ip(81),
				Stages: []fitness.Stage{{Start: ts(1759970000), End: ts(1759971800), State: 4, Name: "rem"}},
			},
		},
		{"vo2_max", `{"time":1}`, nil},
	}
	for _, c := range cases {
		got, err := (&Record{Key: c.key, Value: json.RawMessage(c.value)}).Decode()
		if err != nil {
			t.Fatalf("%s: %v", c.key, err)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s:\n got %s\nwant %s", c.key, marshal(t, got), marshal(t, c.want))
		}
	}
	if _, err := (&Record{Key: "steps", Value: json.RawMessage(`[1]`)}).Decode(); err == nil {
		t.Error("bad steps value accepted")
	}
}

// dataServer adds the data endpoints to a fakeCloud; pages are keyed by next_key
type dataServer struct {
	f       *fakeCloud
	fitness map[string]string
	sport   map[string]string
	fds     string
	object  string
	queries map[string][]string // url path -> plaintext data params
}

func newDataServer(t *testing.T) (*dataServer, *Session) {
	f := newFakeCloud(t)
	d := &dataServer{f: f, queries: map[string][]string{}}
	f.extra = map[string]http.HandlerFunc{
		"/app/v1/data/get_fitness_data_by_time":  d.paged("/app/v1/data/get_fitness_data_by_time", func() map[string]string { return d.fitness }),
		"/app/v1/data/get_sport_records_by_time": d.paged("/app/v1/data/get_sport_records_by_time", func() map[string]string { return d.sport }),
		"/healthapp/service/gen_download_url": func(w http.ResponseWriter, r *http.Request) {
			data := d.verifyGet(r, "/service/gen_download_url")
			d.queries[r.URL.Path] = append(d.queries[r.URL.Path], data)
			f.respondEncrypted(w, r.URL.Query().Get("_nonce"), d.fds)
		},
		"/fds/object": func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Cookie") != "" || r.Header.Get("User-Agent") != "" {
				t.Errorf("fds download sends cookie %q, user agent %q", r.Header.Get("Cookie"), r.Header.Get("User-Agent"))
			}
			_, _ = fmt.Fprint(w, d.object)
		},
	}
	s, err := LoginWithPassToken(context.Background(), testUserID, testPassToken, f.opts())
	if err != nil {
		t.Fatal(err)
	}
	return d, s
}

// verifyGet checks a GET carries only the signed, encrypted params and returns the plaintext data
func (d *dataServer) verifyGet(r *http.Request, signPath string) string {
	t := d.f.t
	if r.Method != http.MethodGet {
		t.Errorf("%s: method %s", r.URL.Path, r.Method)
	}
	q := r.URL.Query()
	for k := range q {
		if k != "data" && k != "rc4_hash__" && k != "signature" && k != "_nonce" {
			t.Errorf("%s: unexpected query param %s", r.URL.Path, k)
		}
	}
	return d.f.verify(http.MethodGet, signPath, q)
}

func (d *dataServer) paged(path string, pages func() map[string]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := d.verifyGet(r, path)
		d.queries[path] = append(d.queries[path], data)
		var req struct {
			NextKey string `json:"next_key"`
		}
		if err := json.Unmarshal([]byte(data), &req); err != nil {
			d.f.t.Errorf("%s: data: %v", path, err)
		}
		page, ok := pages()[req.NextKey]
		if !ok {
			page = `{"code":-1,"message":"no such page"}`
		}
		d.f.respondEncrypted(w, r.URL.Query().Get("_nonce"), page)
	}
}

func valueString(v string) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestFitnessDataFollowsNextKey(t *testing.T) {
	d, s := newDataServer(t)
	steps := `{"time":1760000000,"steps":12,"distance":9,"calories":0.5}`
	d.fitness = map[string]string{
		"": `{"code":0,"result":{"data_list":[` +
			`{"sid":"940134049","key":"steps","time":1760000000,"value":` + valueString(steps) + `,"zone_offset":28800,"zone_name":"Asia/Singapore"},` +
			`{"sid":"940134049","key":"pai","time":1759968000,"value":"{\"daily_pai\":3}","zone_offset":28800}` +
			`],"next_key":"nk-1","has_more":true}}`,
		"nk-1": `{"code":0,"result":{"data_list":[{"sid":"940134049","key":"heart_rate","time":1759990000,"deleted":true}],"next_key":"nk-2","has_more":false}}`,
	}
	from, to := time.Unix(1760000000, 0), time.Unix(1760086400, 0)
	recs, err := s.FitnessData(context.Background(), from, to, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []Record{
		{DID: "940134049", Key: "steps", Time: time.Unix(1760000000, 0).UTC(), ZoneOffset: 28800, ZoneName: "Asia/Singapore", Value: json.RawMessage(steps)},
		{DID: "940134049", Key: "pai", Time: time.Unix(1759968000, 0).UTC(), ZoneOffset: 28800, Value: json.RawMessage(`{"daily_pai":3}`)},
		{DID: "940134049", Key: "heart_rate", Time: time.Unix(1759990000, 0).UTC(), Deleted: true},
	}
	if !reflect.DeepEqual(recs, want) {
		t.Fatalf("records:\n got %s\nwant %s", marshal(t, recs), marshal(t, want))
	}
	wantQ := []string{
		`{"end_time":1760086400,"reverse":true,"start_time":1760000000}`,
		`{"end_time":1760086400,"next_key":"nk-1","reverse":true,"start_time":1760000000}`,
	}
	if got := d.queries["/app/v1/data/get_fitness_data_by_time"]; !reflect.DeepEqual(got, wantQ) {
		t.Fatalf("queries:\n got %q\nwant %q", got, wantQ)
	}
	if v, err := recs[0].Decode(); err != nil || v.(*Step).Steps != 12 {
		t.Fatalf("decode: %v %v", v, err)
	}
}

func TestFitnessDataReportsAPIErrors(t *testing.T) {
	d, s := newDataServer(t)
	d.fitness = map[string]string{"": `{"code":-6,"msg":"param error"}`}
	var apiErr *APIError
	if _, err := s.FitnessData(context.Background(), time.Time{}, time.Unix(1760086400, 0), "steps"); !errors.As(err, &apiErr) || apiErr.Code != -6 {
		t.Fatalf("got %v, want APIError -6", err)
	}
	if got := d.queries["/app/v1/data/get_fitness_data_by_time"]; len(got) != 1 || got[0] != `{"end_time":1760086400,"key":"steps","reverse":true}` {
		t.Fatalf("query %q", got)
	}
}

func TestWorkoutsAndRawFile(t *testing.T) {
	d, s := newDataServer(t)
	report := `{"time":1759371234,"timezone":32,"proto_type":22,"sport_type":22,"version":2,"start_time":1759371234,` +
		`"end_time":1759374834,"duration":3600,"distance":5000,"calories":300,"steps":6000,"avg_hrm":120}`
	d.sport = map[string]string{
		"": `{"code":0,"result":{"sport_records":[{"sid":"940134049","key":"outdoor_walking","category":"walking","time":1759371234,` +
			`"value":` + valueString(report) + `,"zone_offset":28800}],"next_key":"nk-2","has_more":true}}`,
		"nk-2": `{"code":0,"result":{"sport_records":[],"has_more":false}}`,
	}
	ws, err := s.Workouts(context.Background(), time.Unix(1760000000, 0), time.Unix(1760086400, 0))
	if err != nil {
		t.Fatal(err)
	}
	g := loadGoldenData(t)
	wantQ := []string{g.SportByTime[0].Out, g.SportByTime[1].Out}
	if got := d.queries["/app/v1/data/get_sport_records_by_time"]; !reflect.DeepEqual(got, wantQ) {
		t.Fatalf("queries:\n got %q\nwant %q", got, wantQ)
	}
	if len(ws) != 1 || ws[0].Key != "outdoor_walking" || ws[0].Category != "walking" || ws[0].Report == nil {
		t.Fatalf("workouts: %s", marshal(t, ws))
	}
	r := ws[0].Report
	if r.ProtoType != 22 || r.TZOffsetMin != 480 || *r.DistanceM != 5000 || *r.AvgHR != 120 || r.DurationS != 3600 ||
		!r.StartTime.Equal(time.Unix(1759371234, 0)) {
		t.Fatalf("report: %s", marshal(t, r))
	}

	// recover-rate file of the band (golden vector of internal/fitness), stored in server form
	file, _ := hex.DecodeString("e2dfdd682001db00f2eddd6817570500968c82786e15bc2c92")
	devID, err := fitness.ParseID(file)
	if err != nil {
		t.Fatal(err)
	}
	server := append(append(append([]byte{}, file[:6]...), devID.Type), file[7:]...)
	id, ok := ws[0].FileID(fitness.FileRecoverRate)
	if !ok || id.Type != devID.Type || !id.Time.Equal(devID.Time) || id.TZOffsetMin != devID.TZOffsetMin {
		t.Fatalf("file id %v, device id %v", id, devID)
	}
	objKey := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 16))
	d.object = encryptFDSForTest(t, server, objKey)
	it := fdsItemFor("940134049", id)
	d.fds = fmt.Sprintf(`{"code":0,"result":{%q:{"url":%q,"obj_name":"x","obj_key":%q,"method":"GET","expires_time":1}}}`,
		it.serverKey(), d.f.srv.URL+"/fds/object?sig=1", objKey)

	raw, err := s.RawFile(context.Background(), ws[0].DID, id)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, server) {
		t.Fatalf("raw file %x, want %x", raw, server)
	}
	if got := d.queries["/healthapp/service/gen_download_url"]; len(got) != 1 || got[0] != marshal(t, fdsRequest{Items: []fdsItem{it}, DID: "940134049"}) {
		t.Fatalf("fds query %q", got)
	}
	parsed, err := ParseRawFile(id, raw)
	if err != nil {
		t.Fatal(err)
	}
	direct, err := fitness.ParseBody(devID, file[7:])
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Kind != fitness.KindRecoverRate || parsed.Raw != nil || !reflect.DeepEqual(parsed, direct) {
		t.Fatalf("parsed %s\nwant %s", marshal(t, parsed), marshal(t, direct))
	}

	other := id
	other.Type++
	if _, err := ParseRawFile(other, raw); err == nil {
		t.Error("mismatched id accepted")
	}
	d.fds = `{"code":0,"result":{}}`
	if _, err := s.RawFile(context.Background(), ws[0].DID, id); !errors.Is(err, ErrNoRawFile) {
		t.Fatalf("got %v, want ErrNoRawFile", err)
	}
}

func TestWorkoutWithoutFilesHasNoFileID(t *testing.T) {
	if _, ok := (&Workout{Report: &WorkoutReport{Version: 0}}).FileID(fitness.FileGPS); ok {
		t.Error("version 0 report has a file id")
	}
	if _, ok := (&Workout{}).FileID(fitness.FileGPS); ok {
		t.Error("workout without report has a file id")
	}
}

// encryptFDSForTest builds an object body the way FitnessFDSUploader.upload does
func encryptFDSForTest(t *testing.T, raw []byte, objKey string) string {
	t.Helper()
	key, err := base64.RawURLEncoding.DecodeString(objKey)
	if err != nil {
		t.Fatal(err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	n := aes.BlockSize - len(raw)%aes.BlockSize
	ct := append(append([]byte{}, raw...), bytes.Repeat([]byte{byte(n)}, n)...)
	cipher.NewCBCEncrypter(block, fdsIV).CryptBlocks(ct, ct)
	return valueString(base64.RawURLEncoding.EncodeToString(ct))
}
