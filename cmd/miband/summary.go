package main

import (
	"sort"
	"time"

	"miband/internal/cloud"
)

type daySteps struct {
	DID          string  `json:"did"`
	Date         string  `json:"date"`
	Steps        int     `json:"steps"`
	DistanceM    int     `json:"distance_m"`
	CaloriesKcal float64 `json:"calories_kcal"`
}

// deviceRecords keeps the records of one device (all when did is empty).
func deviceRecords(recs []cloud.Record, did string) []cloud.Record {
	out := []cloud.Record{}
	for _, r := range recs {
		if did == "" || r.DID == did {
			out = append(out, r)
		}
	}
	return out
}

// liveRecords also drops tombstones, which Mi Fitness hard-deletes.
func liveRecords(recs []cloud.Record, did string) []cloud.Record {
	out := []cloud.Record{}
	for _, r := range deviceRecords(recs, did) {
		if !r.Deleted {
			out = append(out, r)
		}
	}
	return out
}

// stepsByDay sums step samples per device and local date of each record.
func stepsByDay(recs []cloud.Record, did string) []daySteps {
	type key struct{ did, date string }
	byDay := map[key]*daySteps{}
	for _, r := range liveRecords(recs, did) {
		v, err := r.Decode()
		st, ok := v.(*cloud.Step)
		if err != nil || !ok {
			continue
		}
		k := key{r.DID, st.Time.In(time.FixedZone("", r.ZoneOffset)).Format("2006-01-02")}
		d := byDay[k]
		if d == nil {
			d = &daySteps{DID: k.did, Date: k.date}
			byDay[k] = d
		}
		d.Steps += st.Steps
		d.DistanceM += st.DistanceM
		d.CaloriesKcal += st.CaloriesKcal
	}
	res := make([]daySteps, 0, len(byDay))
	for _, d := range byDay {
		res = append(res, *d)
	}
	sort.Slice(res, func(i, j int) bool {
		if res[i].DID != res[j].DID {
			return res[i].DID < res[j].DID
		}
		return res[i].Date < res[j].Date
	})
	return res
}
