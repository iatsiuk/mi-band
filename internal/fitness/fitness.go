package fitness

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
)

// Kind names the decoded payload of a file.
type Kind string

const (
	KindDailyRecord   Kind = "daily_record"
	KindDailyReport   Kind = "daily_report"
	KindNap           Kind = "nap"
	KindNightSleep    Kind = "night_sleep"
	KindAllDaySleep   Kind = "all_day_sleep"
	KindManualMeasure Kind = "manual_measure"
	KindSportRecord   Kind = "sport_record"
	KindSportReport   Kind = "sport_report"
	KindGPS           Kind = "gps"
	KindRecoverRate   Kind = "recover_rate"
	KindUnknown       Kind = "unknown"
)

// File is a decoded fitness file. Exactly one payload field is set; Raw is set when the
// type or version is not decoded.
type File struct {
	ID   ID   `json:"id"`
	Kind Kind `json:"kind"`

	DailyRecord  []Minute      `json:"daily_record,omitempty"`
	DailyReport  *DailyReport  `json:"daily_report,omitempty"`
	Sleep        *Sleep        `json:"sleep,omitempty"`
	AllDaySleep  *AllDaySleep  `json:"all_day_sleep,omitempty"`
	Measurements []Measurement `json:"measurements,omitempty"`
	SportReport  *SportReport  `json:"sport_report,omitempty"`
	SportRecord  []Segment     `json:"sport_record,omitempty"`
	GPS          *GPS          `json:"gps,omitempty"`
	RecoverRate  *RecoverRate  `json:"recover_rate,omitempty"`
	Raw          *Raw          `json:"raw,omitempty"`
}

// Raw holds the undecoded body of a file.
type Raw struct {
	Reason   string `json:"reason"`
	Validity Hex    `json:"validity,omitempty"`
	Payload  Hex    `json:"payload"`
}

// Hex is a byte slice that marshals to a hex string.
type Hex []byte

func (h Hex) MarshalJSON() ([]byte, error) { return json.Marshal(hex.EncodeToString(h)) }

func (h *Hex) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := hex.DecodeString(s)
	*h = v
	return err
}

// ErrCRC is returned when the trailing CRC32 of a file does not match.
var ErrCRC = errors.New("fitness: crc32 mismatch")

// ParseFile decodes an assembled file: 7-byte id, body, CRC32 (LE, IEEE over id and body).
func ParseFile(b []byte) (*File, error) {
	if len(b) < IDLen+1+4 {
		return nil, fmt.Errorf("fitness: file too short (%d bytes)", len(b))
	}
	n := len(b) - 4
	if crc32.ChecksumIEEE(b[:n]) != binary.LittleEndian.Uint32(b[n:]) {
		return nil, ErrCRC
	}
	id, err := ParseID(b)
	if err != nil {
		return nil, err
	}
	return ParseBody(id, b[IDLen:n])
}

// ParseBody decodes a file body (everything between the id and the CRC32).
func ParseBody(id ID, body []byte) (*File, error) {
	f := &File{ID: id, Kind: id.Kind()}
	l := validityLen(id)
	if l < 0 {
		f.Raw = &Raw{Reason: "unsupported type or version", Payload: body}
		return f, nil
	}
	if len(body) < 1+l {
		return nil, fmt.Errorf("fitness: %s: body shorter than its %d-byte header", id, 1+l)
	}
	valid, p := body[1:1+l], body[1+l:]
	reason, err := decode(f, valid, p)
	if err != nil {
		return nil, fmt.Errorf("fitness: %s: %w", id, err)
	}
	if reason != "" {
		f.Raw = &Raw{Reason: reason, Validity: valid, Payload: p}
	}
	return f, nil
}

// decoder fills one payload kind of f; a non-empty reason means the payload is left raw
type decoder func(f *File, valid, p []byte) (reason string, err error)

var decoders = map[Kind]decoder{
	KindDailyRecord: func(f *File, valid, p []byte) (reason string, err error) {
		f.DailyRecord, err = parseDailyRecord(f.ID, valid, p)
		return "", err
	},
	KindDailyReport: func(f *File, valid, p []byte) (reason string, err error) {
		f.DailyReport, err = parseDailyReport(int(f.ID.Version), valid, p)
		if err == nil && f.DailyReport == nil {
			reason = "empty daily report"
		}
		return reason, err
	},
	KindNightSleep: func(f *File, valid, p []byte) (reason string, err error) {
		f.Sleep, err = parseSleep(int(f.ID.Version), valid, p, true)
		return "", err
	},
	KindNap: func(f *File, valid, p []byte) (reason string, err error) {
		if f.ID.Version == 1 {
			return "day sleep version 1 is not parsed by the app", nil
		}
		f.Sleep, err = parseSleep(int(f.ID.Version), valid, p, false)
		return "", err
	},
	KindAllDaySleep: func(f *File, valid, p []byte) (reason string, err error) {
		f.AllDaySleep, err = parseAllDaySleep(int(f.ID.Version), valid, p)
		return "", err
	},
	KindManualMeasure: func(f *File, _, p []byte) (reason string, err error) {
		f.Measurements, err = parseMeasurements(int(f.ID.Version), p)
		return "", err
	},
	KindSportReport: func(f *File, valid, p []byte) (reason string, err error) {
		f.SportReport, reason, err = parseSportReport(f.ID, valid, p)
		return reason, err
	},
	KindSportRecord: func(f *File, valid, p []byte) (reason string, err error) {
		f.SportRecord, reason, err = parseSportRecord(f.ID, valid, p)
		return reason, err
	},
	KindGPS: func(f *File, valid, p []byte) (reason string, err error) {
		f.GPS, err = parseGPS(f.ID, valid, p)
		if err == nil && len(f.GPS.Tracks) == 0 {
			f.GPS, reason = nil, "no valid gps points"
		}
		return reason, err
	},
	KindRecoverRate: func(f *File, _, p []byte) (reason string, err error) {
		f.RecoverRate, err = parseRecoverRate(int(f.ID.Version), p)
		return "", err
	},
}

// decode fills the payload of f; a non-empty reason means the payload is left raw
func decode(f *File, valid, p []byte) (reason string, err error) {
	d, ok := decoders[f.Kind]
	if !ok {
		return "no parser for this type", nil
	}
	return d(f, valid, p)
}

type validityRange struct {
	dataType, sub, fileType, from, to, length int
}

// validityLen returns the number of validity bytes after the reserved byte, -1 if unsupported
func validityLen(id ID) int {
	// the app reads the version as a signed byte, so versions >= 128 are never supported
	v := int(id.Version)
	for _, r := range validityRanges {
		if r.dataType == int(id.DataType) && r.sub == int(id.Type) && r.fileType == int(id.FileType) && v >= r.from && v <= r.to {
			return r.length
		}
	}
	return -1
}
