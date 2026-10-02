package cloud

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

const (
	testContext  = "ctx-abc"
	testSession  = "is-123"
	testCode     = "123456"
	testMasked   = "u***@example.com"
	notifyReply  = `{"code":0,"securityStatus":16,"notificationUrl":"/fe/service/identity/authStart?sid=miothealth&context=` + testContext + `&_locale=en_US"}`
	identityPath = "/identity/auth/verifyEmail"
)

// identityFake adds the identity check of account.xiaomi.com to fakeCloud; replies, cookies and
// redirects follow a browser HAR of the email check.
type identityFake struct {
	*fakeCloud
	options   string
	listReply string // replaces the list reply
	navPage   string // page the navigations after the check leave from
	trustBox  bool
	sent      int
	tries     int
}

func newIdentityFake(t *testing.T) *identityFake {
	f := &identityFake{fakeCloud: newFakeCloud(t), options: "[8]", navPage: pageEmail}
	f.auth2Reply = notifyReply
	f.extra = map[string]http.HandlerFunc{
		"/identity/list":                 f.list,
		identityPath:                     f.verifyEmail,
		"/identity/auth/sendEmailTicket": f.sendTicket,
		"/identity/result/check":         f.resultCheck,
		"/pass/serviceLoginAuth2/end":    f.passEnd,
		"/healthapp/sts": func(w http.ResponseWriter, r *http.Request) {
			f.checkNavigation(r)
			_, _ = fmt.Fprint(w, "ok")
		},
	}
	return f
}

const (
	pageStart  = "authStart"   // the page of the list and page-data calls
	pageEmail  = "verifyEmail" // the page of the ticket and verify calls and the navigations after them
	pageQuery  = "?sid=miothealth&context=" + testContext + "&_locale=en_US"
	htmlAccept = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"
)

// checkPage checks what the check page in the Mi Fitness webview sends with every identity call:
// the webview UA and cookies, XHR headers, and the page it runs on as Referer, as in the HAR.
func (f *identityFake) checkPage(r *http.Request, page string) {
	t := f.t
	f.checkWebView(r, page)
	if r.Header.Get("X-Requested-With") != "XMLHttpRequest" || r.Header.Get("Accept") != "application/json, text/plain, */*" {
		t.Errorf("%s %s headers: %v", r.Method, r.URL.Path, r.Header)
	}
	if r.Method == http.MethodPost && r.Header.Get("Origin") != f.srv.URL {
		t.Errorf("%s %s origin: %q", r.Method, r.URL.Path, r.Header.Get("Origin"))
	}
}

// checkNavigation checks a page navigation after the check: html Accept, no XHR headers, and no
// identity_session outside its /identity path.
func (f *identityFake) checkNavigation(r *http.Request) {
	t := f.t
	f.checkWebView(r, f.navPage)
	if r.Header.Get("Accept") != htmlAccept || r.Header.Get("X-Requested-With") != "" || r.Header.Get("Origin") != "" {
		t.Errorf("%s %s headers: %v", r.Method, r.URL.Path, r.Header)
	}
	_, inIdentity := cookies(r)["identity_session"]
	if inIdentity != strings.HasPrefix(r.URL.Path, "/identity/") {
		t.Errorf("%s %s cookies: %v", r.Method, r.URL.Path, cookies(r))
	}
}

func (f *identityFake) checkWebView(r *http.Request, page string) {
	t := f.t
	if want := f.srv.URL + "/fe/service/identity/" + page + pageQuery; r.Header.Get("Referer") != want {
		t.Errorf("%s %s referer %q, want %q", r.Method, r.URL.Path, r.Header.Get("Referer"), want)
	}
	ua := r.UserAgent()
	if !strings.HasPrefix(ua, "Mozilla/5.0 (Linux; Android 14; 23127PN0CG") || !strings.Contains(ua, " APP/mi.health APPV/909772 XiaoMi/HybridView/ MK/") ||
		!strings.HasSuffix(ua, " SDKV/5.3.0.release.66 WebViewType/PassportJSBWebView") {
		t.Errorf("%s %s user agent: %q", r.Method, r.URL.Path, ua)
	}
	c := cookies(r)
	if c["deviceId"] != testDeviceID || c["NativeUserAgent"] != b64.EncodeToString([]byte(ua)) {
		t.Errorf("%s %s cookies: %v", r.Method, r.URL.Path, c)
	}
}

