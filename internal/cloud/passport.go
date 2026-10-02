package cloud

import (
	"context"
	"crypto/md5"  //nolint:gosec // passport password hash is md5 by protocol
	"crypto/sha1" //nolint:gosec // passport client sign is sha1 by protocol
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// passport result codes (XMPassport.processLoginContent)
const (
	codeOK            = 0
	codeInvalidUser   = 20003
	codeBadCredential = 70002
	codeAuthFailed    = 70016
	codeVerification  = 81003
	codeCaptcha       = 87001
)

// response bodies may start with this anti-hijacking prefix (XMPassport.PASSPORT_SAFE_PREFIX)
const safePrefix = "&&&START&&&"

var (
	ErrInvalidCredentials = errors.New("xiaomi account: wrong user name or password")
	ErrInvalidUser        = errors.New("xiaomi account: unknown user name")
)

// NotificationError means the account requires a security check (2FA) in a browser.
type NotificationError struct{ URL string }

func (e *NotificationError) Error() string {
	return "xiaomi account: security verification required; open " + e.URL +
		" in a browser, finish it, then use the userId and passToken cookies of account.xiaomi.com with LoginWithPassToken"
}

// CaptchaError means the server wants a captcha solved before accepting the password.
type CaptchaError struct{ URL string }

func (e *CaptchaError) Error() string {
	return "xiaomi account: captcha required (" + e.URL + "); sign in once at " + defaultAccountURL +
		" in a browser, then use the userId and passToken cookies with LoginWithPassToken"
}

// VerificationError means the server asked for a second-step code (code 81003).
type VerificationError struct{}

func (e *VerificationError) Error() string {
	return "xiaomi account: second-step verification code required; sign in at " + defaultAccountURL +
		" in a browser, then use the userId and passToken cookies with LoginWithPassToken"
}

// ResponseError is any other non-zero passport result.
type ResponseError struct {
	Code int
	Desc string
}

func (e *ResponseError) Error() string {
	return fmt.Sprintf("xiaomi account: code %d: %s", e.Code, e.Desc)
}

// Session holds the miothealth service token of a logged-in account.
type Session struct {
	UserID       string
	CUserID      string
	ServiceToken string
	Ssecurity    string
	// PassToken allows LoginWithPassToken without the password; treat it as a secret.
	PassToken string

	opts     Options
	timeDiff time.Duration
}

type loginResult struct {
	Code            int             `json:"code"`
	Desc            string          `json:"desc"`
	Sign            string          `json:"_sign"`
	Qs              string          `json:"qs"`
	Callback        string          `json:"callback"`
	CaptchaURL      string          `json:"captchaUrl"`
	SecurityStatus  int             `json:"securityStatus"`
	NotificationURL string          `json:"notificationUrl"`
	Ssecurity       string          `json:"ssecurity"`
	Nonce           json.RawMessage `json:"nonce"`
	Location        string          `json:"location"`
}

type passportReply struct {
	res    loginResult
	header http.Header
}

// httpReply is a read and closed response.
type httpReply struct {
	status int
	header http.Header
	body   []byte
}

// Login signs in with user name (email, phone or account id) and password, mirroring
// XMPassport.loginByPassword for sid miothealth, and fetches the service token.
func Login(ctx context.Context, user, password string, opts *Options) (*Session, error) {
	o, err := resolve(opts)
	if err != nil {
		return nil, err
	}
	// step 1: serviceLogin without passToken answers 70016 with the login metadata
	meta, err := o.serviceLogin(ctx, user, "")
	if err != nil {
		return nil, err
	}
	if meta.res.Code != codeAuthFailed {
		return nil, fmt.Errorf("xiaomi account: unexpected serviceLogin code %d: %s", meta.res.Code, meta.res.Desc)
	}

	form := url.Values{
		"hash":     {passwordHash(password)},
		"user":     {user},
		"sid":      {healthSID},
		"_json":    {"true"},
		"_locale":  {o.Locale},
		"_sign":    {meta.res.Sign},
		"qs":       {meta.res.Qs},
		"callback": {meta.res.Callback},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.AccountURL+"/pass/serviceLoginAuth2", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reply, err := o.passportDo(req, cookieHeader("deviceId", o.DeviceID))
	if err != nil {
		return nil, err
	}
	return o.finishLogin(ctx, reply, "", "")
}

// LoginWithPassToken signs in with the userId and passToken cookies of account.xiaomi.com, for
// example after finishing a browser verification (CookieLoginUrlInterceptor login-end path).
func LoginWithPassToken(ctx context.Context, userID, passToken string, opts *Options) (*Session, error) {
	o, err := resolve(opts)
	if err != nil {
		return nil, err
	}
	reply, err := o.serviceLogin(ctx, userID, passToken)
	if err != nil {
		return nil, err
	}
	return o.finishLogin(ctx, reply, userID, passToken)
}

func resolve(opts *Options) (Options, error) {
	var o Options
	if opts != nil {
		o = *opts
	}
	return o.withDefaults()
}

// serviceLogin mirrors XMPassport.loginByPassToken (GET /pass/serviceLogin).
func (o *Options) serviceLogin(ctx context.Context, userID, passToken string) (*passportReply, error) {
	q := url.Values{"sid": {healthSID}, "_json": {"true"}, "_locale": {o.Locale}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.AccountURL+"/pass/serviceLogin?"+q.Encode(), http.NoBody)
	if err != nil {
		return nil, err
	}
	return o.passportDo(req, cookieHeader("userId", userID, "passToken", passToken, "deviceId", o.DeviceID))
}

func (o *Options) passportDo(req *http.Request, cookie string) (*passportReply, error) {
	reply, err := o.send(req, passportUserAgent(), cookie)
	if err != nil {
		return nil, err
	}
	r := passportReply{header: reply.header}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(string(reply.body), safePrefix)), &r.res); err != nil {
		return nil, fmt.Errorf("xiaomi account: bad response from %s: %w", req.URL.Path, err)
	}
	return &r, nil
}

