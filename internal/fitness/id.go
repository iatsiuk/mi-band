// Package fitness decodes fitness files downloaded from Xiaomi bands (Mi Fitness format).
package fitness

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

// IDLen is the size of an encoded fitness data id.
const IDLen = 7

// data types (bit 7 of the type byte)
const (
	DataDaily = 0
	DataSport = 1
)

// file types (bits 1..0 of the type byte)
const (
	FileRecord      = 0
	FileReport      = 1
	FileGPS         = 2
	FileRecoverRate = 3
)

// daily types
const (
	DailyActivity      = 0
	DailyNap           = 2
	DailyNightSleep    = 3
	DailyProfile       = 5
	DailyManualMeasure = 6
	DailyAllDaySleep   = 8
)

var dailyNames = map[uint8]string{
	0: "daily_activity", 1: "distribute", 2: "day_sleep", 3: "night_sleep", 4: "average", 5: "profile",
	6: "manual_measure", 7: "sleep_lifeq", 8: "sleep_mi_dev", 9: "abnormal_record", 10: "weight_record",
	11: "ecg_measure", 31: "ecg",
}

// ID is the 7-byte FitnessDataId that prefixes every file. Daily files are re-sent with updated
// content under the same id, so an id alone does not identify file content.
type ID struct {
	Time        time.Time `json:"time"`
	TZOffsetMin int       `json:"tz_offset_min"`
	Version     uint8     `json:"version"`
	DataType    uint8     `json:"data_type"`
	Type        uint8     `json:"type"` // daily type or proto sport type
	FileType    uint8     `json:"file_type"`
}

// ParseID decodes the first 7 bytes of b.
func ParseID(b []byte) (ID, error) {
	if len(b) < IDLen {
		return ID{}, errors.New("fitness: id shorter than 7 bytes")
	}
	t := b[6]
	return ID{
		Time:        time.Unix(int64(binary.LittleEndian.Uint32(b)), 0).UTC(),
		TZOffsetMin: int(int8(b[4])) * 15, //nolint:gosec // signed quarter hours
		Version:     b[5],
		DataType:    t >> 7,
		Type:        (t & 0x7f) >> 2,
		FileType:    t & 3,
	}, nil
}

// Bytes encodes the id back to its 7-byte form.
func (id ID) Bytes() []byte {
	b := make([]byte, IDLen)
	binary.LittleEndian.PutUint32(b, uint32(id.Time.Unix())) //nolint:gosec // u32 unix seconds on the wire
	b[4] = byte(int8(id.TZOffsetMin / 15))                   //nolint:gosec // signed quarter hours
	b[5] = id.Version
	b[6] = id.DataType<<7 | (id.Type&0x1f)<<2 | id.FileType&3
	return b
}

// IsSport reports whether the id describes a workout file.
func (id ID) IsSport() bool { return id.DataType == DataSport }

// TZ returns the device time zone of the file.
func (id ID) TZ() *time.Location {
	return time.FixedZone("", id.TZOffsetMin*60)
}

// TypeName returns the daily type or proto sport type name.
func (id ID) TypeName() string {
	if id.IsSport() {
		if n, ok := protoSportNames[int(id.Type)]; ok {
			return n
		}
	} else if n, ok := dailyNames[id.Type]; ok {
		return n
	}
	return fmt.Sprintf("type_%d", id.Type)
}

// Kind returns the payload kind the file decodes to.
func (id ID) Kind() Kind {
	if id.IsSport() {
		return id.sportKind()
	}
	if id.Type == DailyActivity {
		switch id.FileType {
		case FileRecord:
			return KindDailyRecord
		case FileReport:
			return KindDailyReport
		}
		return KindUnknown
	}
	if k, ok := dailyKinds[id.Type]; ok {
		return k
	}
	return KindUnknown
}

var dailyKinds = map[uint8]Kind{
	DailyNap: KindNap, DailyNightSleep: KindNightSleep, DailyManualMeasure: KindManualMeasure, DailyAllDaySleep: KindAllDaySleep,
}

func (id ID) sportKind() Kind {
	switch id.FileType {
	case FileRecord:
		return KindSportRecord
	case FileReport:
		return KindSportReport
	case FileGPS:
		return KindGPS
	}
	return KindRecoverRate
}

func (id ID) String() string {
	return fmt.Sprintf("%s/%s v%d %s", id.Kind(), id.TypeName(), id.Version, id.Time.In(id.TZ()).Format(time.RFC3339))
}

// SportName returns the name of a FitnessSportType value (the "hundred" sport type of reports).
func SportName(t int) string {
	if n, ok := sportNames[t]; ok {
		return n
	}
	return fmt.Sprintf("sport_%d", t)
}
