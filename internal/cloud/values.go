package cloud

import (
	"bytes"
	"encoding/json"
	"time"

	"miband/internal/fitness"
)

// Record values are the app's DailyRecordItem subclasses serialized by Gson
// (DailyRecordDaoRepo.getRecordItemClazz maps key -> class). Wire structs keep the app's
// SerializedName keys; pointers are the app's nullable boxed fields.

// Step is a "steps" record (StepItem).
type Step struct {
	Time         time.Time `json:"time"`
	Steps        int       `json:"steps"`
	DistanceM    int       `json:"distance_m"`
	CaloriesKcal float64   `json:"calories_kcal"`
}

// Calories is a "calories" record (CaloriesItem).
type Calories struct {
	Time         time.Time `json:"time"`
	CaloriesKcal float64   `json:"calories_kcal"`
}

// HeartRate is a "heart_rate" or "single_heart_rate" record (HrItem).
type HeartRate struct {
	Time time.Time `json:"time"`
	BPM  int       `json:"bpm"`
	Type *int      `json:"type,omitempty"`
}

// SpO2 is a "spo2" or "single_spo2" record (Spo2Item).
type SpO2 struct {
	Time    time.Time `json:"time"`
	Percent int       `json:"percent"`
}

// Stress is a "stress" or "single_stress" record (StressItem).
type Stress struct {
	Time  time.Time `json:"time"`
	Value int       `json:"value"`
}

// Sleep is a "sleep" record (SleepSegmentReport, all-day sleep with HR/SpO2 summary) or a
// "watch_night_sleep" / "watch_daytime_sleep" record (DayNightSleepReport, with scores).
// Durations are as stored by the app (minutes in practice, not proven).
type Sleep struct {
	BedTime       time.Time `json:"bed_time"`
	WakeupTime    time.Time `json:"wakeup_time"`
	TZOffsetMin   int       `json:"tz_offset_min"`
	Duration      int       `json:"duration"`
	DeepDuration  *int      `json:"deep_duration,omitempty"`
	LightDuration *int      `json:"light_duration,omitempty"`
	REMDuration   *int      `json:"rem_duration,omitempty"`
	AwakeDuration *int      `json:"awake_duration,omitempty"`
	AwakeCount    *int      `json:"awake_count,omitempty"`

	// SleepSegmentReport only
	AvgHR      *int `json:"avg_hr,omitempty"`
	MaxHR      *int `json:"max_hr,omitempty"`
	MinHR      *int `json:"min_hr,omitempty"`
	AvgSpO2    *int `json:"avg_spo2,omitempty"`
	MaxSpO2    *int `json:"max_spo2,omitempty"`
	MinSpO2    *int `json:"min_spo2,omitempty"`
	AvgBreath  *int `json:"avg_breath,omitempty"`
	Efficiency *int `json:"efficiency,omitempty"`
	Version    *int `json:"version,omitempty"`

	// DayNightSleepReport only
	TotalScore    *int `json:"total_score,omitempty"`
	QualityScore  *int `json:"quality_score,omitempty"`
	DurationScore *int `json:"duration_score,omitempty"`
	RecoveryScore *int `json:"recovery_score,omitempty"`
	NervousScore  *int `json:"nervous_score,omitempty"`

	Stages []fitness.Stage `json:"stages"`
}

// Decode returns the typed value of known keys: *Step ("steps"), *Calories ("calories"),
// *HeartRate ("heart_rate", "single_heart_rate"), *SpO2 ("spo2", "single_spo2"),
// *Stress ("stress", "single_stress") and *Sleep ("sleep", "watch_night_sleep",
// "watch_daytime_sleep"). Other keys and empty values return nil, nil; use Value.
func (r *Record) Decode() (any, error) {
	if b := bytes.TrimSpace(r.Value); len(b) == 0 || b[0] == '"' || string(b) == "null" {
		return nil, nil
	}
	newWire, ok := valueTypes[r.Key]
	if !ok {
		return nil, nil
	}
	w := newWire()
	if err := json.Unmarshal(r.Value, w); err != nil {
		return nil, err
	}
	return w.value(), nil
}

type wireValue interface{ value() any }

