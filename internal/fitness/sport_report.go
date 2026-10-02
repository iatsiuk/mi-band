package fitness

import (
	"fmt"
	"time"
)

// SportReport is a workout summary (sport file type 1). Fields holds every valid field by
// app name with raw values; the typed fields repeat the common ones.
type SportReport struct {
	ProtoSportType    int                `json:"proto_sport_type"`
	SportType         int                `json:"sport_type"` // FitnessSportType: field 65 when set, else the proto type
	SportName         string             `json:"sport_name"`
	StartTime         *time.Time         `json:"start_time,omitempty"`
	EndTime           *time.Time         `json:"end_time,omitempty"`
	DurationS         *int               `json:"duration_s,omitempty"`
	ValidDurationS    *int               `json:"valid_duration_s,omitempty"`
	DistanceM         *int               `json:"distance_m,omitempty"`
	CaloriesKcal      *int               `json:"calories_kcal,omitempty"`
	TotalCaloriesKcal *int               `json:"total_calories_kcal,omitempty"`
	Steps             *int               `json:"steps,omitempty"`
	AvgHR             *int               `json:"avg_hr,omitempty"`
	MaxHR             *int               `json:"max_hr,omitempty"`
	MinHR             *int               `json:"min_hr,omitempty"`
	AvgPaceSPerKm     *int               `json:"avg_pace_s_per_km,omitempty"`
	MaxPaceSPerKm     *int               `json:"max_pace_s_per_km,omitempty"`
	MinPaceSPerKm     *int               `json:"min_pace_s_per_km,omitempty"`
	AvgSpeedKmh       *float64           `json:"avg_speed_kmh,omitempty"`
	MaxSpeedKmh       *float64           `json:"max_speed_kmh,omitempty"`
	AvgCadence        *int               `json:"avg_cadence,omitempty"`
	MaxCadence        *int               `json:"max_cadence,omitempty"`
	Fields            map[string]float64 `json:"fields"`

	// triathlon sub reports
	Swim *SportReport `json:"swim,omitempty"`
	Bike *SportReport `json:"bike,omitempty"`
	Run  *SportReport `json:"run,omitempty"`
}

// reportParsers mirrors FitnessDataParser.getSportReportParserInstance
var reportParsers = map[int]string{
	1: "OutdoorSport", 2: "OutdoorSport", 4: "OutdoorSport", 5: "OutdoorSport", 3: "IndoorRun", 6: "OutdoorBiking",
	7: "IndoorBiking", 8: "FreeTraining", 9: "Swimming", 10: "Swimming", 11: "EllipticalMachine", 12: "Yoga",
	13: "RowingMachine", 14: "RopeSkipping", 15: "Hiking", 16: "HITraining", 17: "Triathlon", 18: "OrdinaryBall",
	19: "Basketball", 20: "Golf", 21: "Ski", 22: "OutdoorStep", 23: "OutdoorNoStep", 24: "RockClimbing", 25: "Diving",
}

func reportParser(sport int) string { return reportParsers[sport] }

func parseSportReport(id ID, valid, p []byte) (*SportReport, string, error) {
	name := reportParser(int(id.Type))
	if name == "" {
		return nil, "no report parser for this sport type", nil
	}
	r := &reader{b: p}
	rep, err := parseReport(int(id.Type), int(id.Version), name, valid, r)
	if err != nil || id.Type != 17 {
		return rep, "", err
	}
	// triathlon: swim, bike and run reports follow, each as u8 version, validity, payload; parsing stops at the first unsupported one
	v := int(id.Version)
	subs := []struct {
		sport uint8
		dst   **SportReport
	}{{10, &rep.Swim}, {6, &rep.Bike}, {1, &rep.Run}}
	if v >= 3 {
		subs[1].sport, subs[2].sport = 23, 22
	}
	for _, s := range subs {
		sv, err := r.u8()
		if err != nil {
			return nil, "", fmt.Errorf("triathlon sub report: %w", err)
		}
		l := validityLen(ID{DataType: DataSport, Type: s.sport, FileType: FileReport, Version: sv})
		if l < 0 {
			break
		}
		sValid, err := r.bytes(l)
		if err != nil {
			return nil, "", fmt.Errorf("triathlon sub report: %w", err)
		}
		if *s.dst, err = parseReport(int(s.sport), int(sv), reportParser(int(s.sport)), sValid, r); err != nil {
			return nil, "", err
		}
	}
	return rep, "", nil
}

func parseReport(sport, version int, parser string, valid []byte, r *reader) (*SportReport, error) {
	table := reportTables[parser]
	vm, err := parseValid1(table, version, valid)
	if err != nil {
		return nil, err
	}
	rec, err := parseRecord1(table, version, vm, r)
	if err != nil {
		return nil, err
	}
	rep := &SportReport{ProtoSportType: sport, SportType: sport, Fields: map[string]float64{}}
	for k, v := range rec {
		if !v.valid || v.bytes != nil {
			continue
		}
		name, ok := reportFieldNames[k]
		if !ok {
			name = fmt.Sprintf("field_%d", k)
		}
		rep.Fields[name] = v.num()
		rep.set(k, v)
	}
	rep.SportName = SportName(rep.SportType)
	return rep, nil
}

func (rep *SportReport) set(k int16, v value1) {
	ints := map[int16]**int{
		3: &rep.DurationS, 4: &rep.ValidDurationS, 5: &rep.DistanceM, 6: &rep.CaloriesKcal, 7: &rep.TotalCaloriesKcal,
		8: &rep.MaxPaceSPerKm, 9: &rep.MinPaceSPerKm, 10: &rep.AvgPaceSPerKm, 13: &rep.Steps, 14: &rep.MaxCadence,
		15: &rep.AvgCadence, 16: &rep.AvgHR, 17: &rep.MaxHR, 18: &rep.MinHR,
	}
	switch k {
	case 1:
		rep.StartTime = tp(v.i)
	case 2:
		rep.EndTime = tp(v.i)
	case 11:
		rep.AvgSpeedKmh = fp(float32(v.num()))
	case 12:
		rep.MaxSpeedKmh = fp(float32(v.num()))
	case 65:
		if x := v.int32(); x > 0 {
			rep.SportType = int(x)
		}
	default:
		if dst, ok := ints[k]; ok {
			*dst = ip(v.int32())
		}
	}
}
