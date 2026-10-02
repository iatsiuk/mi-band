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

func TestDevicesRejectsCursorCycle(t *testing.T) {
	f := newFakeCloud(t)
	page := func(next string) string {
		return `{"code":0,"result":{"list":[{"sid":"` + next + `","model":"m"}],"page_next":"` + next + `"}}`
	}
	f.pages = map[string]string{"": page("a"), "a": page("b"), "b": page("a")}
	s, err := LoginWithPassToken(context.Background(), testUserID, testPassToken, f.opts())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Devices(context.Background()); !errors.Is(err, ErrIncompletePages) {
		t.Fatalf("got %v, want ErrIncompletePages", err)
	}
}

func TestDevicesReportsPageLimit(t *testing.T) {
	f := newFakeCloud(t)
	f.pages = map[string]string{}
	for i := range maxDevicePages {
		f.pages[pageKey(i)] = `{"code":0,"result":{"list":[],"page_next":"` + pageKey(i+1) + `"}}`
	}
	s, err := LoginWithPassToken(context.Background(), testUserID, testPassToken, f.opts())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Devices(context.Background()); !errors.Is(err, ErrTooManyPages) {
		t.Fatalf("got %v, want ErrTooManyPages", err)
	}
}

func pageKey(i int) string {
	if i == 0 {
		return ""
	}
	return fmt.Sprintf("p%d", i)
}

func TestBadStsLocationDoesNotLeakClientSign(t *testing.T) {
	f := newFakeCloud(t)
	f.stsLocation = "https://sts.example/%zz?ticket=SECRET"
	_, err := LoginWithPassToken(context.Background(), testUserID, testPassToken, f.opts())
	if err == nil {
		t.Fatal("expected error")
	}
	if msg := err.Error(); strings.Contains(msg, "SECRET") || strings.Contains(msg, "clientSign") {
		t.Fatalf("error leaks the sts url: %s", msg)
	}
}

func TestBadHealthURLDoesNotLeakSignedQuery(t *testing.T) {
	s, err := Resume(&Session{UserID: "1", CUserID: "c", ServiceToken: "tok", Ssecurity: testSsecurity},
		&Options{DeviceID: testDeviceID, Region: "a b"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.FitnessData(context.Background(), time.Unix(1, 0), time.Unix(2, 0), "steps")
	if err == nil {
		t.Fatal("expected error")
	}
	if msg := err.Error(); strings.Contains(msg, "signature") || strings.Contains(msg, "rc4_hash__") || strings.Contains(msg, "?") {
		t.Fatalf("error leaks the signed query: %s", msg)
	}
}