func (f *identityFake) checkSession(r *http.Request, page string) {
	f.checkPage(r, page)
	if c := cookies(r); c["identity_session"] != testSession {
		f.t.Errorf("%s %s cookies: %v", r.Method, r.URL.Path, c)
	}
}

func (f *identityFake) form(r *http.Request) url.Values {
	if err := r.ParseForm(); err != nil {
		f.t.Fatal(err)
	}
	if r.Method != http.MethodPost || r.URL.Query().Get("_dc") == "" {
		f.t.Errorf("%s %s: want POST with _dc", r.Method, r.URL)
	}
	return r.PostForm
}

func (f *identityFake) list(w http.ResponseWriter, r *http.Request) {
	want := url.Values{"sid": {"miothealth"}, "supportedMask": {"0"}, "_locale": {"en_US"}, "context": {testContext}}
	if fmt.Sprint(r.URL.Query()) != fmt.Sprint(want) {
		f.t.Errorf("identity list query: %v", r.URL.Query())
	}
	f.checkPage(r, pageStart)
	http.SetCookie(w, pathCookie("identity_session", testSession, "/identity"))
	if f.listReply != "" {
		_, _ = fmt.Fprint(w, f.listReply)
		return
	}
	// code 2 is the normal reply: pick one of the options
	_, _ = fmt.Fprintf(w, `%s{"code":2,"flag":8,"option":8,"options":%s,"version":"v1","showFastUpdateEmailLink":false,`+
		`"externalId":"x","retrieveType":"bind","trustCheckBox":%t,"trustCheckBoxSelected":false,"directVerify":true,"applyToken":"t"}`,
		safePrefix, f.options, f.trustBox)
}

func (f *identityFake) verifyEmail(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		f.checkSession(r, pageStart)
		if q := r.URL.Query(); q.Get("_flag") != "8" || q.Get("_json") != "true" {
			f.t.Errorf("verifyEmail page query: %v", q)
		}
		_, _ = fmt.Fprintf(w, `{"code":0,"description":"成功","maskedEmail":%q,"contentType":160040}`, testMasked)
		return
	}
	f.checkSession(r, pageEmail)
	form := f.form(r)
	f.tries++
	if form.Get("_flag") != "8" || form.Get("trust") != fmt.Sprint(f.trustBox) || form.Get("_json") != "true" {
		f.t.Errorf("verifyEmail form: %v", form)
	}
	switch form.Get("ticket") {
	case testCode:
	case "":
		_, _ = fmt.Fprint(w, `{"code":70012,"desc":"empty ticket"}`)
		return
	default:
		_, _ = fmt.Fprint(w, `{"code":70014,"desc":"wrong ticket"}`)
		return
	}
	_, _ = fmt.Fprintf(w, `{"code":0,"location":%q}`, f.srv.URL+"/identity/result/check?sid=miothealth")
}

func (f *identityFake) sendTicket(w http.ResponseWriter, r *http.Request) {
	f.checkSession(r, pageEmail)
	want := url.Values{"retry": {"0"}, "icode": {""}, "_json": {"true"}}
	if form := f.form(r); fmt.Sprint(form) != fmt.Sprint(want) {
		f.t.Errorf("sendEmailTicket form: %v", form)
	}
	f.sent++
	_, _ = fmt.Fprint(w, `{"code":0,"desc":"成功"}`)
}

func (f *identityFake) resultCheck(w http.ResponseWriter, r *http.Request) {
	f.checkNavigation(r)
	http.Redirect(w, r, "/pass/serviceLoginAuth2/end?sid=miothealth", http.StatusFound)
}

// passEnd sets the login cookies one redirect after the check, as the browser trace shows.
func (f *identityFake) passEnd(w http.ResponseWriter, r *http.Request) {
	f.checkNavigation(r)
	http.SetCookie(w, pathCookie("passToken", testPassToken, "/"))
	http.SetCookie(w, pathCookie("cUserId", testCUserID, "/"))
	http.SetCookie(w, pathCookie("userId", testUserID, "/"))
	http.SetCookie(w, &http.Cookie{Name: "passInfo", Value: "login-end", Path: "/", SameSite: http.SameSiteLaxMode}) //nolint:gosec // plain http test fixture
	http.Redirect(w, r, "/healthapp/sts", http.StatusFound)
}

