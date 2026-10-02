package fitness

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// reader is a little-endian cursor; reads past the end return errShort like the app's ByteBuffer checks
type reader struct {
	b   []byte
	pos int
}

var errShort = errors.New("not enough bytes")

func (r *reader) left() int { return len(r.b) - r.pos }

func (r *reader) bytes(n int) ([]byte, error) {
	if n < 0 || r.left() < n {
		return nil, errShort
	}
	v := r.b[r.pos : r.pos+n]
	r.pos += n
	return v, nil
}

func (r *reader) u8() (uint8, error) {
	b, err := r.bytes(1)
	if err != nil {
		return 0, err
	}
	return b[0], nil
}

func (r *reader) u16() (int, error) {
	b, err := r.bytes(2)
	if err != nil {
		return 0, err
	}
	return int(binary.LittleEndian.Uint16(b)), nil
}

func (r *reader) u32() (int64, error) {
	b, err := r.bytes(4)
	if err != nil {
		return 0, err
	}
	return int64(binary.LittleEndian.Uint32(b)), nil
}

func (r *reader) f32() (float32, error) {
	b, err := r.bytes(4)
	if err != nil {
		return 0, err
	}
	return math.Float32frombits(binary.LittleEndian.Uint32(b)), nil
}

// int reads an n-byte integer, sign extended when signed
func (r *reader) int(n int, signed bool) (int64, error) {
	b, err := r.bytes(n)
	if err != nil {
		return 0, err
	}
	if !signed {
		switch n {
		case 1:
			return int64(b[0]), nil
		case 2:
			return int64(binary.LittleEndian.Uint16(b)), nil
		case 4:
			return int64(binary.LittleEndian.Uint32(b)), nil
		}
	}
	return signedLE(b), nil
}

// signedLE decodes a little-endian two's-complement integer of 1, 2, 4 or 8 bytes
func signedLE(b []byte) int64 {
	switch len(b) {
	case 1:
		return int64(int8(b[0])) //nolint:gosec // two's-complement wire field
	case 2:
		return int64(int16(binary.LittleEndian.Uint16(b))) //nolint:gosec // two's-complement wire field
	case 4:
		return int64(int32(binary.LittleEndian.Uint32(b))) //nolint:gosec // two's-complement wire field
	}
	return int64(binary.LittleEndian.Uint64(b)) //nolint:gosec // two's-complement wire field
}

// intValue mirrors Number.intValue() on a long: the low 32 bits
func intValue(v int64) int32 {
	return int32(v) //nolint:gosec // truncation is the app's semantics
}

// ---------------------------------------------------------------- OneDimen engine (OneDimenDataParser)

type depend struct {
	id     int16
	values []int64
}

// field1 is an OneDimenDataType descriptor
type field1 struct {
	id        int16
	size      uint8
	since     int
	float     bool
	signed    bool
	dep       *depend
	counts    int16
	hasCounts bool
}

// value1 is a decoded OneDimen value (DataValueItem)
type value1 struct {
	valid   bool
	i       int64
	f       float32
	isFloat bool
	bytes   []byte
}

// int32 mirrors Number.intValue() on the boxed value
func (v value1) int32() int32 {
	if v.isFloat {
		return int32(v.f)
	}
	return intValue(v.i)
}

func (v value1) num() float64 {
	if v.isFloat {
		return f64(v.f)
	}
	return float64(v.i)
}

type record1 map[int16]value1

// parseValid1 maps each field to its valid bit: fields with id >= 0 present in this version take the next bit, MSB first
func parseValid1(fields []field1, version int, valid []byte) (map[int16]bool, error) {
	m := make(map[int16]bool, len(fields))
	bit := 0
	for _, f := range fields {
		if f.id < 0 {
			continue
		}
		set := false
		if f.since <= version {
			if bit/8 >= len(valid) {
				return nil, fmt.Errorf("validity bitmap too short (%d bytes)", len(valid))
			}
			set = valid[bit/8]&(0x80>>(bit%8)) != 0
			bit++
		}
		m[f.id] = set
	}
	return m, nil
}

// versionValid marks every field present in this version as valid (used for record headers)
func versionValid(fields []field1, version int) map[int16]bool {
	m := make(map[int16]bool, len(fields))
	for _, f := range fields {
		m[f.id] = f.since <= version
	}
	return m
}

func exists1(f field1, version int, rec record1) bool {
	if f.since > version {
		return false
	}
	if f.dep == nil {
		return true
	}
	d, ok := rec[f.dep.id]
	if !ok || !d.valid {
		return false
	}
	for _, v := range f.dep.values {
		if int64(d.int32()) == v {
			return true
		}
	}
	return false
}

// parseRecord1 reads one record; bytes are consumed even for invalid fields and parsing stops at the first field that does not fit
func parseRecord1(fields []field1, version int, valid map[int16]bool, r *reader) (record1, error) {
	rec := record1{}
	for _, f := range fields {
		if !exists1(f, version, rec) {
			continue
		}
		if r.left() < int(f.size) {
			break
		}
		v, err := read1(f, rec, r)
		if err != nil {
			return nil, fmt.Errorf("field %d: %w", f.id, err)
		}
		ok, found := valid[f.id]
		v.valid = ok || !found
		rec[f.id] = v
	}
	return rec, nil
}

