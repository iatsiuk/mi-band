package fitness

import "time"

// recordParser holds the tables of one SportRecordBaseParser subclass
type recordParser struct {
	init, it  []field1
	distTimes int
	swimming  bool
	four      map[string][]group4
	one       map[string][]field1
}

// Segment is one sport or pause interval of a workout record (sport file type 0).
// Times are the raw device values.
type Segment struct {
	Start       time.Time `json:"start"`
	Count       int64     `json:"count"`
	InitHeightM *float64  `json:"init_height_m,omitempty"`
	SkiPath     *int      `json:"ski_path,omitempty"`

	// interval training summary
	Pause                    *bool    `json:"pause,omitempty"`
	ITState                  *int     `json:"it_state,omitempty"`
	ITTotalCount             *int     `json:"it_total_count,omitempty"`
	ITTotalDistance          *int     `json:"it_total_distance,omitempty"`
	ITTotalSteps             *int     `json:"it_total_steps,omitempty"`
	ITAvgPaces               *int     `json:"it_avg_paces,omitempty"`
	ITTotalDuration          *int     `json:"it_total_duration,omitempty"`
	ITAvgSpeed               *float64 `json:"it_avg_speed,omitempty"`
	ITSkiTotalTime           *int     `json:"it_ski_total_time,omitempty"`
	ITSkiTotalDistance       *int     `json:"it_ski_total_distance,omitempty"`
	ITSkiAvgSpeed            *float64 `json:"it_ski_avg_speed,omitempty"`
	ITSkiMaxSpeed            *float64 `json:"it_ski_max_speed,omitempty"`
	ITSkiSlope               *int     `json:"it_ski_slope,omitempty"`
	DivingType               *int     `json:"diving_type,omitempty"`
	DivingInSectionDuration  *int     `json:"diving_in_section_duration,omitempty"`
	DivingOutSectionDuration *int     `json:"diving_out_section_duration,omitempty"`
	DivingAvgDepth           *float64 `json:"diving_avg_depth,omitempty"`
	DivingMaxDepth           *float64 `json:"diving_max_depth,omitempty"`
	DivingAvgTemperature     *float64 `json:"diving_avg_temperature,omitempty"`
	DivingMinTemperature     *float64 `json:"diving_min_temperature,omitempty"`
	DivingAvgDownSpeed       *float64 `json:"diving_avg_down_speed,omitempty"`
	DivingAvgUpSpeed         *float64 `json:"diving_avg_up_speed,omitempty"`

	Samples []Sample `json:"samples"`
}

