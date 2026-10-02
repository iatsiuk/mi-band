package fitness

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// wireFile wraps id and body with the trailing CRC32 like the band does
func wireFile(id ID, body []byte) []byte {
	b := append(id.Bytes(), body...)
	return binary.LittleEndian.AppendUint32(b, crc32.ChecksumIEEE(b))
}

func TestParseIDMatchesApp(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "id.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Bytes string `json:"bytes"`
		App   struct {
			TimeStamp   int64  `json:"time_stamp"`
			TZ          int    `json:"tz_in15_min"`
			Version     int    `json:"version"`
			DataType    uint8  `json:"data_type"`
			DailyType   uint8  `json:"daily_type"`
			SportType   uint8  `json:"sport_type"`
			FileType    uint8  `json:"file_type"`
			TZOffsetSec int    `json:"tz_offset_in_sec"`
			ValidityLen int    `json:"validity_len"`
			CRC32       string `json:"crc32"`
		} `json:"app"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		b, _ := hex.DecodeString(c.Bytes)
		id, err := ParseID(b)
		if err != nil {
			t.Fatal(err)
		}
		a := c.App
		typ := a.DailyType
		if a.DataType == DataSport {
			typ = a.SportType
		}
		version := int(int8(id.Version)) //nolint:gosec // the app reads the version as a signed byte
		if id.Time.Unix() != a.TimeStamp || id.TZOffsetMin*60 != a.TZOffsetSec || id.TZOffsetMin != a.TZ*15 ||
			version != a.Version || id.DataType != a.DataType || id.Type != typ || id.FileType != a.FileType {
			t.Errorf("%s: go %+v, app %+v", c.Bytes, id, a)
		}
		if got := validityLen(id); got != a.ValidityLen {
			t.Errorf("%s: validity len go %d, app %d", c.Bytes, got, a.ValidityLen)
		}
		if got := hex.EncodeToString(binary.LittleEndian.AppendUint32(nil, crc32.ChecksumIEEE(b))); got != a.CRC32 {
			t.Errorf("%s: crc32 go %s, app %s", c.Bytes, got, a.CRC32)
		}
		if !bytes.Equal(id.Bytes(), b) {
			t.Errorf("%s: re-encoded as %x", c.Bytes, id.Bytes())
		}
	}
}

func TestIDNamesAndKind(t *testing.T) {
	b, _ := hex.DecodeString("e2dfdd682009d9") // sport 22 report v9, tz +8h
	id, _ := ParseID(b)
	if id.Kind() != KindSportReport || id.TypeName() != "sport_outdoor_has_step" {
		t.Fatalf("kind %s name %s", id.Kind(), id.TypeName())
	}
	if s := id.String(); s != "sport_report/sport_outdoor_has_step v9 2025-10-02T10:13:54+08:00" {
		t.Fatalf("String() = %s", s)
	}
	if SportName(308) != "strength_training" || SportName(1) != "running_outdoor" {
		t.Fatalf("sport names %s %s", SportName(308), SportName(1))
	}
	night := ID{DataType: DataDaily, Type: DailyNightSleep}
	if night.Kind() != KindNightSleep || night.TypeName() != "night_sleep" {
		t.Fatalf("night: %s %s", night.Kind(), night.TypeName())
	}
}

func TestParseFileRejectsBadContainer(t *testing.T) {
	id := ID{DataType: DataDaily, Type: DailyActivity, FileType: FileReport, Version: 6}
	good := wireFile(id, make([]byte, 1+5+4))
	if _, err := ParseFile(good); err != nil {
		t.Fatalf("good file: %v", err)
	}
	bad := append([]byte{}, good...)
	bad[len(bad)-1] ^= 1
	if _, err := ParseFile(bad); !errors.Is(err, ErrCRC) {
		t.Fatalf("crc: %v", err)
	}
	if _, err := ParseFile(good[:10]); err == nil {
		t.Fatal("short file accepted")
	}
	// v6 daily report needs 1 reserved + 5 validity bytes
	if _, err := ParseFile(wireFile(id, []byte{0, 0xff})); err == nil || !strings.Contains(err.Error(), "header") {
		t.Fatalf("truncated header: %v", err)
	}
}

func TestUnsupportedFilesAreRaw(t *testing.T) {
	for _, id := range []ID{
		{DataType: DataDaily, Type: DailyActivity, FileType: FileRecord, Version: 9},   // unknown version
		{DataType: DataDaily, Type: DailyActivity, FileType: FileRecord, Version: 129}, // read as a negative version by the app
		{DataType: DataSport, Type: 26, FileType: FileReport, Version: 1},              // unknown sport
		{DataType: DataDaily, Type: 10, FileType: FileRecord, Version: 1},              // weight records are not decoded
		{DataType: DataSport, Type: 17, FileType: FileRecord, Version: 1},              // triathlon record
	} {
		body := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
		f, err := ParseFile(wireFile(id, body))
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if f.Raw == nil || f.Raw.Reason == "" || len(f.Raw.Payload) == 0 {
			t.Fatalf("%s: not raw: %+v", id, f)
		}
		if _, err := json.Marshal(f); err != nil {
			t.Fatal(err)
		}
	}
}

func TestParsedFileMarshalsToJSON(t *testing.T) {
	for _, c := range loadGolden(t, "sport_record")[:4] {
		b, _ := hex.DecodeString(c.File)
		f, err := ParseFile(b)
		if err != nil {
			t.Fatal(err)
		}
		out, err := json.Marshal(f)
		if err != nil {
			t.Fatal(err)
		}
		var back struct {
			Kind Kind `json:"kind"`
			ID   ID   `json:"id"`
		}
		if err := json.Unmarshal(out, &back); err != nil || back.Kind != f.Kind || !back.ID.Time.Equal(f.ID.Time) {
			t.Fatalf("round trip: %v %+v", err, back)
		}
	}
}
