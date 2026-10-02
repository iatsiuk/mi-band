package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"time"

	"miband/internal/fitness"
)

// FitnessApiService (@BaseUrl app/v1/, @Secret pathPrefix ""): GET with the encrypted "data" query
var (
	fitnessByTime = endpoint{path: "/app/v1/data/get_fitness_data_by_time"}
	sportByTime   = endpoint{path: "/app/v1/data/get_sport_records_by_time"}
)

const (
	// GetSportReportByTime(start, end, nextKey) default limit
	sportPageSize = 50
	maxDataPages  = 1000
)

// ErrTooManyPages means the server kept returning another page; the results so far are returned with it.
var ErrTooManyPages = errors.New("xiaomi health cloud: too many result pages")

// ErrIncompletePages means the server repeated a paging cursor while still reporting more pages.
var ErrIncompletePages = errors.New("xiaomi health cloud: paging cursor repeated, results incomplete")

// Record is one health cloud fitness record (FitnessDataModel): a band sample or report as
// stored by Mi Fitness, identified by device, key and time.
type Record struct {
	DID        string    `json:"did"`
	Key        string    `json:"key"`
	Time       time.Time `json:"time"`
	ZoneOffset int       `json:"zone_offset"` // seconds east of UTC
	ZoneName   string    `json:"zone_name,omitempty"`
	Deleted    bool      `json:"deleted,omitempty"`
	// Value is the payload object the app stored as a JSON string (DailyRecordItem subclass).
	Value json.RawMessage `json:"value,omitempty"`
}

type wireRecord struct {
	Category   string  `json:"category,omitempty"`
	Deleted    *bool   `json:"deleted"`
	Key        string  `json:"key"`
	SID        string  `json:"sid"`
	Time       int64   `json:"time"`
	Value      *string `json:"value"`
	ZoneName   string  `json:"zone_name"`
	ZoneOffset *int    `json:"zone_offset"`
}

func (w *wireRecord) record() Record {
	r := Record{DID: w.SID, Key: w.Key, Time: time.Unix(w.Time, 0).UTC(), ZoneName: w.ZoneName}
	if w.ZoneOffset != nil {
		r.ZoneOffset = *w.ZoneOffset
	}
	if w.Deleted != nil {
		r.Deleted = *w.Deleted
	}
	if w.Value != nil && *w.Value != "" {
		if json.Valid([]byte(*w.Value)) {
			r.Value = json.RawMessage(*w.Value)
		} else {
			r.Value, _ = json.Marshal(*w.Value)
		}
	}
	return r
}

// fitnessByTimeRequest is GetFitnessDataByTime; fields in the order Gson writes them.
type fitnessByTimeRequest struct {
	EndTime   *int64 `json:"end_time,omitempty"`
	Key       string `json:"key,omitempty"`
	NextKey   string `json:"next_key,omitempty"`
	Reverse   bool   `json:"reverse"`
	StartTime *int64 `json:"start_time,omitempty"`
}

// sportByTimeRequest is GetSportReportByTime; category is never set by the sync code.
type sportByTimeRequest struct {
	EndTime   *int64 `json:"end_time,omitempty"`
	Limit     int    `json:"limit"`
	NextKey   string `json:"next_key,omitempty"`
	Reverse   bool   `json:"reverse"`
	StartTime *int64 `json:"start_time,omitempty"`
}

// unixOrNil mirrors CloudSyncBaseRequest: times <= 0 are sent as null (omitted)
func unixOrNil(t time.Time) *int64 {
	if t.IsZero() || t.Unix() <= 0 {
		return nil
	}
	u := t.Unix()
	return &u
}