// Sample is one record of a segment, nominally one second. Values are scaled like the app does.
type Sample struct {
	Start               time.Time `json:"start"`
	End                 time.Time `json:"end"`
	DurationS           int       `json:"duration_s"`
	Type                *int      `json:"type,omitempty"` // swimming type (0 section, 1 passage, 2 manual) or ball swing type
	HR                  *int      `json:"hr,omitempty"`
	Calories            *int      `json:"calories,omitempty"`
	TotalCal            *int      `json:"total_cal,omitempty"`
	Steps               *int      `json:"steps,omitempty"`
	DistanceM           *float64  `json:"distance_m,omitempty"`
	WholeKM             *bool     `json:"whole_km,omitempty"`
	HeightM             *float64  `json:"height_m,omitempty"`
	HeightChangeM       *float64  `json:"height_change_m,omitempty"`
	Count               *int      `json:"count,omitempty"`
	Pace                *int      `json:"pace,omitempty"`
	Speed               *float64  `json:"speed,omitempty"`
	Cadence             *int      `json:"cadence,omitempty"`
	CycleCadence        *int      `json:"cycle_cadence,omitempty"`
	RowingCadence       *int      `json:"rowing_cadence,omitempty"`
	JumpCadence         *int      `json:"jump_cadence,omitempty"`
	Stride              *int      `json:"stride,omitempty"`
	Power               *int      `json:"power,omitempty"`
	RunningPower        *int      `json:"running_power,omitempty"`
	EqualIntensityPace  *int      `json:"equal_intensity_pace,omitempty"`
	SpO2                *int      `json:"spo2,omitempty"`
	Stress              *int      `json:"stress,omitempty"`
	LandingImpact       *float64  `json:"landing_impact,omitempty"`
	TouchdownTime       *int      `json:"touchdown_time,omitempty"`
	InTheAirTime        *int      `json:"in_the_air_time,omitempty"`
	TouchdownAirRatio   *float64  `json:"touchdown_air_ratio,omitempty"`
	VerticalStrideRatio *float64  `json:"vertical_stride_ratio,omitempty"`
	VerticalAmplitude   *float64  `json:"vertical_amplitude,omitempty"`
	Resistance          *int      `json:"resistance,omitempty"`
	PullOars            *int      `json:"pull_oars,omitempty"`
	LayOars             *int      `json:"lay_oars,omitempty"`
	MainPosture         *int      `json:"main_posture,omitempty"`
	Swolf               *int      `json:"swolf,omitempty"`
	StrokeCount         *int      `json:"stroke_count,omitempty"`
	StrokeFreq          *int      `json:"stroke_freq,omitempty"`
	UnknownStrokeFreq   *int      `json:"unknown_stroke_freq,omitempty"`
	BreastStrokeFreq    *int      `json:"breast_stroke_freq,omitempty"`
	CrawlStrokeFreq     *int      `json:"crawl_stroke_freq,omitempty"`
	BackStrokeFreq      *int      `json:"back_stroke_freq,omitempty"`
	ButterflyStrokeFreq *int      `json:"butterfly_stroke_freq,omitempty"`
	FreeBreathAngle     *int      `json:"free_breath_angle,omitempty"`
	FreePitchAngle      *int      `json:"free_pitch_angle,omitempty"`
	FrogBreathAngle     *int      `json:"frog_breath_angle,omitempty"`
	FrogSkiDuration     *int      `json:"frog_ski_duration,omitempty"`
	SwimmingHRCount     *int      `json:"swimming_hr_count,omitempty"`
	SwimmingHR          []int     `json:"swimming_hr,omitempty"`
	DribbleDuration     *int      `json:"dribble_duration,omitempty"`
	BackSwingDuration   *float64  `json:"back_swing_duration,omitempty"`
	DownSwingDuration   *float64  `json:"down_swing_duration,omitempty"`
	SwingRhythm         *float64  `json:"swing_rhythm,omitempty"`
	SwingSpeed          *float64  `json:"swing_speed,omitempty"`
	SkipStartingHeight  *int      `json:"skip_starting_height,omitempty"`
	StumbleTimes        *int      `json:"stumble_times,omitempty"`
	DivingStatus        *int      `json:"diving_status,omitempty"`
	DivingDepth         *float64  `json:"diving_depth,omitempty"`
	DivingTemperature   *float64  `json:"diving_temperature,omitempty"`
}

// recordArray picks the table a parser uses for a version
type recordArray func(v int) string

func fixedArray(name string) recordArray { return func(int) string { return name } }

