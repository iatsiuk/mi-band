package cloud

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// identity check behind notificationUrl as the account.xiaomi.com web page runs it in the Mi Fitness
// webview (mcfe--mi-account bundle: IVE chunk for email, identity flags EMAIL=8; requests checked
// against a browser HAR); the session lives in the identity_session cookie
const (
	identityEmailFlag   = 8
	identityChoose      = 2 // identity/list: pick one of the options
	identityListPath    = "/identity/list"
	identityEmailPath   = "/identity/auth/verifyEmail"
	identityTicketPath  = "/identity/auth/sendEmailTicket"
	identityContentType = "application/x-www-form-urlencoded; charset=UTF-8"
	identityAccept      = "application/json, text/plain, */*"
	navigationAccept    = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"
	// the page moves from authStart to this path before it sends the ticket (HAR Referer)
	identityEmailPage = "/fe/service/identity/verifyEmail"
)

var (
	ErrNoEmailVerification = errors.New("xiaomi account: the security check offers no email code")
	ErrWrongCode           = errors.New("xiaomi account: wrong verification code")
)

// wrongCodes are the verifyEmail results the web page reports as a wrong (ResErrorSMS) or empty
// (EmptyErrorSMS) code.
var wrongCodes = map[int]bool{70001: true, 70012: true, 70014: true, 70016: true, 100005: true, 100006: true}

// EmailVerification is a started security check waiting for the code sent to the account email.
// CodeSent is false when the server reports the check as already passed; Finish then ignores the code.
type EmailVerification struct {
	MaskedEmail string
	CodeSent    bool

	opts   Options // HTTPClient carries the cookie jar of the check
	page   string  // url of the page the calls come from, their Referer
	trust  bool    // the page offers "trust this device"
	passed string  // location of an already passed check
}

type identityResult struct {
	Code          int    `json:"code"`
	Options       []int  `json:"options"`
	TrustCheckBox bool   `json:"trustCheckBox"`
	MaskedEmail   string `json:"maskedEmail"`
	Location      string `json:"location"`
}

// SendEmailCode starts the security check of the notification url and sends a code to the
// account email; ErrNoEmailVerification means the check needs a browser.
func (e *NotificationError) SendEmailCode(ctx context.Context) (*EmailVerification, error) {
	u, err := url.Parse(e.URL)
	if err != nil {
		return nil, errors.New("xiaomi account: bad notification url")
	}
	v, err := newEmailVerification(&e.opts, e.URL)
	if err != nil {
		return nil, err
	}
	if err := v.offersEmail(ctx, u.Query()); err != nil || v.passed != "" {
		return v, err
	}
	page, err := v.call(ctx, http.MethodGet, identityEmailPath+"?_flag=8&_json=true", nil)
	if err != nil {
		return nil, err
	}
	v.MaskedEmail = page.MaskedEmail
	u.Path = identityEmailPage
	v.page = u.String()
	form := url.Values{"retry": {"0"}, "icode": {""}, "_json": {"true"}}
	if _, err := v.call(ctx, http.MethodPost, identityTicketPath+"?_dc="+dc(), form); err != nil {
		return nil, err
	}
	v.CodeSent = true
	return v, nil
}

