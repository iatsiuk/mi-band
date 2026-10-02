package fitness

import (
	"strconv"
	"time"
)

// Minute is one per-minute daily activity sample (DailyRecordParser). Nil fields were not valid.
// Up to 1440 minutes are decoded from the file start; the app drops minutes past the local day end when storing.
// Distance and calories units are not confirmed by the app (likely m and kcal).
type Minute struct {
	Time             time.Time `json:"time"`
	Steps            *int      `json:"steps,omitempty"`
	Calories         *int      `json:"calories,omitempty"`
	Distance         *int      `json:"distance,omitempty"`
	HR               *int      `json:"hr,omitempty"`
	SpO2             *int      `json:"spo2,omitempty"`
	Stress           *int      `json:"stress,omitempty"`
	ActivityType     *int      `json:"activity_type,omitempty"` // 0 none, 1 stop, 2 step, 3 running
	ActivityHILevel  *int      `json:"activity_hi_level,omitempty"`
	SportType        *int      `json:"sport_type,omitempty"`
	Energy           *int      `json:"energy,omitempty"`
	TotalCal         *int      `json:"total_cal,omitempty"`
	EnergyState      *int      `json:"energy_state,omitempty"`
	EnergyStateValue *int      `json:"energy_state_value,omitempty"`
	AbnormalHR       *bool     `json:"abnormal_hr,omitempty"`
	HRPreAbnormal    *int      `json:"hr_pre_abnormal,omitempty"`
	LightValue       *int      `json:"light_value,omitempty"`
	BodyMomentum     *int      `json:"body_momentum,omitempty"`
}

func ip(v int32) *int {
	i := int(v)
	return &i
}

func fp(v float32) *float64 {
	f := f64(v)
	return &f
}

// f64 widens a float32 to the float64 with the same shortest decimal form (92.2, not 92.19999694824219)
func f64(v float32) float64 {
	f, _ := strconv.ParseFloat(strconv.FormatFloat(float64(v), 'g', -1, 32), 64)
	return f
}

func tp(unix int64) *time.Time {
	t := time.Unix(unix, 0).UTC()
	return &t
}

// at most one day of minutes is decoded (DateTimeHelper.sDayInMinutes)
const dayMinutes = 1440

func parseDailyRecord(id ID, valid, p []byte) ([]Minute, error) {
	v := int(id.Version)
	groups := dailyRecordV2
	if v == 1 {
		groups = dailyRecordV1
	}
	samples, err := loop4(groups, parseValid4(groups, v, valid), &reader{b: p}, dayMinutes)
	if err != nil {
		return nil, err
	}
	start := id.Time.Unix() / 60 * 60
	out := make([]Minute, len(samples))
	for i, s := range samples {
		m := &out[i]
		m.Time = time.Unix(start+int64(60*i), 0).UTC()
		for k, raw := range s {
			m.set(k, intValue(raw), v)
		}
	}
	return out, nil
}

func (m *Minute) set(k int16, x int32, version int) {
	ints := map[int16]**int{
		2: &m.Steps, 3: &m.ActivityType, 4: &m.Calories, 6: &m.SportType, 7: &m.Distance, 8: &m.HR, 9: &m.Energy,
		11: &m.TotalCal, 12: &m.EnergyState, 14: &m.SpO2, 15: &m.Stress, 16: &m.HRPreAbnormal, 17: &m.LightValue,
		18: &m.BodyMomentum,
	}
	if version >= 3 {
		ints[5] = &m.ActivityHILevel
	}
	if dst, ok := ints[k]; ok {
		*dst = ip(x)
		return
	}
	switch k {
	case 1:
		b := x > 0
		m.AbnormalHR = &b
	case 13:
		// sign-magnitude with bit 7 set meaning positive
		mag := x & 0x7f
		if x&0x80 == 0 {
			mag = -mag
		}
		m.EnergyStateValue = ip(mag)
	}
}