// recordLayouts mirrors FitnessDataParser.getSportRecordParserInstance and each parseRecord
var recordLayouts = map[int]struct {
	parser string
	array  recordArray
}{
	1: {"OutdoorSport", fixedArray("dataInfoArray")}, 2: {"OutdoorSport", fixedArray("dataInfoArray")},
	4: {"OutdoorSport", fixedArray("dataInfoArray")}, 5: {"OutdoorSport", fixedArray("dataInfoArray")},
	15: {"OutdoorSport", fixedArray("dataInfoArray")},
	3:  {"IndoorRun", fixedArray("dataInfoArray")},
	6:  {"OutdoorBiking", fixedArray("dataInfoArray")},
	7: {"IndoorBiking", func(v int) string {
		if v == 1 {
			return "v1DataTypeArray"
		}
		return "v2DataInfoArray"
	}},
	8: {"FreeTraining", func(v int) string {
		if v >= 3 {
			return "dataInfoArrayV3"
		}
		return "dataTypeArray"
	}},
	9: {"Swimming", fixedArray("dataTypeArray")}, 10: {"Swimming", fixedArray("dataTypeArray")},
	11: {"EllipticalMachine", fixedArray("dataInfoArray")},
	12: {"Yoga", fixedArray("dataTypeArray")},
	13: {"RowingMachine", func(v int) string {
		if v < 4 {
			return "dataInfoArray"
		}
		return "dataInfoArray4"
	}},
	14: {"RopeSkipping", func(v int) string {
		switch {
		case v < 5:
			return "dataTypeArray"
		case v == 5:
			return "dataTypeArray5"
		}
		return "dataTypeArray6"
	}},
	16: {"HITraining", fixedArray("dataTypeArray")},
	18: {"OrdinaryBall", fixedArray("dataInfoArray")},
	19: {"Basketball", fixedArray("dataInfoArray")},
	20: {"Golf", fixedArray("dataTypeArray")},
	21: {"Ski", fixedArray("dataInfoArray")},
	22: {"OutdoorStep", fixedArray("dataInfoArray")},
	23: {"OutdoorNoStep", fixedArray("dataInfoArray")},
	24: {"RockClimbing", fixedArray("dataInfoArray")},
	25: {"Diving", fixedArray("dataInfoArray")},
}

func recordLayout(sport, v int) (rp recordParser, four []group4, one []field1, reason string) {
	if sport == 17 {
		return rp, nil, nil, "triathlon records are not decoded"
	}
	l, ok := recordLayouts[sport]
	if !ok {
		return rp, nil, nil, "no record parser for this sport type"
	}
	rp = recordParsers[l.parser]
	array := l.array(v)
	// rowing, rope skipping and HIIT use a grouped layout when the version has no IT summary
	if (sport == 13 || sport == 14 || sport == 16) && leastLen1(rp.it, v) == 0 {
		return rp, nil, nil, "grouped record layout is not decoded"
	}
	return rp, rp.four[array], rp.one[array], ""
}

func parseSportRecord(id ID, valid, p []byte) ([]Segment, string, error) {
	v := int(id.Version)
	rp, four, one, reason := recordLayout(int(id.Type), v)
	if reason != "" {
		return nil, reason, nil
	}
	var vm4 map[key4]valid4
	var vm1 map[int16]bool
	if four != nil {
		vm4 = parseValid4(four, v, valid)
	} else {
		var err error
		if vm1, err = parseValid1(one, v, valid); err != nil {
			return nil, "", err
		}
	}
	r := &reader{b: p}
	head := leastLen1(rp.init, v) + 8 + leastLen1(rp.it, v)
	segs := []Segment{}
	for r.left() >= head {
		seg, err := parseSegment(&rp, v, four, vm4, one, vm1, r)
		if err != nil {
			return nil, "", err
		}
		segs = append(segs, seg)
	}
	return segs, "", nil
}

// parseSegment reads [pause-init fields][u32 count][u32 start][IT summary fields][count samples]
func parseSegment(rp *recordParser, v int, four []group4, vm4 map[key4]valid4, one []field1, vm1 map[int16]bool, r *reader) (Segment, error) {
	var seg Segment
	height, err := seg.readInit(rp, v, r)
	if err != nil {
		return seg, err
	}
	count, err := r.u32()
	if err != nil {
		return seg, err
	}
	start, err := r.u32()
	if err != nil {
		return seg, err
	}
	seg.Start, seg.Count = time.Unix(start, 0).UTC(), count
	if len(rp.it) > 0 {
		rec, err := parseRecord1(rp.it, v, versionValid(rp.it, v), r)
		if err != nil {
			return seg, err
		}
		seg.setIT(rec)
	}
	samples, err := readSamples(four, vm4, one, vm1, v, r, count)
	if err != nil {
		return seg, err
	}
	seg.Samples = processSamples(rp, start, height, samples)
	return seg, nil
}