var valueTypes = map[string]func() wireValue{
	"steps":               func() wireValue { return &wireStep{} },
	"calories":            func() wireValue { return &wireCalories{} },
	"heart_rate":          func() wireValue { return &wireHR{} },
	"single_heart_rate":   func() wireValue { return &wireHR{} },
	"spo2":                func() wireValue { return &wireSpO2{} },
	"single_spo2":         func() wireValue { return &wireSpO2{} },
	"stress":              func() wireValue { return &wireStress{} },
	"single_stress":       func() wireValue { return &wireStress{} },
	"sleep":               func() wireValue { return &wireSleepSegment{} },
	"watch_night_sleep":   func() wireValue { return &wireDayNightSleep{} },
	"watch_daytime_sleep": func() wireValue { return &wireDayNightSleep{} },
}

func unix(s int64) time.Time { return time.Unix(s, 0).UTC() }

type wireStep struct {
	Calories float64 `json:"calories"`
	Distance int     `json:"distance"`
	Steps    int     `json:"steps"`
	Time     int64   `json:"time"`
}

func (w *wireStep) value() any {
	return &Step{Time: unix(w.Time), Steps: w.Steps, DistanceM: w.Distance, CaloriesKcal: w.Calories}
}

type wireCalories struct {
	Calories float64 `json:"calories"`
	Time     int64   `json:"time"`
}

func (w *wireCalories) value() any { return &Calories{Time: unix(w.Time), CaloriesKcal: w.Calories} }

type wireHR struct {
	BPM  int   `json:"bpm"`
	Type *int  `json:"type,omitempty"`
	Time int64 `json:"time"`
}

func (w *wireHR) value() any { return &HeartRate{Time: unix(w.Time), BPM: w.BPM, Type: w.Type} }

type wireSpO2 struct {
	SpO2 int   `json:"spo2"`
	Time int64 `json:"time"`
}

func (w *wireSpO2) value() any { return &SpO2{Time: unix(w.Time), Percent: w.SpO2} }

type wireStress struct {
	Stress int   `json:"stress"`
	Time   int64 `json:"time"`
}

func (w *wireStress) value() any { return &Stress{Time: unix(w.Time), Value: w.Stress} }

// wireStage is SleepStateItem; Gson also accepts the alternate names "start" and "end".
type wireStage struct {
	EndTime   int64 `json:"end_time"`
	State     int   `json:"state"`
	StartTime int64 `json:"start_time"`
}