// FitnessData returns the fitness records between from and to (unix seconds on the wire; a zero
// time leaves that bound open). key selects one data type ("steps", "sleep", "heart_rate", ...);
// empty returns every type like the app's yearly pull. Pages are followed via next_key while
// has_more is set (CloudSyncBaseRequest.pullAndSaveFitnessDataByTime). Records come newest first.
func (s *Session) FitnessData(ctx context.Context, from, to time.Time, key string) ([]Record, error) {
	req := fitnessByTimeRequest{EndTime: unixOrNil(to), Key: key, Reverse: true, StartTime: unixOrNil(from)}
	var out []Record
	seen := map[string]bool{"": true}
	for range maxDataPages {
		var res struct {
			DataList []wireRecord `json:"data_list"`
			HasMore  bool         `json:"has_more"`
			NextKey  string       `json:"next_key"`
		}
		if err := s.call(ctx, http.MethodGet, fitnessByTime, req, &res); err != nil {
			return out, err
		}
		for _, w := range res.DataList {
			out = append(out, w.record())
		}
		// like the app, an empty next_key ends the pull
		if !res.HasMore || res.NextKey == "" {
			return out, nil
		}
		if seen[res.NextKey] {
			return out, ErrIncompletePages
		}
		seen[res.NextKey] = true
		req.NextKey = res.NextKey
	}
	return out, ErrTooManyPages
}

// Workout is one sport record (SportReportModel). Its value is a SportBasicReport, the app's JSON
// form of the band's sport report file; the detail files are stored raw (see RawFile).
type Workout struct {
	DID        string    `json:"did"`
	Key        string    `json:"key"`      // sport name, e.g. "outdoor_running"
	Category   string    `json:"category"` // e.g. "running"
	Time       time.Time `json:"time"`     // record key time (report start time)
	ZoneOffset int       `json:"zone_offset"`
	ZoneName   string    `json:"zone_name,omitempty"`
	Deleted    bool      `json:"deleted,omitempty"`
	// Report is nil when the value is missing or not an object.
	Report *WorkoutReport  `json:"report,omitempty"`
	Value  json.RawMessage `json:"value,omitempty"`
}

// Workouts returns the sport records between from and to (zero time = open bound), following
// next_key pages of 50 like CloudSyncBaseRequest.pullAndSaveSportReportByTime.
func (s *Session) Workouts(ctx context.Context, from, to time.Time) ([]Workout, error) {
	req := sportByTimeRequest{EndTime: unixOrNil(to), Limit: sportPageSize, Reverse: true, StartTime: unixOrNil(from)}
	var out []Workout
	seen := map[string]bool{"": true}
	for range maxDataPages {
		var res struct {
			SportRecords []wireRecord `json:"sport_records"`
			HasMore      bool         `json:"has_more"`
			NextKey      string       `json:"next_key"`
		}
		if err := s.call(ctx, http.MethodGet, sportByTime, req, &res); err != nil {
			return out, err
		}
		for _, w := range res.SportRecords {
			r := w.record()
			wo := Workout{
				DID: r.DID, Key: r.Key, Category: w.Category, Time: r.Time, ZoneOffset: r.ZoneOffset,
				ZoneName: r.ZoneName, Deleted: r.Deleted, Value: r.Value,
			}
			var rep wireSportReport
			if isObject(r.Value) && json.Unmarshal(r.Value, &rep) == nil {
				wo.Report = rep.report()
			}
			out = append(out, wo)
		}
		if !res.HasMore || res.NextKey == "" {
			return out, nil
		}
		if seen[res.NextKey] {
			return out, ErrIncompletePages
		}
		seen[res.NextKey] = true
		req.NextKey = res.NextKey
	}
	return out, ErrTooManyPages
}

// FileID returns the id of a band file of this workout (fitness.FileRecord, FileGPS,
// FileRecoverRate), built like FitnessFDSDataGetter: report time, timezone and proto sport type.
// Version is unknown until the file is downloaded (ParseRawFile fills it in). ok is false when
// the report has version <= 0, for which the app assumes no detail files, or a sport type
// outside the one-byte id field.
func (w *Workout) FileID(fileType uint8) (id fitness.ID, ok bool) {
	if w.Report == nil || w.Report.Version <= 0 || w.Report.ProtoType < 0 || w.Report.ProtoType > math.MaxUint8 {
		return fitness.ID{}, false
	}
	return fitness.ID{
		Time:        w.Report.Time,
		TZOffsetMin: w.Report.TZOffsetMin,
		DataType:    fitness.DataSport,
		Type:        uint8(w.Report.ProtoType), //nolint:gosec // range checked above
		FileType:    fileType,
	}, true
}

// isObject reports whether v is a JSON object; Gson stores absent values as null or plain strings.
func isObject(v json.RawMessage) bool {
	b := bytes.TrimSpace(v)
	return len(b) > 0 && b[0] == '{'
}
