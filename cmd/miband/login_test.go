package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"miband/internal/cloud"
)

const (
	fakeUser      = "me@example.com"
	fakeUserID    = "42"
	fakePassToken = "pt"
	fakeCode      = "222222"
)

// fakeCookie is not Secure: the fixture is plain http and the jar of Go 1.25 drops Secure cookies there.
func fakeCookie(name, value string) *http.Cookie {
	return &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode} //nolint:gosec // plain http test fixture
}

// fakeAccount configures newFakeAccount: the identity options offered, the sendEmailTicket reply,
// and a list reply that reports the check as passed.
type fakeAccount struct {
	options     string
	ticketReply string
	passed      bool
}

// newFakeAccount serves the password login, the email identity check and the sts step.
func newFakeAccount(t *testing.T, fa fakeAccount) *httptest.Server {
	t.Helper()
	if fa.ticketReply == "" {
		fa.ticketReply = `{"code":0}`
	}
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/pass/serviceLogin", func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("passToken"); err == nil && c.Value == fakePassToken {
			_, _ = fmt.Fprintf(w, `{"code":0,"ssecurity":"ss","nonce":1,"location":%q}`, srv.URL+"/sts")
			return
		}
		_, _ = fmt.Fprint(w, `{"code":70016,"_sign":"s","qs":"q","callback":"c"}`)
	})
	mux.HandleFunc("/pass/serviceLoginAuth2", func(w http.ResponseWriter, r *http.Request) {
		if r.PostFormValue("user") != fakeUser {
			t.Errorf("login user %q", r.PostFormValue("user"))
		}
		_, _ = fmt.Fprint(w, `{"code":0,"securityStatus":16,"notificationUrl":"/fe/service/identity/authStart?sid=miothealth&context=c1"}`)
	})
	mux.HandleFunc("/identity/list", func(w http.ResponseWriter, _ *http.Request) {
		if fa.passed {
			_, _ = fmt.Fprintf(w, `{"code":0,"location":%q}`, srv.URL+"/identity/result/check")
			return
		}
		_, _ = fmt.Fprintf(w, `{"code":2,"flag":8,"option":8,"options":%s,"retrieveType":"bind","trustCheckBox":false,"trustCheckBoxSelected":false,"directVerify":true}`, fa.options)
	})
	mux.HandleFunc("/identity/auth/sendEmailTicket", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, fa.ticketReply)
	})
	mux.HandleFunc("/identity/auth/verifyEmail", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet:
			_, _ = fmt.Fprint(w, `{"code":0,"description":"成功","maskedEmail":"m***@example.com","contentType":160040}`)
		case r.PostFormValue("ticket") == fakeCode:
			_, _ = fmt.Fprintf(w, `{"code":0,"location":%q}`, srv.URL+"/identity/result/check")
		case r.PostFormValue("ticket") == "":
			t.Error("empty code sent to the server")
			_, _ = fmt.Fprint(w, `{"code":70012}`)
		default:
			_, _ = fmt.Fprint(w, `{"code":70014}`)
		}
	})
	mux.HandleFunc("/identity/result/check", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/pass/serviceLoginAuth2/end", http.StatusFound)
	})
	mux.HandleFunc("/pass/serviceLoginAuth2/end", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, fakeCookie("passToken", fakePassToken))
		http.SetCookie(w, fakeCookie("userId", fakeUserID))
		http.Redirect(w, r, "/healthapp/sts", http.StatusFound)
	})
	mux.HandleFunc("/healthapp/sts", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "ok")
	})
	mux.HandleFunc("/sts", func(w http.ResponseWriter, _ *http.Request) {
		http.SetCookie(w, fakeCookie("serviceToken", "st"))
		w.WriteHeader(http.StatusFound)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	testServer = srv.URL
	t.Cleanup(func() { testServer = "" })
	return srv
}

func TestLoginPromptsForRegionAccountPasswordAndEmailCode(t *testing.T) {
	tests := []struct {
		name, region, wantRegion string
	}{
		{"region given", "de", "de"},
		{"empty region is china", "", "cn"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newFakeAccount(t, fakeAccount{options: "[4,8]"})
			in := tt.region + "\n" + fakeUser + "\nsecret\n111111\n \n" + fakeCode + "\n"
			_, stderr, err := executeIn(t, in, "login")
			if err != nil {
				t.Fatalf("%v\n%s", err, stderr)
			}
			for _, want := range []string{"region", "e.g. de", "account", "code sent to m***@example.com", "wrong code", "empty code", "logged in as " + fakeUserID} {
				if !strings.Contains(stderr, want) {
					t.Errorf("stderr has no %q:\n%s", want, stderr)
				}
			}
			p, err := sessionPath()
			if err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			var saved savedSession
			if err := json.Unmarshal(b, &saved); err != nil {
				t.Fatal(err)
			}
			if saved.Region != tt.wantRegion || saved.Session.PassToken != fakePassToken || saved.Session.ServiceToken != "st" {
				t.Fatalf("saved %s", b)
			}
		})
	}
}

func TestLoginFlagsSkipPrompts(t *testing.T) {
	newFakeAccount(t, fakeAccount{options: "[8]"})
	_, stderr, err := executeIn(t, "secret\n"+fakeCode+"\n", "login", "--region", "sg", "--user", fakeUser)
	if err != nil {
		t.Fatalf("%v\n%s", err, stderr)
	}
	if strings.Contains(stderr, "region of") || strings.Contains(stderr, "account (") {
		t.Errorf("prompted for a flag value:\n%s", stderr)
	}
}

func TestLoginWithoutEmailCodePointsToBrowser(t *testing.T) {
	newFakeAccount(t, fakeAccount{options: "[4]"})
	_, _, err := executeIn(t, "secret\n", "login", "--region", "sg", "--user", fakeUser)
	var ne *cloud.NotificationError
	if !errors.As(err, &ne) || !strings.Contains(err.Error(), "--pass-token") || exitCode(err) != exitAuth {
		t.Fatalf("got %v, want browser hint", err)
	}
}

func TestLoginGivesUpAfterWrongCodes(t *testing.T) {
	newFakeAccount(t, fakeAccount{options: "[8]"})
	_, _, err := executeIn(t, "secret\n1\n2\n3\n"+fakeCode+"\n", "login", "--region", "sg", "--user", fakeUser)
	if !errors.Is(err, cloud.ErrWrongCode) || exitCode(err) != exitAuth {
		t.Fatalf("got %v, want ErrWrongCode", err)
	}
}

func TestLoginFallsBackToBrowserWhenEmailCheckFails(t *testing.T) {
	newFakeAccount(t, fakeAccount{options: "[8]", ticketReply: `{"code":70022,"desc":"no chances"}`})
	_, _, err := executeIn(t, "secret\n", "login", "--region", "sg", "--user", fakeUser)
	var ne *cloud.NotificationError
	if !errors.As(err, &ne) || exitCode(err) != exitAuth {
		t.Fatalf("got %v, want NotificationError", err)
	}
	for _, want := range []string{"70022", "authStart", "--pass-token"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error has no %q: %v", want, err)
		}
	}
}

func TestLoginSkipsCodeWhenCheckIsPassed(t *testing.T) {
	newFakeAccount(t, fakeAccount{passed: true})
	_, stderr, err := executeIn(t, "secret\n", "login", "--region", "sg", "--user", fakeUser)
	if err != nil {
		t.Fatalf("%v\n%s", err, stderr)
	}
	if strings.Contains(stderr, "code sent") || !strings.Contains(stderr, "logged in as "+fakeUserID) {
		t.Errorf("stderr:\n%s", stderr)
	}
}