// readInit reads the pause-init fields and returns the initial height, if any
func (seg *Segment) readInit(rp *recordParser, v int, r *reader) (*float32, error) {
	if len(rp.init) == 0 {
		return nil, nil
	}
	rec, err := parseRecord1(rp.init, v, versionValid(rp.init, v), r)
	if err != nil {
		return nil, err
	}
	if x, ok := rec[-2]; ok {
		seg.SkiPath = ip(x.int32())
	}
	x, ok := rec[0]
	if !ok {
		return nil, nil
	}
	h := float32(x.num())
	seg.InitHeightM = fp(h)
	return &h, nil
}

// readSamples reads count samples with the four-dimension layout when present, else the one-dimension one
func readSamples(four []group4, vm4 map[key4]valid4, one []field1, vm1 map[int16]bool, v int, r *reader, count int64) ([]map[int16]value1, error) {
	var samples []map[int16]value1
	if four == nil {
		recs, err := loop1(one, v, vm1, r, count)
		for _, s := range recs {
			samples = append(samples, s)
		}
		return samples, err
	}
	recs, err := loop4(four, vm4, r, count)
	for _, s := range recs {
		m := make(map[int16]value1, len(s))
		for k, x := range s {
			m[k] = value1{valid: true, i: x}
		}
		samples = append(samples, m)
	}
	return samples, err
}

func (seg *Segment) setIT(rec record1) {
	ints := map[int16]**int{
		42: &seg.ITTotalCount, 43: &seg.ITTotalDistance, 54: &seg.ITTotalSteps, 55: &seg.ITAvgPaces,
		59: &seg.ITSkiTotalTime, 60: &seg.ITSkiTotalDistance, 63: &seg.ITSkiSlope, 64: &seg.DivingType,
		65: &seg.DivingInSectionDuration, 66: &seg.DivingOutSectionDuration, 78: &seg.ITTotalDuration,
	}
	tenths := map[int16]**float64{
		58: &seg.ITAvgSpeed, 61: &seg.ITSkiAvgSpeed, 62: &seg.ITSkiMaxSpeed, 67: &seg.DivingAvgDepth,
		68: &seg.DivingMaxDepth, 69: &seg.DivingAvgTemperature, 75: &seg.DivingMinTemperature,
		76: &seg.DivingAvgDownSpeed, 77: &seg.DivingAvgUpSpeed,
	}
	for k, x := range rec {
		v := x.int32()
		switch {
		case k == 41:
			pause := v&0x80 == 0
			state := v & 0x7f
			if pause && state == 32 {
				state = 128
			}
			seg.Pause, seg.ITState = &pause, ip(state)
		case ints[k] != nil:
			*ints[k] = ip(v)
		case tenths[k] != nil:
			*tenths[k] = fp(float32(v) / 10)
		}
	}
}

// processSamples mirrors SportRecordBaseParser.processSportRecordList: each sample covers one
// second unless it carries an end time; heights accumulate from the segment's initial height
func processSamples(rp *recordParser, start int64, height *float32, recs []map[int16]value1) []Sample {
	out := make([]Sample, 0, len(recs))
	t, passageStart := start, start
	for _, rec := range recs {
		s := Sample{}
		end := t + 1
		for k, x := range rec {
			if !x.valid {
				continue
			}
			if k == 1 {
				end = x.i
				continue
			}
			s.set(k, x, rec, rp.distTimes, &height)
		}
		begin := t
		if rp.swimming && s.Type != nil && *s.Type == 1 {
			// a passage starts where the previous passage ended
			begin, passageStart = passageStart, end
		}
		s.Start, s.End, s.DurationS = time.Unix(begin, 0).UTC(), time.Unix(end, 0).UTC(), int(intValue(end-begin))
		t = end
		out = append(out, s)
	}
	return out
}

func (s *Sample) set(k int16, x value1, rec map[int16]value1, distTimes int, height **float32) {
	v := x.int32()
	if s.setScaled(k, v) {
		return
	}
	switch k {
	case 6:
		b := v > 0
		s.WholeKM = &b
	case 8:
		s.addHeightChange(v, rec, height)
	case 9, 88:
		s.DistanceM = fp(float32(v) / float32(distTimes))
	case 74:
		s.Speed = fp(signedSpeed(v, rec))
	case 86:
		s.setSwimmingHR(x, v)
	case 87:
		s.setHeight(v, height)
	}
}

