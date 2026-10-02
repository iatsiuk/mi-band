package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFitnessDataRejectsRepeatedCursor(t *testing.T) {
	d, s := newDataServer(t)
	page := func(next string) string {
		return `{"code":0,"result":{"data_list":[],"next_key":"` + next + `","has_more":true}}`
	}
	d.fitness = map[string]string{"": page("a"), "a": page("b"), "b": page("a")}
	_, err := s.FitnessData(context.Background(), time.Unix(1, 0), time.Unix(2, 0), "steps")
	if !errors.Is(err, ErrIncompletePages) {
		t.Fatalf("got %v", err)
	}
}

func TestNullValuesDecodeToNothing(t *testing.T) {
	for _, v := range []string{"null", `"null"`, `"x"`} {
		r := Record{Key: "steps", Value: json.RawMessage(v)}
		if got, err := r.Decode(); got != nil || err != nil {
			t.Errorf("value %s: got %v, %v", v, got, err)
		}
	}
}

func TestWorkoutWithNullValueHasNoReport(t *testing.T) {
	d, s := newDataServer(t)
	d.sport = map[string]string{
		"": `{"code":0,"result":{"sport_records":[{"sid":"1","key":"outdoor_walking","time":1759371234,"value":"null"}],"has_more":false}}`,
	}
	ws, err := s.Workouts(context.Background(), time.Unix(1, 0), time.Unix(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(ws) != 1 || ws[0].Report != nil {
		t.Fatalf("got %+v", ws)
	}
}

func TestFDSDownloadFollowsRedirectAndRejectsOversize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, "/object", http.StatusFound)
		case "/object":
			_, _ = fmt.Fprint(w, "0123456789")
		default:
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	defer srv.Close()
	c := &http.Client{}
	b, err := download(context.Background(), c, srv.URL+"/redirect", 64)
	if err != nil || string(b) != "0123456789" {
		t.Fatalf("got %q, %v", b, err)
	}
	if _, err := download(context.Background(), c, srv.URL+"/object", 5); err == nil || !strings.Contains(err.Error(), "larger") {
		t.Fatalf("oversize: %v", err)
	}
	if _, err := download(context.Background(), c, srv.URL+"/denied?sig=secret", 64); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("status error: %v", err)
	}
}

func TestFDSDownloadErrorHidesRedirectLocation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://example.invalid/%zz?signature=SECRET")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()
	_, err := download(context.Background(), &http.Client{}, srv.URL+"/object", 64)
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("got %v", err)
	}
}

func TestFDSDownloadKeepsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := download(ctx, &http.Client{}, "https://example.invalid/x", 64)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}
