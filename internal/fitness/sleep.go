package fitness

import "time"

// Sleep is a night sleep (daily type 3) or nap (daily type 2) file. Durations are as stored
// (likely minutes); only Duration and Stages are present for naps.
type Sleep struct {
	TotalScore         *int       `json:"total_score,omitempty"`
	QualityScore       *int       `json:"quality_score,omitempty"`
	DurationScore      *int       `json:"duration_score,omitempty"`
	Summary            *int       `json:"summary,omitempty"`
	Advice             *int       `json:"advice,omitempty"`
	Duration           *int       `json:"duration,omitempty"`
	BedTime            *time.Time `json:"bed_time,omitempty"`
	WakeupTime         *time.Time `json:"wakeup_time,omitempty"`
	RecoveryScore      *int       `json:"recovery_score,omitempty"`
	NervousScore       *int       `json:"nervous_score,omitempty"`
	AwakeCount         *int       `json:"awake_count,omitempty"`
	DeepDuration       *int       `json:"deep_duration,omitempty"`
	LightDuration      *int       `json:"light_duration,omitempty"`
	REMDuration        *int       `json:"rem_duration,omitempty"`
	AwakeDuration      *int       `json:"awake_duration,omitempty"`
	FriendlyTotalScore *int       `json:"friendly_total_score,omitempty"`
	Stages             []Stage    `json:"stages"`
}

// Stage is the interval [Start, End) spent in one sleep state.
type Stage struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	State int       `json:"state"`
	Name  string    `json:"name"`
}

var stageNames = map[int]string{0: "not_sleep", 2: "deep", 3: "light", 4: "rem", 5: "awake", 6: "sleep_trace"}

// StageName returns the name of a SleepState value.
func StageName(state int) string {
	if n, ok := stageNames[state]; ok {
		return n
	}
	return "unknown"
}

func parseSleep(version int, valid, p []byte, night bool) (*Sleep, error) {
	header, stageFields := napHeader, napStages
	if night {
		header, stageFields = nightSleepHeader, nightSleepStages
	}
	vm, err := parseValid1(header, version, valid)
	if err != nil {
		return nil, err
	}
	r := &reader{b: p}
	rec, err := parseRecord1(header, version, vm, r)
	if err != nil {
		return nil, err
	}
	s := &Sleep{Stages: []Stage{}}
	for k, v := range rec {
		if v.valid {
			s.set(k, v, night)
		}
	}
	all := map[int16]bool{stageFields[0].id: true, stageFields[1].id: true}
	recs, err := loop1(stageFields, version, all, r, int64(len(p)))
	if err != nil {
		return nil, err
	}
	// each record starts a stage that ends at the next record's time; the last one only closes the previous stage
	for i := 1; i < len(recs); i++ {
		prev, cur := recs[i-1], recs[i]
		state := int(prev[stageFields[1].id].int32())
		s.Stages = append(s.Stages, Stage{
			Start: time.Unix(prev[stageFields[0].id].i, 0).UTC(),
			End:   time.Unix(cur[stageFields[0].id].i, 0).UTC(),
			State: state,
			Name:  StageName(state),
		})
	}
	return s, nil
}

func (s *Sleep) set(k int16, v value1, night bool) {
	if !night {
		if k == 0 {
			s.Duration = ip(v.int32())
		}
		return
	}
	switch k {
	case 6:
		s.BedTime = tp(v.i)
	case 7:
		s.WakeupTime = tp(v.i)
	default:
		dst := map[int16]**int{
			0: &s.TotalScore, 1: &s.QualityScore, 2: &s.DurationScore, 3: &s.Summary, 4: &s.Advice, 5: &s.Duration,
			8: &s.RecoveryScore, 9: &s.NervousScore, 10: &s.AwakeCount, 11: &s.DeepDuration, 12: &s.LightDuration,
			13: &s.REMDuration, 14: &s.AwakeDuration, 15: &s.FriendlyTotalScore,
		}[k]
		if dst != nil {
			*dst = ip(v.int32())
		}
	}
}

