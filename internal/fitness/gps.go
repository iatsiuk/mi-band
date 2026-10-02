package fitness

import (
	"fmt"
	"time"
)

// GPS is a workout track file (sport file type 2). Triathlon and ski files carry one track per sub sport.
type GPS struct {
	Tracks  []GPSTrack `json:"tracks"`
	TGCType *int       `json:"tgc_type,omitempty"`
	TGC     Hex        `json:"tgc,omitempty"` // opaque blob of version 4 files
}

// GPSTrack holds the points of one (sub) sport, keyed by proto sport type.
type GPSTrack struct {
	SportType int        `json:"sport_type"`
	Points    []GPSPoint `json:"points"`
}

// GPSPoint is one fix. The coordinate datum (WGS-84 or GCJ-02) is not established.
type GPSPoint struct {
	Time      time.Time `json:"time"`
	Lon       float64   `json:"lon"`
	Lat       float64   `json:"lat"`
	Accuracy  *float64  `json:"accuracy,omitempty"`
	Speed     *float64  `json:"speed,omitempty"`
	Source    *int      `json:"source,omitempty"`
	AltitudeM *float64  `json:"altitude_m,omitempty"`
	HDOP      *float64  `json:"hdop,omitempty"`
}

func parseGPS(id ID, valid, p []byte) (*GPS, error) {
	v := int(id.Version)
	vm, err := parseValid1(gpsPoint, v, valid)
	if err != nil {
		return nil, err
	}
	g := &GPS{Tracks: []GPSTrack{}}
	r := &reader{b: p}
	if id.Type == 17 || id.Type == 21 {
		err = g.readSubTracks(int(id.Type), v, vm, r)
	} else {
		err = g.readTrack(int(id.Type), v, vm, r)
	}
	if err != nil {
		return nil, err
	}
	return g, nil
}

// gpsUsable reports whether points have valid time, longitude and latitude
func gpsUsable(vm map[int16]bool) bool {
	return vm[0] && vm[1] && vm[2]
}

// readSubTracks reads the u8 sub sport, u32 count, points blocks of triathlon and ski files
func (g *GPS) readSubTracks(sport, v int, vm map[int16]bool, r *reader) error {
	for r.left() >= 2 {
		sub, _ := r.u8()
		n, err := r.u32()
		if err != nil {
			return err
		}
		recs, err := loop1(gpsPoint, v, vm, r, n)
		if err != nil {
			return err
		}
		if gpsUsable(vm) {
			g.add(gpsSubSport(sport, int(sub)), gpsPoints(recs))
		}
	}
	return nil
}

func (g *GPS) readTrack(sport, v int, vm map[int16]bool, r *reader) error {
	count := int64(len(r.b))
	var err error
	if v >= 4 {
		if count, err = r.u32(); err != nil {
			return err
		}
	}
	recs, err := loop1(gpsPoint, v, vm, r, count)
	if err != nil {
		return err
	}
	if v >= 4 {
		if err := g.readTGC(r); err != nil {
			return err
		}
	}
	if gpsUsable(vm) && len(recs) > 0 {
		g.add(sport, gpsPoints(recs))
	}
	return nil
}

// readTGC reads the u8 type, u32 length and blob that end version 4 tracks
func (g *GPS) readTGC(r *reader) error {
	t, err := r.u8()
	if err != nil {
		return err
	}
	n, err := r.u32()
	if err != nil {
		return err
	}
	g.TGCType = ip(int32(t))
	if t == 0 {
		return nil
	}
	b, err := r.bytes(int(n))
	if err != nil {
		return fmt.Errorf("tgc data: %w", err)
	}
	g.TGC = append(Hex{}, b...)
	return nil
}

func gpsSubSport(sport, sub int) int {
	if sport != 17 {
		return sub
	}
	switch sub {
	case 0:
		return 10
	case 1:
		return 6
	case 2:
		return 1
	}
	return 0
}

// add appends points to the track of the sport, creating it in first-seen order
func (g *GPS) add(sport int, pts []GPSPoint) {
	for i := range g.Tracks {
		if g.Tracks[i].SportType == sport {
			g.Tracks[i].Points = append(g.Tracks[i].Points, pts...)
			return
		}
	}
	g.Tracks = append(g.Tracks, GPSTrack{SportType: sport, Points: pts})
}

func gpsPoints(recs []record1) []GPSPoint {
	pts := make([]GPSPoint, 0, len(recs))
	for _, rec := range recs {
		pt := GPSPoint{Time: time.Unix(rec[0].i, 0).UTC(), Lon: f64(rec[1].f), Lat: f64(rec[2].f)}
		pt.Accuracy, pt.AltitudeM, pt.HDOP = validFloat(rec, 3), validFloat(rec, 6), validFloat(rec, 7)
		if x, ok := rec[4]; ok && x.valid {
			raw := x.int32()
			pt.Speed = fp(float32((raw&0xfff0)>>4) / 10)
			if s, ok := rec[5]; ok && s.valid {
				pt.Source = ip(raw & 0x0f)
			}
		}
		pts = append(pts, pt)
	}
	return pts
}

// validFloat returns the float of a valid field, nil if the field is absent or invalid
func validFloat(rec record1, k int16) *float64 {
	if x, ok := rec[k]; ok && x.valid {
		return fp(x.f)
	}
	return nil
}

// RecoverRate is the post-workout heart rate recovery file (sport file type 3).
type RecoverRate struct {
	Time      time.Time `json:"time"`
	Rate      int       `json:"rate"`
	Ratio     float64   `json:"ratio"`
	Samples   []int     `json:"samples"`
	StartRate *int      `json:"start_rate,omitempty"`
	EndRate   *int      `json:"end_rate,omitempty"`
}

var recoverRateSample = []field1{{id: 0, size: 1, since: 1}}

func parseRecoverRate(version int, p []byte) (*RecoverRate, error) {
	r := &reader{b: p}
	t, err := r.u32()
	if err != nil {
		return nil, err
	}
	rate, err := r.u8()
	if err != nil {
		return nil, err
	}
	ratio, err := r.u8()
	if err != nil {
		return nil, err
	}
	n, err := r.u16()
	if err != nil {
		return nil, err
	}
	recs, err := loop1(recoverRateSample, version, map[int16]bool{0: true}, r, int64(n))
	if err != nil {
		return nil, err
	}
	rr := &RecoverRate{Time: time.Unix(t, 0).UTC(), Rate: int(rate), Ratio: f64(float32(ratio) / 10), Samples: make([]int, len(recs))}
	for i, rec := range recs {
		rr.Samples[i] = int(rec[0].int32())
	}
	if len(recs) > 0 {
		rr.StartRate = ip(recs[0][0].int32())
		rr.EndRate = ip(recs[len(recs)-1][0].int32())
	}
	return rr, nil
}