// setScaled sets the plain integer and fixed-point fields; false means k is not one of them
func (s *Sample) setScaled(k int16, v int32) bool {
	ints := map[int16]**int{
		-1: &s.Type, 30: &s.Type, 2: &s.Calories, 3: &s.TotalCal, 4: &s.Steps, 5: &s.HR, 10: &s.Count, 11: &s.MainPosture, 12: &s.Pace,
		13: &s.Swolf, 16: &s.StrokeCount, 17: &s.StrokeFreq, 18: &s.UnknownStrokeFreq, 19: &s.BreastStrokeFreq,
		20: &s.CrawlStrokeFreq, 21: &s.BackStrokeFreq, 22: &s.ButterflyStrokeFreq, 23: &s.Resistance, 24: &s.PullOars,
		25: &s.LayOars, 26: &s.Count, 27: &s.Count, 28: &s.DribbleDuration, 29: &s.Count, 35: &s.Count,
		36: &s.SkipStartingHeight, 37: &s.StumbleTimes, 38: &s.SpO2, 39: &s.Stress, 40: &s.Stride, 45: &s.TouchdownTime,
		46: &s.InTheAirTime, 47: &s.Power, 49: &s.Cadence, 50: &s.CycleCadence, 52: &s.RowingCadence, 53: &s.JumpCadence,
		56: &s.EqualIntensityPace, 57: &s.RunningPower, 70: &s.DivingStatus, 81: &s.FreeBreathAngle, 82: &s.FreePitchAngle,
		83: &s.FrogBreathAngle, 84: &s.FrogSkiDuration, 85: &s.SwimmingHRCount,
	}
	tenths := map[int16]**float64{
		33: &s.SwingRhythm, 34: &s.SwingSpeed, 44: &s.LandingImpact, 48: &s.TouchdownAirRatio, 51: &s.Speed,
		71: &s.DivingDepth, 72: &s.DivingTemperature, 79: &s.VerticalStrideRatio, 80: &s.VerticalAmplitude,
	}
	hundredths := map[int16]**float64{31: &s.BackSwingDuration, 32: &s.DownSwingDuration}
	if dst, ok := ints[k]; ok {
		*dst = ip(v)
		return true
	}
	if dst, ok := tenths[k]; ok {
		*dst = fp(float32(v) / 10)
		return true
	}
	if dst, ok := hundredths[k]; ok {
		*dst = fp(float32(v) / 100)
		return true
	}
	return false
}

// setSwimmingHR expands the v heart rate bytes of a swimming sample
func (s *Sample) setSwimmingHR(x value1, v int32) {
	if x.bytes == nil || v <= 0 {
		return
	}
	s.SwimmingHR = make([]int, v)
	for i := range s.SwimmingHR {
		s.SwimmingHR[i] = int(x.bytes[i])
	}
}

// addHeightChange applies a height change: sign flag in field 7, value in decimetres
func (s *Sample) addHeightChange(v int32, rec map[int16]value1, height **float32) {
	sign, ok := rec[7]
	if *height == nil || !ok || !sign.valid {
		return
	}
	d := int32(-1)
	if sign.int32() != 0 {
		d = 1
	}
	change := float32(d*v) / 10
	h := **height + change
	*height = &h
	s.HeightM, s.HeightChangeM = fp(h), fp(change)
}

// signedSpeed applies the sign flag in field 73; without a valid flag the speed is 0
func signedSpeed(v int32, rec map[int16]value1) float32 {
	sign, ok := rec[73]
	switch {
	case ok && sign.valid && sign.int32() == 1:
		return float32(v) / 10
	case ok && sign.valid && sign.int32() == 0:
		return float32(v) * -1 / 10
	}
	return 0
}

// setHeight sets the absolute height in decimetres; 65535 keeps the previous height
func (s *Sample) setHeight(v int32, height **float32) {
	if v != 65535 {
		h := float32(v) / 10
		*height = &h
	}
	if *height != nil {
		s.HeightM = fp(**height)
	} else {
		s.HeightM = nil
	}
}
