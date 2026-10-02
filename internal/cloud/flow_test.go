package cloud

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testUser      = "user@example.com"
	testPassword  = "P@ss w0rd!"
	testDeviceID  = "android_test"
	testUserID    = "1234567"
	testCUserID   = "cU-abc"
	testPassToken = "pt-xyz"
	testToken     = "st-123"
	testSsecurity = "MTg/Rk1UW2JpcHd+hYyTmg=="
	testNonce     = "7065373842716454912"
)

// fakeCloud emulates account.xiaomi.com and hlth.io.mi.com on one httptest server.
type fakeCloud struct {
	t   *testing.T
	srv *httptest.Server

	mu sync.Mutex
	// overrides the serviceLoginAuth2 json reply
	auth2Reply string
	// overrides the sts location of a successful login
	stsLocation string
	// pages served by the device list, keyed by page_start
	pages map[string]string
	// number of 401s to answer before serving the device list, with a Date header skew
	unauthorized int
	skew         time.Duration
	// observed request details
	listRequests []url.Values
	headers      []http.Header
	// extra handlers keyed by url path, run with mu held
	extra map[string]http.HandlerFunc
}

func newFakeCloud(t *testing.T) *fakeCloud {
	f := &fakeCloud{t: t, pages: map[string]string{
		"": `{"code":0,"result":{"list":[{"sid":"940134049","model":"miwear.watch.o66","name":"Xiaomi Smart Band 10","pd_id":12345,` +
			`"detail":{"mac":"aa:bb:cc:dd:ee:ff","sn":"SN1","encrypt_key":"00112233445566778899aabbccddeeff"}}],"page_next":"p2"}}`,
		"p2": `{"code":0,"result":{"list":[{"sid":"2","model":"m2","name":"n2","detail":{"mac":"11:22:33:44:55:66","random_mac":"de:ad:be:ef:00:01","sn":"SN2","encrypt_key":"ffeeddccbbaa99887766554433221100"}}],"page_next":""}}`,
	}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeCloud) opts() *Options {
	return &Options{DeviceID: testDeviceID, AccountURL: f.srv.URL, HealthURL: f.srv.URL}
}

func cookies(r *http.Request) map[string]string {
	m := map[string]string{}
	for _, c := range r.Cookies() {
		m[c.Name] = c.Value
	}
	return m
}

func (f *fakeCloud) serve(w http.ResponseWriter, r *http.Request) {
	t := f.t
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.URL.Path {
	case "/pass/serviceLogin":
		q := r.URL.Query()
		if q.Get("sid") != "miothealth" || q.Get("_json") != "true" || q.Get("_locale") != "en_US" {
			t.Errorf("serviceLogin query: %v", q)
		}
		c := cookies(r)
		if c["deviceId"] != testDeviceID {
			t.Errorf("serviceLogin cookies: %v", c)
		}
		if !strings.Contains(r.UserAgent(), " APP/mi.health APPV/909772 ") || !strings.Contains(r.UserAgent(), " SDKV/5.3.0.release.66") {
			t.Errorf("passport user agent: %q", r.UserAgent())
		}
		if c["passToken"] == testPassToken && c["userId"] == testUserID {
			f.loginOK(w)
			return
		}
		if c["userId"] != testUser {
			t.Errorf("serviceLogin userId cookie: %v", c)
		}
		_, _ = fmt.Fprint(w, safePrefix+`{"code":70016,"desc":"登录验证失败","_sign":"SIGN+/=","qs":"%3Fsid%3Dmiothealth","callback":"https://sts.example/cb","captchaUrl":null}`)
	case "/pass/serviceLoginAuth2":
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		want := url.Values{
			"hash": {passwordHash(testPassword)}, "user": {testUser}, "sid": {"miothealth"}, "_json": {"true"},
			"_locale": {"en_US"}, "_sign": {"SIGN+/="}, "qs": {"%3Fsid%3Dmiothealth"}, "callback": {"https://sts.example/cb"},
		}
		if fmt.Sprint(r.PostForm) != fmt.Sprint(want) {
			t.Errorf("serviceLoginAuth2 form:\n got %v\nwant %v", r.PostForm, want)
		}
		if cookies(r)["deviceId"] != testDeviceID {
			t.Errorf("serviceLoginAuth2 cookies: %v", r.Header.Get("Cookie"))
		}
		if f.auth2Reply != "" {
			_, _ = fmt.Fprint(w, safePrefix+f.auth2Reply)
			return
		}
		f.loginOK(w)
	case "/sts":
		q := r.URL.Query()
		if q.Get("d") != "1" || q.Get("_userIdNeedEncrypt") != "true" || q.Get("clientSign") != clientSign(testNonce, testSsecurity) {
			t.Errorf("sts query: %v", q)
		}
		http.SetCookie(w, testCookie("serviceToken", "EXPIRED", -1))
		http.SetCookie(w, testCookie("serviceToken", testToken, 0))
		w.Header().Set("Location", "https://elsewhere.example/")
		w.WriteHeader(http.StatusFound)
	case deviceListPath:
		f.headers = append(f.headers, r.Header.Clone())
		if f.unauthorized > 0 {
			f.unauthorized--
			w.Header().Set("Date", time.Now().Add(f.skew).UTC().Format(http.TimeFormat))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		f.listRequests = append(f.listRequests, r.PostForm)
		data := f.verify(http.MethodPost, r.URL.Path, r.PostForm)
		var req struct {
			PageSize  int    `json:"page_size"`
			PageStart string `json:"page_start"`
		}
		if err := json.Unmarshal([]byte(data), &req); err != nil || req.PageSize != 50 {
			t.Errorf("device list data: %s", data)
		}
		page, ok := f.pages[req.PageStart]
		if !ok {
			page = `{"code":-1,"message":"no such page"}`
		}
		f.respondEncrypted(w, r.PostForm.Get("_nonce"), page)
	default:
		if h, ok := f.extra[r.URL.Path]; ok {
			h(w, r)
			return
		}
		t.Errorf("unexpected request %s %s", r.Method, r.URL)
		http.NotFound(w, r)
	}
}

func (f *fakeCloud) loginOK(w http.ResponseWriter) {
	http.SetCookie(w, testCookie("userId", testUserID, 0))
	http.SetCookie(w, testCookie("passToken", testPassToken, 0))
	http.SetCookie(w, testCookie("cUserId", testCUserID, 0))
	loc := f.srv.URL + "/sts?d=1"
	if f.stsLocation != "" {
		loc = f.stsLocation
	}
	_, _ = fmt.Fprintf(w, safePrefix+`{"code":0,"desc":"成功","securityStatus":0,"ssecurity":%q,"psecurity":"ps","nonce":%s,"location":%q}`,
		testSsecurity, testNonce, loc)
}

// verify checks signature and rc4_hash__ the way the server must and returns the plaintext data.
// path is the signed path; form holds the POST form or the GET query.
func (f *fakeCloud) verify(method, path string, form url.Values) string {
	t := f.t
	nonce := form.Get("_nonce")
	signed, err := signedNonce(testSsecurity, nonce)
	if err != nil {
		t.Fatal(err)
	}
	if len(form) != 4 {
		t.Errorf("form fields: %v", form)
	}
	enc := map[string]string{"data": form.Get("data"), "rc4_hash__": form.Get("rc4_hash__")}
	if got := sha1Sign(method, path, enc, signed); got != form.Get("signature") {
		t.Errorf("signature %s, want %s", form.Get("signature"), got)
	}
	c, err := rc4Stream(signed)
	if err != nil {
		t.Fatal(err)
	}
	plain := map[string]string{}
	for _, k := range []string{"data", "rc4_hash__"} {
		b, err := b64.DecodeString(enc[k])
		if err != nil {
			t.Fatal(err)
		}
		c.XORKeyStream(b, b)
		plain[k] = string(b)
	}
	if want := sha1Sign(method, path, map[string]string{"data": plain["data"]}, signed); plain["rc4_hash__"] != want {
		t.Errorf("rc4_hash__ %s, want %s", plain["rc4_hash__"], want)
	}
	return plain["data"]
}

func (f *fakeCloud) respondEncrypted(w http.ResponseWriter, nonce, body string) {
	signed, _ := signedNonce(testSsecurity, nonce)
	c, err := rc4Stream(signed)
	if err != nil {
		f.t.Fatal(err)
	}
	b := []byte(body)
	c.XORKeyStream(b, b)
	_, _ = io.WriteString(w, b64.EncodeToString(b))
}

func testCookie(name, value string, maxAge int) *http.Cookie {
	return &http.Cookie{Name: name, Value: value, MaxAge: maxAge, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}
}

func TestLoginAndDevices(t *testing.T) {
	f := newFakeCloud(t)
	s, err := Login(context.Background(), testUser, testPassword, f.opts())
	if err != nil {
		t.Fatal(err)
	}
	if s.UserID != testUserID || s.CUserID != testCUserID || s.ServiceToken != testToken || s.Ssecurity != testSsecurity || s.PassToken != testPassToken {
		t.Fatalf("session: %+v", s)
	}

	devs, err := s.Devices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []Device{
		{DID: "940134049", Model: "miwear.watch.o66", Name: "Xiaomi Smart Band 10", ProductID: 12345, MAC: "AA:BB:CC:DD:EE:FF", SN: "SN1", EncryptKey: "00112233445566778899aabbccddeeff"},
		{DID: "2", Model: "m2", Name: "n2", MAC: "DE:AD:BE:EF:00:01", SN: "SN2", EncryptKey: "ffeeddccbbaa99887766554433221100"},
	}
	if fmt.Sprint(devs) != fmt.Sprint(want) {
		t.Fatalf("devices:\n got %+v\nwant %+v", devs, want)
	}

	h := f.headers[0]
	if got := h.Get("Cookie"); got != "cUserId="+testCUserID+"; serviceToken="+testToken+"; locale=en_us" {
		t.Errorf("cookie header: %q", got)
	}
	if h.Get("region_tag") != "cn" {
		t.Errorf("region_tag: %q", h.Get("region_tag"))
	}
	if ua := h.Get("User-Agent"); !strings.HasPrefix(ua, "Android-14-9.9.772-Xiaomi-") || !strings.HasSuffix(ua, "-"+md5Hex(testUserID)) {
		t.Errorf("health user agent: %q", ua)
	}
	if len(f.listRequests) != 2 {
		t.Fatalf("device list requests: %d", len(f.listRequests))
	}
}

func TestLoginWithPassToken(t *testing.T) {
	f := newFakeCloud(t)
	s, err := LoginWithPassToken(context.Background(), testUserID, testPassToken, f.opts())
	if err != nil {
		t.Fatal(err)
	}
	if s.ServiceToken != testToken || s.CUserID != testCUserID {
		t.Fatalf("session: %+v", s)
	}
}

func TestLoginReportsSecurityChecks(t *testing.T) {
	cases := []struct {
		name  string
		reply string
		check func(error) bool
	}{
		{"notification", `{"code":0,"securityStatus":16,"notificationUrl":"/identity/authStart?sid=miothealth","location":"x","ssecurity":"x","nonce":1}`, func(err error) bool {
			var e *NotificationError
			return errors.As(err, &e) && strings.HasSuffix(e.URL, "/identity/authStart?sid=miothealth") && strings.HasPrefix(e.URL, "http")
		}},
		{"captcha", `{"code":87001,"desc":"captcha","captchaUrl":"/pass/getCode?icodeType=login"}`, func(err error) bool {
			var e *CaptchaError
			return errors.As(err, &e) && strings.HasSuffix(e.URL, "/pass/getCode?icodeType=login")
		}},
		{"wrong password with captcha", `{"code":70016,"desc":"x","_sign":"s","qs":"q","callback":"c","captchaUrl":"https://c.example/x"}`, func(err error) bool {
			var e *CaptchaError
			return errors.As(err, &e) && e.URL == "https://c.example/x"
		}},
		{"wrong password", `{"code":70016,"desc":"x","_sign":"s","qs":"q","callback":"c","captchaUrl":null}`, func(err error) bool {
			return errors.Is(err, ErrInvalidCredentials)
		}},
		{"step2", `{"code":81003,"desc":"x","_sign":"s","qs":"q","callback":"c"}`, func(err error) bool {
			var e *VerificationError
			return errors.As(err, &e)
		}},
		{"other", `{"code":12345,"desc":"boom"}`, func(err error) bool {
			var e *ResponseError
			return errors.As(err, &e) && e.Code == 12345
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeCloud(t)
			f.auth2Reply = c.reply
			_, err := Login(context.Background(), testUser, testPassword, f.opts())
			if !c.check(err) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestDevicesRetriesAfterClockSkew(t *testing.T) {
	f := newFakeCloud(t)
	f.unauthorized = 1
	f.skew = 3 * time.Hour
	f.pages = map[string]string{"": `{"code":0,"result":{"list":[],"page_next":""}}`}
	s, err := LoginWithPassToken(context.Background(), testUserID, testPassToken, f.opts())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Devices(context.Background()); err != nil {
		t.Fatal(err)
	}
	raw, _ := b64.DecodeString(f.listRequests[0].Get("_nonce"))
	minutes := int64(binary.BigEndian.Uint32(raw[8:]))
	want := time.Now().Add(f.skew).UnixMilli() / 60000
	if minutes < want-1 || minutes > want+1 {
		t.Fatalf("nonce minutes %d, want about %d", minutes, want)
	}
}

func TestDevicesReportsUnauthorizedAndAPIErrors(t *testing.T) {
	f := newFakeCloud(t)
	s, err := LoginWithPassToken(context.Background(), testUserID, testPassToken, f.opts())
	if err != nil {
		t.Fatal(err)
	}
	f.unauthorized = 5
	if _, err := s.Devices(context.Background()); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("got %v, want ErrUnauthorized", err)
	}
	f.unauthorized = 0
	f.pages = map[string]string{"": `{"code":-6,"msg":"param error"}`}
	var apiErr *APIError
	if _, err := s.Devices(context.Background()); !errors.As(err, &apiErr) || apiErr.Code != -6 || apiErr.Msg != "param error" {
		t.Fatalf("got %v, want APIError -6", err)
	}
}

func TestRegionSelectsHealthHost(t *testing.T) {
	for region, want := range map[string]string{
		"":   "https://hlth.io.mi.com",
		"cn": "https://hlth.io.mi.com",
		"DE": "https://de.hlth.io.mi.com",
		"sg": "https://sg.hlth.io.mi.com",
		"ru": "https://ru.hlth.io.mi.com",
	} {
		o, err := (&Options{Region: region}).withDefaults()
		if err != nil {
			t.Fatal(err)
		}
		if o.HealthURL != want {
			t.Errorf("region %q: got %s want %s", region, o.HealthURL, want)
		}
	}
}

func TestRegionTagHeaderFollowsRegion(t *testing.T) {
	f := newFakeCloud(t)
	o := f.opts()
	o.Region = "de"
	s, err := LoginWithPassToken(context.Background(), testUserID, testPassToken, o)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Devices(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.headers[0].Get("region_tag"); got != "de" {
		t.Fatalf("region_tag %q", got)
	}
}