// newEmailVerification gives the check its own cookie jar with the cookies the app fills before it
// loads the page: deviceId of the login and NativeUserAgent (CookieFillAccountDeviceParamsULPT).
func newEmailVerification(opts *Options, page string) (*EmailVerification, error) {
	o, err := resolve(opts)
	if err != nil {
		return nil, err
	}
	account, err := url.Parse(o.AccountURL)
	if err != nil {
		return nil, fmt.Errorf("xiaomi account: bad account url: %w", err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	jar.SetCookies(account, []*http.Cookie{
		{Name: "deviceId", Value: o.DeviceID}, //nolint:gosec // client jar entry, not a server cookie
		{Name: "NativeUserAgent", Value: base64.StdEncoding.EncodeToString([]byte(webViewUserAgent()))}, //nolint:gosec // client jar entry, not a server cookie
	})
	c := *o.HTTPClient
	c.Jar = jar
	o.HTTPClient = &c
	return &EmailVerification{opts: o, page: page}, nil
}

// offersEmail opens the check from the notification url query and makes sure it offers an email code.
func (v *EmailVerification) offersEmail(ctx context.Context, notification url.Values) error {
	sid := notification.Get("sid")
	if sid == "" {
		sid = healthSID
	}
	q := url.Values{"sid": {sid}, "supportedMask": {"0"}, "_locale": {v.opts.Locale}, "context": {notification.Get("context")}}
	list, err := v.fetch(ctx, http.MethodGet, identityListPath+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	// code 0 with a location: nothing left to verify, the page just navigates there
	if list.Code == codeOK && list.Location != "" {
		v.passed = v.opts.absolute(list.Location)
		return nil
	}
	if list.Code != codeOK && list.Code != identityChoose {
		return &ResponseError{Code: list.Code}
	}
	if !slices.Contains(list.Options, identityEmailFlag) {
		return ErrNoEmailVerification
	}
	v.trust = list.TrustCheckBox
	return nil
}

// Finish submits the email code, marks the device as trusted and logs in with the pass token the
// check issues. ErrWrongCode leaves the check open for another code.
func (v *EmailVerification) Finish(ctx context.Context, code string) (*Session, error) {
	if v.passed != "" {
		return v.login(ctx, v.passed)
	}
	// the page sends trust=false when it shows no checkbox; ticked when it does, so the device stays trusted
	form := url.Values{"_flag": {strconv.Itoa(identityEmailFlag)}, "ticket": {code}, "trust": {strconv.FormatBool(v.trust)}, "_json": {"true"}}
	res, err := v.call(ctx, http.MethodPost, identityEmailPath+"?_dc="+dc(), form)
	var re *ResponseError
	if errors.As(err, &re) && wrongCodes[re.Code] {
		return nil, ErrWrongCode
	}
	if err != nil {
		return nil, err
	}
	if res.Location == "" {
		return nil, errors.New("xiaomi account: no location after verification")
	}
	return v.login(ctx, v.opts.absolute(res.Location))
}

// login follows the location of a passed check and logs in with the pass token it issues.
func (v *EmailVerification) login(ctx context.Context, location string) (*Session, error) {
	userID, passToken, err := v.followLogin(ctx, location)
	if err != nil {
		return nil, err
	}
	return LoginWithPassToken(ctx, userID, passToken, &v.opts)
}

// call is fetch that accepts only code 0, as the web page checks every identity reply but the list.
func (v *EmailVerification) call(ctx context.Context, method, path string, form url.Values) (*identityResult, error) {
	res, err := v.fetch(ctx, method, path, form)
	if err != nil {
		return nil, err
	}
	if res.Code != codeOK {
		return nil, &ResponseError{Code: res.Code}
	}
	return res, nil
}

func (v *EmailVerification) fetch(ctx context.Context, method, path string, form url.Values) (*identityResult, error) {
	var body io.Reader = http.NoBody
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, v.opts.AccountURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", identityAccept)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Referer", v.page)
	if form != nil {
		req.Header.Set("Content-Type", identityContentType)
		req.Header.Set("Origin", v.opts.AccountURL)
	}
	var res identityResult
	if _, err := v.opts.passportJSON(req, webViewUserAgent(), "", &res); err != nil {
		return nil, err
	}
	if v.opts.Logf != nil {
		v.opts.Logf("%s %s: code %d", method, req.URL.Path, res.Code)
	}
	return &res, nil
}

// followLogin walks the redirects after the check; one of them sets the userId and passToken
// cookies of the account host (passInfo=login-end, CookieLoginUrlInterceptor).
func (v *EmailVerification) followLogin(ctx context.Context, location string) (userID, passToken string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, location, http.NoBody)
	if err != nil {
		return "", "", errors.New("xiaomi account: bad location after verification")
	}
	req.Header.Set("Accept", navigationAccept)
	req.Header.Set("Referer", v.page)
	c := *v.opts.HTTPClient
	c.CheckRedirect = nil
	o := v.opts
	o.HTTPClient = &c
	_, sendErr := o.send(req, webViewUserAgent(), "")
	account, err := url.Parse(o.AccountURL)
	if err != nil {
		return "", "", err
	}
	for _, ck := range c.Jar.Cookies(account) {
		switch ck.Name {
		case "userId":
			userID = ck.Value
		case "passToken":
			passToken = ck.Value
		}
	}
	if userID != "" && passToken != "" {
		return userID, passToken, nil
	}
	if sendErr != nil {
		return "", "", sendErr
	}
	return "", "", errors.New("xiaomi account: no userId or passToken after verification")
}

// dc is the cache buster the web page appends to identity posts.
func dc() string {
	return strconv.FormatInt(time.Now().UnixMilli(), 10)
}