// cookieHeader joins name/value pairs in order, skipping empty values.
func cookieHeader(kv ...string) string {
	var parts []string
	for i := 0; i+1 < len(kv); i += 2 {
		if kv[i+1] != "" {
			parts = append(parts, kv[i]+"="+kv[i+1])
		}
	}
	return strings.Join(parts, "; ")
}

// send performs a request; 200 and 302 count as success (SimpleRequest). The reply is also
// returned with an HTTPError.
func (o *Options) send(req *http.Request, ua, cookie string) (*httpReply, error) {
	req.Header.Set("User-Agent", ua)
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err := o.HTTPClient.Do(req)
	if err != nil {
		return nil, &requestError{op: req.Method + " " + req.URL.Scheme + "://" + req.URL.Host + req.URL.Path, cause: err}
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	reply := &httpReply{status: resp.StatusCode, header: resp.Header, body: body}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusFound {
		return reply, &HTTPError{Status: resp.StatusCode, URL: req.URL.Scheme + "://" + req.URL.Host + req.URL.Path}
	}
	return reply, nil
}

// HTTPError is an unexpected HTTP status.
type HTTPError struct {
	Status int
	URL    string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("xiaomi cloud: HTTP %d from %s", e.Status, e.URL)
}

// finishLogin mirrors processLoginContent + parseLoginResult + getServiceTokenByStsUrl.
func (o *Options) finishLogin(ctx context.Context, r *passportReply, userID, passToken string) (*Session, error) {
	res := &r.res
	if err := o.loginError(res); err != nil {
		return nil, err
	}
	s := &Session{
		UserID:    pick(r.header, "userId", userID),
		PassToken: pick(r.header, "passToken", passToken),
		CUserID:   pick(r.header, "cUserId", ""),
		Ssecurity: res.Ssecurity,
		opts:      *o,
	}
	if s.UserID == "" || s.PassToken == "" {
		return nil, errors.New("xiaomi account: no userId or passToken in login response")
	}
	if s.Ssecurity == "" || res.Location == "" {
		return nil, errors.New("xiaomi account: no ssecurity or location in login response")
	}
	if err := o.fetchServiceToken(ctx, s, res); err != nil {
		return nil, err
	}
	return s, nil
}

// loginError maps a passport result to its error; nil means the login succeeded.
func (o *Options) loginError(res *loginResult) error {
	if err := o.codeError(res); err != nil {
		return err
	}
	if res.SecurityStatus == 0 {
		return nil
	}
	if res.NotificationURL == "" {
		return &ResponseError{Code: res.Code, Desc: "securityStatus set without notificationUrl"}
	}
	return &NotificationError{URL: o.absolute(res.NotificationURL)}
}

func (o *Options) codeError(res *loginResult) error {
	switch res.Code {
	case codeOK:
		return nil
	case codeInvalidUser:
		return ErrInvalidUser
	case codeBadCredential:
		return ErrInvalidCredentials
	case codeAuthFailed:
		if res.CaptchaURL != "" && res.CaptchaURL != "null" {
			return &CaptchaError{URL: o.absolute(res.CaptchaURL)}
		}
		return ErrInvalidCredentials
	case codeCaptcha:
		return &CaptchaError{URL: o.absolute(res.CaptchaURL)}
	case codeVerification:
		return &VerificationError{}
	default:
		return &ResponseError{Code: res.Code, Desc: res.Desc}
	}
}

// fetchServiceToken does the sts step: GET location with clientSign, redirects not followed,
// the token comes as a cookie.
func (o *Options) fetchServiceToken(ctx context.Context, s *Session, res *loginResult) error {
	nonce := strings.Trim(string(res.Nonce), `"`)
	if _, err := strconv.ParseInt(nonce, 10, 64); err != nil {
		nonce = "0"
	}
	loc := res.Location
	sep := "?"
	if strings.Contains(loc, "?") {
		sep = "&"
	}
	loc += sep + "clientSign=" + url.QueryEscape(clientSign(nonce, s.Ssecurity)) + "&_userIdNeedEncrypt=true"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, loc, http.NoBody)
	if err != nil {
		return err
	}
	reply, err := o.send(req, passportUserAgent(), "")
	if err != nil {
		return err
	}
	s.ServiceToken = pick(reply.header, healthSID+"_serviceToken", "")
	if s.ServiceToken == "" {
		s.ServiceToken = pick(reply.header, "serviceToken", "")
	}
	if s.ServiceToken == "" {
		return errors.New("xiaomi account: no serviceToken in sts response")
	}
	if s.CUserID == "" {
		s.CUserID = pick(reply.header, "cUserId", "")
	}
	return nil
}

