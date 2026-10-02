package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"miband/internal/cloud"
)

var errNotLoggedIn = errors.New("not logged in: run miband login (or pass --user / --user-id with --pass-token)")

// maxCodeTries is how many email codes login asks for before it gives up.
const maxCodeTries = 3

// testServer, when set, replaces the account and health cloud hosts.
var testServer string

// savedSession is the on-disk cloud login, mode 0600.
type savedSession struct {
	Region   string        `json:"region"`
	DeviceID string        `json:"device_id"`
	Session  cloud.Session `json:"session"`
}

func sessionPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "miband", "cloud.json"), nil
}

// writeSecret replaces path atomically with a 0600 file, so a failed write keeps the old content.
func writeSecret(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

type cloudFlags struct {
	region    string
	user      string
	userID    string
	passToken string
	verbose   bool
	format    string

	deviceID string // effective pseudo device id, kept across logins
	stdin    io.Reader
	stderr   io.Writer
	in       *bufio.Reader // shared by all prompts, so buffered lines of piped stdin are not lost
}

func (c *cloudFlags) options() *cloud.Options {
	o := &cloud.Options{Region: c.region, DeviceID: c.deviceID, AccountURL: testServer, HealthURL: testServer}
	if c.verbose {
		o.Logf = func(f string, a ...any) { _, _ = fmt.Fprintf(c.stderr, f+"\n", a...) }
	}
	return o
}

func (c *cloudFlags) save(s *cloud.Session) error {
	p, err := sessionPath()
	if err != nil {
		return err
	}
	c.deviceID = s.DeviceID()
	b, err := json.MarshalIndent(savedSession{Region: c.region, DeviceID: c.deviceID, Session: *s}, "", "  ")
	if err != nil {
		return err
	}
	return writeSecret(p, b)
}

// loadSaved fills region and device id from the saved login; ok is false without one.
func (c *cloudFlags) loadSaved() (*savedSession, bool, error) {
	p, err := sessionPath()
	if err != nil {
		return nil, false, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var saved savedSession
	if err := json.Unmarshal(b, &saved); err != nil {
		return nil, false, fmt.Errorf("bad saved session %s: %w", p, err)
	}
	// files written before Session had json tags decode without tokens
	if saved.Session.ServiceToken == "" || saved.Session.UserID == "" {
		return nil, false, fmt.Errorf("saved session %s is incomplete: %w", p, errNotLoggedIn)
	}
	if c.region == "" {
		c.region = saved.Region
	}
	c.deviceID = saved.DeviceID
	return &saved, true, nil
}

func (c *cloudFlags) explicit() bool {
	return c.user != "" || c.passToken != ""
}

// login uses explicit credentials if given, else the saved session; a new login is saved.
func (c *cloudFlags) login(ctx context.Context) (*cloud.Session, error) {
	saved, ok, err := c.loadSaved()
	if c.explicit() {
		// a corrupt saved session must not block a fresh login
		return c.freshLogin(ctx)
	}
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errNotLoggedIn
	}
	sess, err := cloud.Resume(&saved.Session, c.options())
	if err != nil {
		return nil, err
	}
	// sessions saved before the device id was stored get one now, so it stays stable
	if saved.DeviceID == "" {
		return sess, c.save(sess)
	}
	return sess, nil
}

func (c *cloudFlags) freshLogin(ctx context.Context) (*cloud.Session, error) {
	var sess *cloud.Session
	var err error
	if c.passToken != "" {
		if c.userID == "" {
			return nil, errors.New("--pass-token needs --user-id: the numeric userId cookie of account.xiaomi.com (not the email)")
		}
		sess, err = cloud.LoginWithPassToken(ctx, c.userID, c.passToken, c.options())
	} else {
		var pass string
		if pass, err = c.readPassword(ctx); err != nil {
			return nil, err
		}
		sess, err = cloud.Login(ctx, c.user, pass, c.options())
		var ne *cloud.NotificationError
		if errors.As(err, &ne) {
			sess, err = c.verifyEmail(ctx, ne)
		}
	}
	if err != nil {
		return nil, explainLogin(err)
	}
	return sess, c.save(sess)
}

// readPassword takes MIBAND_PASSWORD, or prompts without echo on a terminal, else reads the first
// stdin line; a canceled ctx stops the wait.
func (c *cloudFlags) readPassword(ctx context.Context) (string, error) {
	if p := os.Getenv("MIBAND_PASSWORD"); p != "" {
		return p, nil
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	fd := int(os.Stdin.Fd()) //nolint:gosec // fd fits in int
	if c.stdin != os.Stdin || !term.IsTerminal(fd) {
		return readCtx(ctx, c.readLine)
	}
	_, _ = fmt.Fprint(c.stderr, "password: ")
	defer func() { _, _ = fmt.Fprintln(c.stderr) }()
	return c.readHidden(ctx, fd)
}

// verifyEmail passes the security check with the code sent to the account email. When the server
// offers no email code or refuses the check, the error keeps ne, so explainLogin shows the browser path.
func (c *cloudFlags) verifyEmail(ctx context.Context, ne *cloud.NotificationError) (*cloud.Session, error) {
	v, err := ne.SendEmailCode(ctx)
	if errors.Is(err, cloud.ErrNoEmailVerification) {
		return nil, ne
	}
	if err != nil {
		return nil, withBrowserPath(err, ne)
	}
	if !v.CodeSent {
		sess, err := v.Finish(ctx, "")
		return sess, withBrowserPath(err, ne)
	}
	to := v.MaskedEmail
	if to == "" {
		to = "the account email"
	}
	for tries := 0; tries < maxCodeTries; {
		code, err := c.ask(ctx, "code sent to "+to)
		if err != nil {
			return nil, err
		}
		// an empty answer would spend a server attempt
		if code = strings.TrimSpace(code); code == "" {
			_, _ = fmt.Fprintln(c.stderr, "empty code, enter the digits from the email")
			continue
		}
		tries++
		sess, err := v.Finish(ctx, code)
		if !errors.Is(err, cloud.ErrWrongCode) {
			return sess, withBrowserPath(err, ne)
		}
		_, _ = fmt.Fprintln(c.stderr, "wrong code")
	}
	return nil, cloud.ErrWrongCode
}

// withBrowserPath joins ne to a refusal of the email check; transport errors and cancellation stay as they are.
func withBrowserPath(err error, ne *cloud.NotificationError) error {
	var re *cloud.ResponseError
	if errors.As(err, &re) {
		return errors.Join(err, ne)
	}
	return err
}

// askMissing prompts for the region and the account that no flag gave.
func (c *cloudFlags) askMissing(ctx context.Context, regionSet bool) error {
	if !regionSet {
		r, err := c.ask(ctx, "region of the Mi Fitness server, e.g. de (Europe), sg (Singapore), ru (Russia), us (USA), i2 (India); empty for cn (China)")
		if err != nil {
			return err
		}
		c.region = strings.ToLower(strings.TrimSpace(r))
		if c.region == "" {
			c.region = "cn"
		}
	}
	if c.user == "" {
		u, err := c.ask(ctx, "Xiaomi account (email, phone or user id)")
		if err != nil {
			return err
		}
		if c.user = strings.TrimSpace(u); c.user == "" {
			return errors.New("no Xiaomi account given")
		}
	}
	return nil
}

// ask prints a prompt to stderr and reads one line of stdin.
func (c *cloudFlags) ask(ctx context.Context, prompt string) (string, error) {
	_, _ = fmt.Fprint(c.stderr, prompt+": ")
	s, err := readCtx(ctx, c.readLine)
	if errors.Is(err, io.EOF) {
		return "", fmt.Errorf("no input for the prompt %q", prompt)
	}
	return s, err
}

func (c *cloudFlags) readLine() (string, error) {
	if c.in == nil {
		c.in = bufio.NewReader(c.stdin)
	}
	line, err := c.in.ReadString('\n')
	if errors.Is(err, io.EOF) && line != "" {
		err = nil
	}
	return strings.TrimRight(line, "\r\n"), err
}

// readCtx returns when read does or ctx is done; a read left behind ends with the process.
func readCtx(ctx context.Context, read func() (string, error)) (string, error) {
	type result struct {
		s   string
		err error
	}
	ch := make(chan result, 1)
	go func() {
		s, err := read()
		ch <- result{s, err}
	}()
	select {
	case r := <-ch:
		return r.s, r.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// hintError replaces the message of a passport error with instructions but keeps it for errors.As.
type hintError struct {
	msg string
	err error
}

func (e *hintError) Error() string { return e.msg }
func (e *hintError) Unwrap() error { return e.err }

// explainLogin turns passport verification errors into instructions.
func explainLogin(err error) error {
	var ne *cloud.NotificationError
	var ce *cloud.CaptchaError
	var ve *cloud.VerificationError
	switch {
	case errors.As(err, &ve):
		return &hintError{"second-step verification code required: log in once at https://account.xiaomi.com in a browser, then rerun miband login with --user-id and --pass-token cookies", err}
	case errors.As(err, &ne):
		msg := fmt.Sprintf("account needs verification: open %s in a browser, then rerun miband login with --user-id and --pass-token taken from the account.xiaomi.com cookies", ne.URL)
		var re *cloud.ResponseError
		if errors.As(err, &re) {
			msg = "email code check failed (" + re.Error() + "); " + msg
		}
		return &hintError{msg, err}
	case errors.As(err, &ce):
		return &hintError{fmt.Sprintf("captcha required: log in once at %s in a browser, then rerun miband login with --user-id and --pass-token cookies", ce.URL), err}
	}
	return err
}

// client keeps the session and logs in again with the saved pass token once on 401.
type client struct {
	flags     *cloudFlags
	sess      *cloud.Session
	refreshed bool
	relogin   func(ctx context.Context, userID, passToken string) (*cloud.Session, error)
}

func newClient(ctx context.Context, cf *cloudFlags) (*client, error) {
	s, err := cf.login(ctx)
	if err != nil {
		return nil, err
	}
	return &client{flags: cf, sess: s, relogin: func(ctx context.Context, userID, passToken string) (*cloud.Session, error) {
		return cloud.LoginWithPassToken(ctx, userID, passToken, cf.options())
	}}, nil
}

// retry reports whether the call should be repeated with a refreshed session; otherwise it
// returns the error to show: the original one, or why the refresh failed.
func (cl *client) retry(ctx context.Context, err error) (bool, error) {
	if !errors.Is(err, cloud.ErrUnauthorized) || cl.refreshed || cl.sess.PassToken == "" {
		return false, err
	}
	cl.refreshed = true
	ns, lerr := cl.relogin(ctx, cl.sess.UserID, cl.sess.PassToken)
	if lerr != nil {
		return false, fmt.Errorf("cloud session expired and re-login failed: %w", explainLogin(lerr))
	}
	cl.sess = ns
	if serr := cl.flags.save(ns); serr != nil {
		return false, fmt.Errorf("save refreshed cloud session: %w", serr)
	}
	return true, nil
}

// withRetry runs fn and repeats it once with a refreshed session after a 401.
func withRetry[T any](ctx context.Context, cl *client, fn func(*cloud.Session) (T, error)) (T, error) {
	v, err := fn(cl.sess)
	if err == nil {
		return v, nil
	}
	again, err := cl.retry(ctx, err)
	if !again {
		return v, err
	}
	return fn(cl.sess)
}
