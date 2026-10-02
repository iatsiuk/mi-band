package cloud

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestLogoutEndsAccountSession(t *testing.T) {
	t.Parallel()
	f := newFakeCloud(t)
	calls := 0
	f.extra = map[string]http.HandlerFunc{"/pass/logout": func(w http.ResponseWriter, r *http.Request) {
		calls++
		if q := r.URL.Query(); q.Get("userId") != testUserID || q.Get("callback") != f.srv.URL || len(q) != 2 {
			t.Errorf("logout query: %v", q)
		}
		c := cookies(r)
		if c["passToken"] != testPassToken || c["userId"] != testUserID || c["cUserId"] != testCUserID || c["deviceId"] != testDeviceID {
			t.Errorf("logout cookies: %v", c)
		}
		http.SetCookie(w, testCookie("passToken", "EXPIRED", -1))
		// the fan-out over the other Xiaomi domains is for browser cookies only and must not be followed
		w.Header().Set("Location", "http://logout.invalid/logout")
		w.WriteHeader(http.StatusFound)
	}}
	s, err := Resume(&Session{UserID: testUserID, CUserID: testCUserID, PassToken: testPassToken, ServiceToken: testToken}, f.opts())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("logout calls: %d", calls)
	}
}

func TestLogoutReportsServerErrorWithoutToken(t *testing.T) {
	t.Parallel()
	f := newFakeCloud(t)
	f.extra = map[string]http.HandlerFunc{"/pass/logout": func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}}
	s, err := Resume(&Session{UserID: testUserID, PassToken: testPassToken}, f.opts())
	if err != nil {
		t.Fatal(err)
	}
	err = s.Logout(context.Background())
	if err == nil || strings.Contains(err.Error(), testPassToken) || strings.Contains(err.Error(), testUserID) {
		t.Fatalf("got %v, want an error without the token or user id", err)
	}
}