// DailyReport is the day summary (DailyReportParser). Nil fields were not valid.
type DailyReport struct {
	Steps                      *int       `json:"steps,omitempty"`
	CaloriesKcal               *int       `json:"calories_kcal,omitempty"`
	TotalCaloriesKcal          *int       `json:"total_calories_kcal,omitempty"`
	CurrentHR                  *int       `json:"current_hr,omitempty"`
	RestHR                     *int       `json:"rest_hr,omitempty"`
	MaxHR                      *int       `json:"max_hr,omitempty"`
	MaxHRTime                  *time.Time `json:"max_hr_time,omitempty"`
	MinHR                      *int       `json:"min_hr,omitempty"`
	MinHRTime                  *time.Time `json:"min_hr_time,omitempty"`
	AvgHR                      *int       `json:"avg_hr,omitempty"`
	AvgStress                  *int       `json:"avg_stress,omitempty"`
	MaxStress                  *int       `json:"max_stress,omitempty"`
	MinStress                  *int       `json:"min_stress,omitempty"`
	ValidStandHours            []int      `json:"valid_stand_hours,omitempty"` // local hours of the day with a valid stand
	RecoveryTime               *int       `json:"recovery_time,omitempty"`
	CurrentSpO2                *int       `json:"current_spo2,omitempty"`
	MaxSpO2                    *int       `json:"max_spo2,omitempty"`
	MaxSpO2Time                *time.Time `json:"max_spo2_time,omitempty"`
	MinSpO2                    *int       `json:"min_spo2,omitempty"`
	MinSpO2Time                *time.Time `json:"min_spo2_time,omitempty"`
	AvgSpO2                    *int       `json:"avg_spo2,omitempty"`
	DayTrainingLoad            *int       `json:"day_training_load,omitempty"`
	WeekTrainingLoad           *int       `json:"week_training_load,omitempty"`
	TrainingLoadLevel          *int       `json:"training_load_level,omitempty"`
	DayLowIntensityVitality    *int       `json:"day_low_intensity_vitality,omitempty"`
	DayMediumIntensityVitality *int       `json:"day_medium_intensity_vitality,omitempty"`
	DayHighIntensityVitality   *int       `json:"day_high_intensity_vitality,omitempty"`
	Total7DaysVitality         *int       `json:"total_7days_vitality,omitempty"`
	SuggestActivityType        *int       `json:"suggest_activity_type,omitempty"`
	SuggestActivityDuration    *int       `json:"suggest_activity_duration,omitempty"`
	PhysicalFitnessStatus      *int       `json:"physical_fitness_status,omitempty"`
	PhysicalFitnessLevel       *int       `json:"physical_fitness_level,omitempty"`
	FatigueLevel               *int       `json:"fatigue_level,omitempty"`
	PhysicalStatusStress       *int       `json:"physical_status_stress,omitempty"`
}

func parseDailyReport(version int, valid, p []byte) (*DailyReport, error) {
	vm, err := parseValid1(dailyReportFields, version, valid)
	if err != nil {
		return nil, err
	}
	rec, err := parseRecord1(dailyReportFields, version, vm, &reader{b: p})
	if err != nil || len(rec) == 0 {
		return nil, err
	}
	d := &DailyReport{}
	ints := map[int16]**int{
		0: &d.Steps, 1: &d.CaloriesKcal, 2: &d.CurrentHR, 3: &d.RestHR, 4: &d.MaxHR, 6: &d.MinHR, 8: &d.AvgHR,
		9: &d.AvgStress, 10: &d.MaxStress, 11: &d.MinStress, 13: &d.TotalCaloriesKcal, 14: &d.RecoveryTime,
		15: &d.CurrentSpO2, 16: &d.MaxSpO2, 18: &d.MinSpO2, 20: &d.AvgSpO2, 21: &d.DayTrainingLoad,
		22: &d.WeekTrainingLoad, 23: &d.TrainingLoadLevel, 24: &d.DayLowIntensityVitality,
		25: &d.DayMediumIntensityVitality, 26: &d.DayHighIntensityVitality, 27: &d.Total7DaysVitality,
		28: &d.SuggestActivityType, 29: &d.SuggestActivityDuration, 30: &d.PhysicalFitnessStatus,
		31: &d.PhysicalFitnessLevel, 32: &d.FatigueLevel, 33: &d.PhysicalStatusStress,
	}
	times := map[int16]**time.Time{5: &d.MaxHRTime, 7: &d.MinHRTime, 17: &d.MaxSpO2Time, 19: &d.MinSpO2Time}
	for k, v := range rec {
		if !v.valid {
			continue
		}
		if dst, ok := ints[k]; ok {
			*dst = ip(v.int32())
		} else if dst, ok := times[k]; ok {
			*dst = tp(v.i)
		} else if k == 12 {
			d.ValidStandHours = standHours(v.bytes)
		}
	}
	return d, nil
}

// standHours expands the stand bitmap: bit k (LSB first) of byte j is hour 8*j+k
func standHours(b []byte) []int {
	hours := []int{}
	for j, x := range b {
		for k := range 8 {
			if x&(1<<k) != 0 {
				hours = append(hours, 8*j+k)
			}
		}
	}
	return hours
}