// passwordHash mirrors CloudCoder.getMd5DigestUpperCase.
func passwordHash(password string) string {
	sum := md5.Sum([]byte(password)) //nolint:gosec // protocol-defined password hash
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

// clientSign mirrors XMPassport.getClientSign: base64(sha1("nonce=<nonce>&<ssecurity>")).
func clientSign(nonce, ssecurity string) string {
	sum := sha1.Sum([]byte("nonce=" + nonce + "&" + ssecurity)) //nolint:gosec // protocol-defined client sign
	return b64.EncodeToString(sum[:])
}

func (o *Options) absolute(u string) string {
	if strings.HasPrefix(u, "http") {
		return u
	}
	return o.AccountURL + u
}

// pick mirrors StringContent.getHeader: response headers win over live Set-Cookie values.
func pick(h http.Header, name, fallback string) string {
	if v := h.Get(name); v != "" {
		return v
	}
	v := ""
	for _, line := range h.Values("Set-Cookie") {
		c, err := http.ParseSetCookie(line)
		if err != nil || c.Name != name || c.MaxAge < 0 || (!c.Expires.IsZero() && c.Expires.Before(time.Now())) {
			continue
		}
		v = c.Value
	}
	if v == "" {
		return fallback
	}
	return v
}

// Resume rebuilds a session saved from an earlier login (all fields of Session).
func Resume(saved *Session, opts *Options) (*Session, error) {
	o, err := resolve(opts)
	if err != nil {
		return nil, err
	}
	s := *saved
	s.opts = o
	return &s, nil
}

// DeviceID returns the pseudo device id this session logs in with; save it with the session so
// later logins look like the same phone, as Mi Fitness does with its stored hashed device id.
func (s *Session) DeviceID() string { return s.opts.DeviceID }