// read1 reads the value of one field; a counted field takes count * size bytes
func read1(f field1, rec record1, r *reader) (value1, error) {
	var v value1
	var err error
	size := int(f.size)
	switch {
	case f.hasCounts:
		n := int64(rec[f.counts].int32())
		v.i = n
		v.bytes, err = r.bytes(int(n) * size)
	case size == 0:
	case size == 1 || size == 2:
		v.i, err = r.int(size, f.signed)
	case size == 4 && f.float:
		v.isFloat = true
		v.f, err = r.f32()
	case size == 4:
		v.i, err = r.int(4, f.signed)
	case size == 8:
		v.i, err = r.int(8, true)
	default:
		v.bytes, err = r.bytes(size)
	}
	return v, err
}

// leastLen1 is the size of the unconditional fields with id >= 0
func leastLen1(fields []field1, version int) int {
	n := 0
	for _, f := range fields {
		if f.id >= 0 && f.since <= version && f.dep == nil {
			n += int(f.size)
		}
	}
	return n
}

func loop1(fields []field1, version int, valid map[int16]bool, r *reader, count int64) ([]record1, error) {
	least := leastLen1(fields, version)
	var out []record1
	for i := int64(0); i < count && r.left() >= least; i++ {
		before := r.pos
		rec, err := parseRecord1(fields, version, valid, r)
		if err != nil {
			return out, err
		}
		if r.pos == before {
			break // the app would repeat empty records up to count (a u32), stop instead
		}
		out = append(out, rec)
	}
	return out, nil
}

// ---------------------------------------------------------------- FourDimen engine (FourDimenDataParser)

// key4 is a FourDimenType; the app keys validity by its value, so equal types share one validity
type key4 struct {
	hi, mid, lo   int16
	hasMid, hasLo bool
}

// item4 is a DataItemLen: the whole value or a bit field
type item4 struct {
	on, whole   bool
	start, bits uint8
}

type dyn4 struct {
	id, dep int16
	value   int64
}

// group4 is a FourDimenDataInfo descriptor
type group4 struct {
	typed       bool
	typ         key4
	size        uint8
	hi, mid, lo item4
	since, maxV int
	signed      bool
	dyn         *dyn4
}

type valid4 struct{ exist, hi, mid, lo bool }

// parseValid4 assigns one validity nibble per enabled typed group, high nibble first
func parseValid4(groups []group4, version int, valid []byte) map[key4]valid4 {
	m := make(map[key4]valid4, len(groups))
	n := 0
	for _, g := range groups {
		if !g.typed {
			continue
		}
		var v valid4
		if (g.maxV == 0 || g.maxV >= version) && g.since <= version && n/2 < len(valid) {
			nib := valid[n/2] >> 4
			if n%2 == 1 {
				nib = valid[n/2] & 0x0f
			}
			v = valid4{nib&8 != 0, nib&4 != 0, nib&2 != 0, nib&1 != 0}
			n++
		}
		m[g.typ] = v
	}
	return m
}

func (it item4) value(raw int32) int64 {
	if it.whole {
		return int64(raw)
	}
	mask := int32(1)<<it.bits - 1
	return int64(uint32(raw&(mask<<it.start)) >> it.start) //nolint:gosec // bit field of the raw pattern
}

// parseSample4 reads one record: a group takes bytes only if its exist bit is set
func parseSample4(groups []group4, vm map[key4]valid4, r *reader) (map[int16]int64, error) {
	res := map[int16]int64{}
	for i := range groups {
		g := &groups[i]
		if g.dyn != nil {
			if err := g.readDyn(vm, res, r); err != nil {
				return res, err
			}
			continue
		}
		if !g.typed {
			return res, fmt.Errorf("record group without type")
		}
		v := vm[g.typ]
		if !v.exist {
			continue
		}
		if r.left() < int(g.size) {
			break
		}
		u, err := r.int(int(g.size), g.signed)
		if err != nil {
			return res, err
		}
		g.put(res, v, intValue(u))
	}
	return res, nil
}

// readDyn reads a dynamic field, present only when its dependency holds the expected value
func (g *group4) readDyn(vm map[key4]valid4, res map[int16]int64, r *reader) error {
	own := true
	if g.typed {
		v := vm[g.typ]
		own = v.exist && v.hi
	}
	dv, ok := res[g.dyn.dep]
	if !own || !ok || int64(intValue(dv)) != g.dyn.value {
		return nil
	}
	u, err := r.int(int(g.size), false)
	if err != nil {
		return fmt.Errorf("dynamic field %d: %w", g.dyn.id, err)
	}
	res[g.dyn.id] = int64(intValue(u))
	return nil
}

// put stores the valid parts of a raw group value
func (g *group4) put(res map[int16]int64, v valid4, raw int32) {
	if g.hi.on && v.hi {
		res[g.typ.hi] = g.hi.value(raw)
	}
	if g.mid.on && v.mid {
		res[g.typ.mid] = g.mid.value(raw)
	}
	if g.lo.on && v.lo {
		res[g.typ.lo] = g.lo.value(raw)
	}
}

func loop4(groups []group4, vm map[key4]valid4, r *reader, count int64) ([]map[int16]int64, error) {
	recLen := 0
	for _, g := range groups {
		if g.typed && vm[g.typ].exist {
			recLen += int(g.size)
		}
	}
	var out []map[int16]int64
	for i := int64(0); i < count && r.left() >= recLen; i++ {
		before := r.pos
		s, err := parseSample4(groups, vm, r)
		if err != nil {
			return out, err
		}
		if r.pos == before {
			break // the app would repeat empty records up to count (a u32), stop instead
		}
		out = append(out, s)
	}
	return out, nil
}
