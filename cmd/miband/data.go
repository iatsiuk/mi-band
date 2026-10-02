package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"miband/internal/cloud"
	"miband/internal/fitness"
)

const maxDays = 3650

type dataOptions struct {
	days    int
	kinds   string
	did     string
	details bool
	raw     bool
}

type workoutOut struct {
	cloud.Workout
	Files      map[string]*fitness.File `json:"files,omitempty"`
	FileErrors map[string]string        `json:"file_errors,omitempty"`
	// Unavailable lists detail files the cloud does not have (e.g. no GPS for indoor sports)
	Unavailable []string `json:"files_unavailable,omitempty"`
}

func newDataCmd(cf *cloudFlags) *cobra.Command {
	o := &dataOptions{}
	cmd := &cobra.Command{
		Use:   "data",
		Short: "Fetch steps, sleep and workouts synced by Mi Fitness from the Xiaomi cloud",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if o.days < 0 || o.days > maxDays {
				return fmt.Errorf("--days must be between 0 and %d", maxDays)
			}
			cl, err := newClient(cmd.Context(), cf)
			if err != nil {
				return err
			}
			out, err := fetchData(cmd.Context(), cl, o, time.Now())
			if err != nil {
				return err
			}
			return writeOutput(cmd.OutOrStdout(), cf.format, out)
		},
	}
	f := cmd.Flags()
	f.IntVar(&o.days, "days", 7, "days before today to include, from local midnight; today is always included")
	f.StringVar(&o.kinds, "kind", "steps,sleep,workouts", "comma list: steps, sleep, workouts, or any raw record key")
	f.StringVar(&o.did, "did", "", "only records of this device id (see miband devices); default: all devices")
	f.BoolVar(&o.details, "details", false, "download and decode raw workout files (per-second record, GPS); markdown lists them, json prints them")
	f.BoolVar(&o.raw, "raw", false, "print raw cloud records instead of summaries")
	return cmd
}

type dataFetch struct {
	ctx      context.Context
	cl       *client
	o        *dataOptions
	from, to time.Time
	out      *dataResult
}

// fetchData collects every requested kind from local midnight o.days before now up to now.
func fetchData(ctx context.Context, cl *client, o *dataOptions, now time.Time) (*dataResult, error) {
	y, m, d := now.AddDate(0, 0, -o.days).Date()
	r := &dataFetch{ctx: ctx, cl: cl, o: o, from: time.Date(y, m, d, 0, 0, 0, 0, time.Local), to: now}
	r.out = &dataResult{From: r.from, To: r.to}
	for _, kind := range strings.Split(o.kinds, ",") {
		var err error
		switch kind = strings.TrimSpace(kind); kind {
		case "":
		case "workouts":
			err = r.workouts()
		default:
			err = r.records(kind)
		}
		if err != nil {
			return nil, err
		}
	}
	return r.out, nil
}

func (r *dataFetch) workouts() error {
	ws, err := withRetry(r.ctx, r.cl, func(s *cloud.Session) ([]cloud.Workout, error) { return s.Workouts(r.ctx, r.from, r.to) })
	if err != nil {
		return fmt.Errorf("workouts: %w", err)
	}
	list := []workoutOut{}
	for i := range ws {
		w := &ws[i]
		if !r.keep(w) {
			continue
		}
		wo := workoutOut{Workout: *w}
		if !r.o.raw {
			wo.Value = nil
		}
		if r.o.details && !w.Deleted {
			if err := workoutFiles(r.ctx, r.cl, &wo); err != nil {
				return err
			}
			r.out.Partial = r.out.Partial || len(wo.FileErrors) > 0
		}
		list = append(list, wo)
	}
	r.add("workouts", list)
	return nil
}

// keep drops workouts of other devices and, unless raw, tombstones.
func (r *dataFetch) keep(w *cloud.Workout) bool {
	return (r.o.did == "" || w.DID == r.o.did) && (r.o.raw || !w.Deleted)
}

func (r *dataFetch) add(key string, v any) {
	r.out.Sections = append(r.out.Sections, dataSection{Key: key, Value: v})
}

func (r *dataFetch) records(kind string) error {
	recs, err := withRetry(r.ctx, r.cl, func(s *cloud.Session) ([]cloud.Record, error) {
		return s.FitnessData(r.ctx, r.from, r.to, kind)
	})
	if err != nil {
		return fmt.Errorf("%s: %w", kind, err)
	}
	switch {
	case r.o.raw:
		r.add(kind, deviceRecords(recs, r.o.did))
	case kind == "steps":
		r.add("steps_by_day", stepsByDay(recs, r.o.did))
	default:
		r.add(kind, decodedRecords(recs, r.o.did))
	}
	return nil
}

// decodedRecords replaces known values with typed ones and keeps unknown records as is.
func decodedRecords(recs []cloud.Record, did string) []any {
	vals := []any{}
	live := liveRecords(recs, did)
	for i := range live {
		if v, err := live[i].Decode(); err == nil && v != nil {
			vals = append(vals, decodedRecord{DID: live[i].DID, Value: v, rec: &live[i]})
		} else {
			vals = append(vals, live[i])
		}
	}
	return vals
}

// workoutFiles downloads the detail files of a workout. Files the cloud does not have are
// listed as unavailable: not every sport has GPS or a recover-rate file.
func workoutFiles(ctx context.Context, cl *client, wo *workoutOut) error {
	wo.Files, wo.FileErrors = map[string]*fitness.File{}, map[string]string{}
	for _, f := range []struct {
		name string
		typ  uint8
	}{{"record", fitness.FileRecord}, {"gps", fitness.FileGPS}, {"recover_rate", fitness.FileRecoverRate}} {
		id, ok := wo.FileID(f.typ)
		if !ok {
			continue
		}
		data, err := withRetry(ctx, cl, func(s *cloud.Session) ([]byte, error) { return s.RawFile(ctx, wo.DID, id) })
		switch {
		case ctx.Err() != nil:
			return ctx.Err()
		case errors.Is(err, cloud.ErrNoRawFile):
			wo.Unavailable = append(wo.Unavailable, f.name)
			continue
		case err != nil:
			wo.FileErrors[f.name] = err.Error()
			continue
		}
		parsed, err := cloud.ParseRawFile(id, data)
		if err != nil {
			wo.FileErrors[f.name] = "parse: " + err.Error()
			continue
		}
		wo.Files[f.name] = parsed
	}
	return nil
}
