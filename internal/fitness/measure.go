package fitness

import "time"

// measurement types (MeasureDataType)
const (
	MeasureHR            = 1
	MeasureSpO2          = 2
	MeasureStress        = 3
	MeasureTemperature   = 4
	MeasureBloodPressure = 5
)

var measureNames = map[int]string{1: "hr", 2: "spo2", 3: "stress", 4: "temperature", 5: "blood_pressure"}

// Measurement is one manual/spot measurement (daily type 6).
type Measurement struct {
	Time      time.Time `json:"time"`
	Type      int       `json:"type"`
	TypeName  string    `json:"type_name"`
	Value     *int      `json:"value,omitempty"`
	SkinTempC *float64  `json:"skin_temp_c,omitempty"`
	BodyTempC *float64  `json:"body_temp_c,omitempty"`
	Systolic  *int      `json:"systolic,omitempty"`
	Diastolic *int      `json:"diastolic,omitempty"`
	Pulse     *int      `json:"pulse,omitempty"`
	Status    *int      `json:"status,omitempty"`
}

// v1 value lengths per type; v2 stores the length in the high nibble of the type byte
var measureLenV1 = map[int]int{1: 1, 2: 1, 3: 1, 4: 4, 5: 6}

func parseMeasurements(version int, p []byte) ([]Measurement, error) {
	r := &reader{b: p}
	out := []Measurement{}
	for r.left() >= 5 {
		t, _ := r.u32()
		b, _ := r.u8()
		typ, n, ok := measureLayout(version, int(b))
		if !ok || r.left() < n {
			break
		}
		m := Measurement{Time: time.Unix(t, 0).UTC(), Type: typ, TypeName: measureNames[typ]}
		kept, err := m.read(n, r)
		if err != nil {
			return nil, err
		}
		if kept {
			out = append(out, m)
		}
	}
	return out, nil
}

// measureLayout splits the type byte into the measurement type and its value length
func measureLayout(version, b int) (typ, n int, ok bool) {
	if version == 1 {
		n, ok = measureLenV1[b]
		return b, n, ok
	}
	return b & 0x0f, b >> 4, true
}

// read decodes an n-byte value; false means a length the app skips
func (m *Measurement) read(n int, r *reader) (bool, error) {
	switch {
	case m.Type == MeasureTemperature:
		return true, m.readTemperature(r)
	case m.Type == MeasureBloodPressure:
		b, err := r.bytes(6)
		if err != nil {
			return false, err
		}
		m.Systolic = ip(int32(b[0]) | int32(b[1])<<8)
		m.Diastolic = ip(int32(b[2]) | int32(b[3])<<8)
		m.Pulse = ip(int32(b[4]))
		m.Status = ip(int32(b[5]))
	case n == 1 || n == 2:
		v, _ := r.int(n, false)
		m.Value = ip(intValue(v))
	default:
		r.pos += n
		return false, nil
	}
	return true, nil
}

func (m *Measurement) readTemperature(r *reader) error {
	skin, err := r.u16()
	if err != nil {
		return err
	}
	body, err := r.u16()
	if err != nil {
		return err
	}
	if skin > 0 {
		m.SkinTempC = fp(float32(skin) / 100)
	}
	if body > 0 {
		m.BodyTempC = fp(float32(body) / 100)
	}
	return nil
}