// AllDaySleep is a Mi device sleep file (daily type 8). Stages are computed by a native
// library in the app from SleepSource and are not decoded here.
type AllDaySleep struct {
	Finished           bool       `json:"finished"`
	BedTime            *time.Time `json:"bed_time,omitempty"`
	WakeupTime         *time.Time `json:"wakeup_time,omitempty"`
	Quality            *int       `json:"quality,omitempty"`
	Efficiency         *int       `json:"efficiency,omitempty"`
	EntrySleepDuration *int64     `json:"entry_sleep_duration,omitempty"`
	InBedDuration      *int64     `json:"in_bed_duration,omitempty"`
	GoBedTime          *time.Time `json:"go_bed_time,omitempty"`
	LeaveBedTime       *time.Time `json:"leave_bed_time,omitempty"`
	HR                 *Series    `json:"hr,omitempty"`
	SpO2               *Series    `json:"spo2,omitempty"`
	SnoreDB            *Series    `json:"snore_db,omitempty"`
	SleepSource        Hex        `json:"sleep_source"`
}

// Series holds samples taken every IntervalS seconds from Start.
type Series struct {
	Start     time.Time `json:"start"`
	IntervalS int       `json:"interval_s"`
	Values    []float64 `json:"values"`
}

func parseAllDaySleep(version int, valid, p []byte) (*AllDaySleep, error) {
	vm, err := parseValid1(allDaySleepHeader, version, valid)
	if err != nil {
		return nil, err
	}
	r := &reader{b: p}
	rec, err := parseRecord1(allDaySleepHeader, version, vm, r)
	if err != nil {
		return nil, err
	}
	s := &AllDaySleep{Finished: true}
	for k, v := range rec {
		if v.valid {
			s.set(k, v)
		}
	}
	if err := s.readSeries(r, version, vm); err != nil {
		return nil, err
	}
	s.SleepSource = append(Hex{}, p[r.pos:]...)
	return s, nil
}

func (s *AllDaySleep) set(k int16, v value1) {
	times := map[int16]**time.Time{0: &s.BedTime, 1: &s.WakeupTime, 9: &s.GoBedTime, 10: &s.LeaveBedTime}
	ints := map[int16]**int{2: &s.Quality, 6: &s.Efficiency}
	durations := map[int16]**int64{7: &s.EntrySleepDuration, 8: &s.InBedDuration}
	switch {
	case k == -1:
		s.Finished = v.int32() == 1
	case times[k] != nil:
		*times[k] = tp(v.i)
	case ints[k] != nil:
		*ints[k] = ip(v.int32())
	case durations[k] != nil:
		d := v.i
		*durations[k] = &d
	}
}

// readSeries reads the valid hr, spo2 and snore blocks that follow the header
func (s *AllDaySleep) readSeries(r *reader, version int, vm map[int16]bool) error {
	var bed int64
	if s.BedTime != nil {
		bed = s.BedTime.Unix()
	}
	dst := map[int16]**Series{3: &s.HR, 4: &s.SpO2, 5: &s.SnoreDB}
	for _, f := range allDaySleepSeries {
		if !vm[f.id] {
			continue
		}
		ser, err := parseSeries(r, version, f, bed)
		if err != nil {
			return err
		}
		if d := dst[f.id]; d != nil {
			*d = ser
		}
	}
	return nil
}

// parseSeries reads an assist block: i16 interval, i16 count, [u32 first time since v2], count values
func parseSeries(r *reader, version int, f field1, bed int64) (*Series, error) {
	if r.left() < 4 {
		return nil, nil
	}
	unit, _ := r.int(2, true)
	cnt, _ := r.int(2, true)
	count := int(cnt)
	if count <= 0 {
		return nil, nil
	}
	n := int(f.size) * count
	if version >= 2 {
		n += 4
	}
	if r.left() < n {
		return nil, nil
	}
	start := bed
	if version >= 2 {
		start, _ = r.u32()
	}
	s := &Series{Start: time.Unix(start, 0).UTC(), IntervalS: int(unit), Values: make([]float64, count)}
	all := map[int16]bool{f.id: true}
	for i := range s.Values {
		v, err := parseRecord1([]field1{f}, version, all, r)
		if err != nil {
			return nil, err
		}
		s.Values[i] = v[f.id].num()
	}
	return s, nil
}