func (w *wireStage) UnmarshalJSON(b []byte) error {
	var v struct {
		EndTime   *int64 `json:"end_time"`
		End       *int64 `json:"end"`
		State     int    `json:"state"`
		StartTime *int64 `json:"start_time"`
		Start     *int64 `json:"start"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*w = wireStage{State: v.State, StartTime: firstSet(v.StartTime, v.Start), EndTime: firstSet(v.EndTime, v.End)}
	return nil
}

func firstSet(vs ...*int64) int64 {
	for _, v := range vs {
		if v != nil {
			return *v
		}
	}
	return 0
}

func stages(items []wireStage) []fitness.Stage {
	out := make([]fitness.Stage, 0, len(items))
	for _, it := range items {
		out = append(out, fitness.Stage{Start: unix(it.StartTime), End: unix(it.EndTime), State: it.State, Name: fitness.StageName(it.State)})
	}
	return out
}

// wireSleepSegment is SleepSegmentReport (key "sleep").
type wireSleepSegment struct {
	AvgBreath     *int        `json:"avg_breath,omitempty"`
	AvgHR         *int        `json:"avg_hr,omitempty"`
	AvgSpO2       *int        `json:"avg_spo2,omitempty"`
	BedTime       int64       `json:"bedtime"`
	DeepDuration  int         `json:"sleep_deep_duration"`
	LightDuration int         `json:"sleep_light_duration"`
	MaxHR         *int        `json:"max_hr,omitempty"`
	MaxSpO2       *int        `json:"max_spo2,omitempty"`
	MinHR         *int        `json:"min_hr,omitempty"`
	MinSpO2       *int        `json:"min_spo2,omitempty"`
	REMDuration   *int        `json:"sleep_rem_duration,omitempty"`
	Duration      int         `json:"duration"`
	Efficiency    *int        `json:"sleep_efficiency,omitempty"`
	Items         []wireStage `json:"items,omitempty"`
	TZIn15Min     int         `json:"timezone"`
	Version       *int        `json:"version,omitempty"`
	AwakeCount    *int        `json:"awake_count,omitempty"`
	AwakeDuration *int        `json:"sleep_awake_duration,omitempty"`
	WakeupTime    int64       `json:"wake_up_time"`
}

func (w *wireSleepSegment) value() any {
	return &Sleep{
		BedTime: unix(w.BedTime), WakeupTime: unix(w.WakeupTime), TZOffsetMin: w.TZIn15Min * 15,
		Duration: w.Duration, DeepDuration: &w.DeepDuration, LightDuration: &w.LightDuration,
		REMDuration: w.REMDuration, AwakeDuration: w.AwakeDuration, AwakeCount: w.AwakeCount,
		AvgHR: w.AvgHR, MaxHR: w.MaxHR, MinHR: w.MinHR, AvgSpO2: w.AvgSpO2, MaxSpO2: w.MaxSpO2,
		MinSpO2: w.MinSpO2, AvgBreath: w.AvgBreath, Efficiency: w.Efficiency, Version: w.Version,
		Stages: stages(w.Items),
	}
}

// wireDayNightSleep is DayNightSleepReport (keys "watch_night_sleep", "watch_daytime_sleep").
type wireDayNightSleep struct {
	AwakeCount    *int        `json:"awake_count,omitempty"`
	AwakeDuration *int        `json:"sleep_awake_duration,omitempty"`
	BedTime       int64       `json:"bedtime"`
	DeepDuration  *int        `json:"sleep_deep_duration,omitempty"`
	DurationScore *int        `json:"duration_score,omitempty"`
	LightDuration *int        `json:"sleep_light_duration,omitempty"`
	NervousScore  *int        `json:"nervous_score,omitempty"`
	QualityScore  *int        `json:"quality_score,omitempty"`
	RecoveryScore *int        `json:"recover_score,omitempty"`
	REMDuration   *int        `json:"sleep_rem_duration,omitempty"`
	Duration      int         `json:"duration"`
	Items         []wireStage `json:"items,omitempty"`
	Time          int64       `json:"date_time"`
	TotalScore    *int        `json:"total_score,omitempty"`
	TZIn15Min     int         `json:"timezone"`
	WakeupTime    int64       `json:"wake_up_time"`
}

func (w *wireDayNightSleep) value() any {
	return &Sleep{
		BedTime: unix(w.BedTime), WakeupTime: unix(w.WakeupTime), TZOffsetMin: w.TZIn15Min * 15,
		Duration: w.Duration, DeepDuration: w.DeepDuration, LightDuration: w.LightDuration,
		REMDuration: w.REMDuration, AwakeDuration: w.AwakeDuration, AwakeCount: w.AwakeCount,
		TotalScore: w.TotalScore, QualityScore: w.QualityScore, DurationScore: w.DurationScore,
		RecoveryScore: w.RecoveryScore, NervousScore: w.NervousScore, Stages: stages(w.Items),
	}
}

// WorkoutReport is the typed subset of SportBasicReport, the value of a sport record.
type WorkoutReport struct {
	// Time, TZOffsetMin and ProtoType are the FitnessDataId fields of the band's sport files.
	Time              time.Time `json:"time"`
	TZOffsetMin       int       `json:"tz_offset_min"`
	ProtoType         int       `json:"proto_type"`
	SportType         int       `json:"sport_type"`
	Version           int       `json:"version"`
	StartTime         time.Time `json:"start_time"`
	EndTime           time.Time `json:"end_time"`
	DurationS         int       `json:"duration_s"`
	ValidDurationS    *int      `json:"valid_duration_s,omitempty"`
	DistanceM         *int      `json:"distance_m,omitempty"`
	CaloriesKcal      *int      `json:"calories_kcal,omitempty"`
	TotalCaloriesKcal *int      `json:"total_calories_kcal,omitempty"`
	Steps             *int      `json:"steps,omitempty"`
	AvgHR             *int      `json:"avg_hr,omitempty"`
	MaxHR             *int      `json:"max_hr,omitempty"`
	MinHR             *int      `json:"min_hr,omitempty"`
	AvgPaceSPerKm     *int      `json:"avg_pace_s_per_km,omitempty"`
	MaxPaceSPerKm     *int      `json:"max_pace_s_per_km,omitempty"`
	AvgSpeedKmh       *float64  `json:"avg_speed_kmh,omitempty"`
	MaxSpeedKmh       *float64  `json:"max_speed_kmh,omitempty"`
	AvgCadence        *int      `json:"avg_cadence,omitempty"`
	AvgStride         *int      `json:"avg_stride,omitempty"`
	RiseHeight        *float64  `json:"rise_height,omitempty"`
	FallHeight        *float64  `json:"fall_height,omitempty"`
	TrainEffect       *float64  `json:"train_effect,omitempty"`
	TrainLoad         *int      `json:"train_load,omitempty"`
	VO2Max            *int      `json:"vo2_max,omitempty"`
	RecoverTime       *int      `json:"recover_time,omitempty"`
}

// wireSportReport is the modelled part of SportBasicReport; "timestamp" and "time_zone" are
// Gson alternates of "time" and "timezone".
type wireSportReport struct {
	AvgCadence    *int     `json:"avg_cadence,omitempty"`
	AvgHR         *int     `json:"avg_hrm,omitempty"`
	AvgPace       *int     `json:"avg_pace,omitempty"`
	AvgSpeed      *float64 `json:"avg_speed,omitempty"`
	AvgStride     *int     `json:"avg_stride,omitempty"`
	Calories      *int     `json:"calories,omitempty"`
	Distance      *int     `json:"distance,omitempty"`
	Duration      int      `json:"duration"`
	EndTime       int64    `json:"end_time"`
	FallHeight    *float64 `json:"fall_height,omitempty"`
	MaxHR         *int     `json:"max_hrm,omitempty"`
	MaxPace       *int     `json:"max_pace,omitempty"`
	MaxSpeed      *float64 `json:"max_speed,omitempty"`
	MinHR         *int     `json:"min_hrm,omitempty"`
	ProtoType     int      `json:"proto_type"`
	RecoverTime   *int     `json:"recover_time,omitempty"`
	RiseHeight    *float64 `json:"rise_height,omitempty"`
	SportType     int      `json:"sport_type"`
	StartTime     int64    `json:"start_time"`
	Steps         *int     `json:"steps,omitempty"`
	Time          int64    `json:"time"`
	TotalCal      *int     `json:"total_cal,omitempty"`
	TrainEffect   *float64 `json:"train_effect,omitempty"`
	TrainLoad     *int     `json:"train_load,omitempty"`
	TZIn15Min     int      `json:"timezone"`
	ValidDuration *int     `json:"valid_duration,omitempty"`
	Version       int      `json:"version"`
	VO2Max        *int     `json:"vo2_max,omitempty"`
}

func (w *wireSportReport) UnmarshalJSON(b []byte) error {
	type plain wireSportReport
	v := struct {
		*plain
		Timestamp *int64 `json:"timestamp"`
		TimeZone  *int   `json:"time_zone"`
	}{plain: (*plain)(w)}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	if v.Timestamp != nil && w.Time == 0 {
		w.Time = *v.Timestamp
	}
	if v.TimeZone != nil && w.TZIn15Min == 0 {
		w.TZIn15Min = *v.TimeZone
	}
	return nil
}

func (w *wireSportReport) report() *WorkoutReport {
	return &WorkoutReport{
		Time: unix(w.Time), TZOffsetMin: w.TZIn15Min * 15, ProtoType: w.ProtoType, SportType: w.SportType,
		Version: w.Version, StartTime: unix(w.StartTime), EndTime: unix(w.EndTime), DurationS: w.Duration,
		ValidDurationS: w.ValidDuration, DistanceM: w.Distance, CaloriesKcal: w.Calories,
		TotalCaloriesKcal: w.TotalCal, Steps: w.Steps, AvgHR: w.AvgHR, MaxHR: w.MaxHR, MinHR: w.MinHR,
		AvgPaceSPerKm: w.AvgPace, MaxPaceSPerKm: w.MaxPace, AvgSpeedKmh: w.AvgSpeed, MaxSpeedKmh: w.MaxSpeed,
		AvgCadence: w.AvgCadence, AvgStride: w.AvgStride, RiseHeight: w.RiseHeight, FallHeight: w.FallHeight,
		TrainEffect: w.TrainEffect, TrainLoad: w.TrainLoad, VO2Max: w.VO2Max, RecoverTime: w.RecoverTime,
	}
}