// pathCookie is not Secure: the fixture is plain http and the jar of Go 1.25 drops Secure cookies there.
func pathCookie(name, value, path string) *http.Cookie {
	return &http.Cookie{Name: name, Value: value, Path: path, HttpOnly: true, SameSite: http.SameSiteLaxMode} //nolint:gosec // plain http test fixture
}

func notification(t *testing.T, f *identityFake) *NotificationError {
	t.Helper()
	_, err := Login(context.Background(), testUser, testPassword, f.opts())
	var ne *NotificationError
	if !errors.As(err, &ne) {
		t.Fatalf("got %v, want NotificationError", err)
	}
	return ne
}

func TestEmailCodeFinishesLogin(t *testing.T) {
	t.Parallel()
	f := newIdentityFake(t)
	v, err := notification(t, f).SendEmailCode(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if v.MaskedEmail != testMasked || f.sent != 1 {
		t.Fatalf("masked email %q, tickets sent %d", v.MaskedEmail, f.sent)
	}
	for _, code := range []string{"000000", ""} {
		if _, err := v.Finish(context.Background(), code); !errors.Is(err, ErrWrongCode) {
			t.Fatalf("code %q: got %v, want ErrWrongCode", code, err)
		}
	}
	s, err := v.Finish(context.Background(), testCode)
	if err != nil {
		t.Fatal(err)
	}
	if s.UserID != testUserID || s.PassToken != testPassToken || s.ServiceToken != testToken || s.DeviceID() != testDeviceID {
		t.Fatalf("session: %+v", s)
	}
	if f.tries != 3 {
		t.Errorf("verify attempts: %d", f.tries)
	}
}

func TestEmailCodeNeedsEmailOption(t *testing.T) {
	t.Parallel()
	f := newIdentityFake(t)
	f.options = "[4,1]"
	_, err := notification(t, f).SendEmailCode(context.Background())
	if !errors.Is(err, ErrNoEmailVerification) {
		t.Fatalf("got %v, want ErrNoEmailVerification", err)
	}
	if f.sent != 0 {
		t.Errorf("tickets sent: %d", f.sent)
	}
}

func TestEmailCodeReportsIdentityErrors(t *testing.T) {
	t.Parallel()
	f := newIdentityFake(t)
	f.extra["/identity/auth/sendEmailTicket"] = func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"code":70022,"desc":"see /identity/result/check?identityToken=SECRET"}`)
	}
	_, err := notification(t, f).SendEmailCode(context.Background())
	var re *ResponseError
	if !errors.As(err, &re) || re.Code != 70022 {
		t.Fatalf("got %v, want ResponseError 70022", err)
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Errorf("server desc leaks into the error: %v", err)
	}
}

func TestEmailCodeWithoutPassTokenFails(t *testing.T) {
	t.Parallel()
	f := newIdentityFake(t)
	f.extra["/pass/serviceLoginAuth2/end"] = func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }
	v, err := notification(t, f).SendEmailCode(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Finish(context.Background(), testCode); err == nil {
		t.Fatal("want error without passToken")
	}
}

func TestEmailCodeAcceptsCookiesBeforeFailedLanding(t *testing.T) {
	t.Parallel()
	f := newIdentityFake(t)
	f.extra["/healthapp/sts"] = func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) }
	v, err := notification(t, f).SendEmailCode(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s, err := v.Finish(context.Background(), testCode); err != nil || s.PassToken != testPassToken {
		t.Fatalf("got %+v, %v", s, err)
	}
}

func TestEmailCodeRejectsBadReplies(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, path, reply string
		finish            bool
	}{
		{"malformed identity list", "/identity/list", `{"code":`, false},
		{"identity list error", "/identity/list", `{"code":10012,"options":[8]}`, false},
		{"page data refused", identityPath, `{"code":70016}`, false},
		{"verify without location", identityPath, `{"code":0}`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newIdentityFake(t)
			orig := f.extra[tt.path]
			f.extra[tt.path] = func(w http.ResponseWriter, r *http.Request) {
				if tt.path == identityPath && tt.finish != (r.Method == http.MethodPost) {
					orig(w, r)
					return
				}
				_, _ = fmt.Fprint(w, tt.reply)
			}
			v, err := notification(t, f).SendEmailCode(context.Background())
			if !tt.finish {
				if err == nil {
					t.Fatal("want error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := v.Finish(context.Background(), testCode); err == nil {
				t.Fatal("want error")
			}
		})
	}
}

func TestEmailCodeStopsOnCancel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, path string
		finish     bool
	}{
		{"identity list", "/identity/list", false},
		{"send ticket", "/identity/auth/sendEmailTicket", false},
		{"verify code", identityPath, true},
		{"redirect chain", "/pass/serviceLoginAuth2/end", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newIdentityFake(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			orig := f.extra[tt.path]
			f.extra[tt.path] = func(w http.ResponseWriter, r *http.Request) {
				if tt.path == identityPath && r.Method != http.MethodPost {
					orig(w, r)
					return
				}
				// the server notices the closed connection only after the body is read
				_, _ = io.Copy(io.Discard, r.Body)
				cancel()
				select {
				case <-r.Context().Done():
				case <-time.After(5 * time.Second):
				}
			}
			ne := notification(t, f)
			var err error
			if tt.finish {
				var v *EmailVerification
				if v, err = ne.SendEmailCode(context.Background()); err != nil {
					t.Fatal(err)
				}
				_, err = v.Finish(ctx, testCode)
			} else {
				_, err = ne.SendEmailCode(ctx)
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("got %v, want context.Canceled", err)
			}
		})
	}
}

func TestEmailCodeLogsStepsWithoutSecrets(t *testing.T) {
	t.Parallel()
	f := newIdentityFake(t)
	var lines []string
	o := f.opts()
	o.Logf = func(format string, args ...any) { lines = append(lines, fmt.Sprintf(format, args...)) }
	_, err := Login(context.Background(), testUser, testPassword, o)
	var ne *NotificationError
	if !errors.As(err, &ne) {
		t.Fatalf("got %v", err)
	}
	if _, err := ne.SendEmailCode(context.Background()); err != nil {
		t.Fatal(err)
	}
	log := strings.Join(lines, "\n")
	for _, want := range []string{"GET /identity/list: code 2", "GET /identity/auth/verifyEmail: code 0", "POST /identity/auth/sendEmailTicket: code 0"} {
		if !strings.Contains(log, want) {
			t.Errorf("log has no %q:\n%s", want, log)
		}
	}
	if strings.Contains(log, testContext) || strings.Contains(log, testMasked) {
		t.Errorf("log leaks request or reply data:\n%s", log)
	}
}

func TestEmailCodeTrustsDeviceWhenOffered(t *testing.T) {
	t.Parallel()
	f := newIdentityFake(t)
	f.trustBox = true
	v, err := notification(t, f).SendEmailCode(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Finish(context.Background(), testCode); err != nil {
		t.Fatal(err)
	}
}

func TestEmailCodeSkippedWhenCheckIsPassed(t *testing.T) {
	t.Parallel()
	f := newIdentityFake(t)
	f.listReply = fmt.Sprintf(`{"code":0,"location":%q}`, f.srv.URL+"/identity/result/check?sid=miothealth")
	// the page navigates from authStart, it never reaches the email page
	f.navPage = pageStart
	v, err := notification(t, f).SendEmailCode(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if v.CodeSent || f.sent != 0 {
		t.Fatalf("code sent %v, tickets %d", v.CodeSent, f.sent)
	}
	s, err := v.Finish(context.Background(), "")
	if err != nil || s.PassToken != testPassToken || f.tries != 0 {
		t.Fatalf("got %+v, %v after %d verify calls", s, err, f.tries)
	}
}

func TestEmailCodeTimesOut(t *testing.T) {
	t.Parallel()
	f := newIdentityFake(t)
	f.extra["/identity/auth/sendEmailTicket"] = func(_ http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}
	ne := notification(t, f)
	ne.opts.HTTPClient = &http.Client{Timeout: 100 * time.Millisecond}
	start := time.Now()
	_, err := ne.SendEmailCode(context.Background())
	if err == nil || !strings.Contains(err.Error(), "timeout") || time.Since(start) > 3*time.Second {
		t.Fatalf("got %v after %v, want a timeout", err, time.Since(start))
	}
}

func TestEmailCodeStopsRedirectLoop(t *testing.T) {
	t.Parallel()
	f := newIdentityFake(t)
	f.extra["/identity/result/check"] = func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/identity/result/check", http.StatusFound)
	}
	v, err := notification(t, f).SendEmailCode(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Finish(context.Background(), testCode); err == nil {
		t.Fatal("want error for a redirect loop")
	}
}
