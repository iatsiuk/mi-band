package main

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"miband/internal/cloud"
)

// execute runs the root command with an isolated config dir and returns stdout, stderr and the error.
func execute(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	return executeIn(t, "", args...)
}

// executeIn is execute with stdin.
func executeIn(t *testing.T, stdin string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("MIBAND_PASSWORD", "")
	return run(stdin, args...)
}

// run executes the root command in the current environment.
func run(stdin string, args ...string) (stdout, stderr string, err error) {
	var out, errOut bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	err = cmd.Execute()
	return out.String(), errOut.String(), err
}

func TestRootHelpListsCommands(t *testing.T) {
	out, _, err := execute(t, "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"login", "logout", "devices", "data"} {
		if !strings.Contains(out, name) {
			t.Errorf("help has no %q command:\n%s", name, out)
		}
	}
}

func TestRootVersionFlag(t *testing.T) {
	out, _, err := execute(t, "--version")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, version) {
		t.Errorf("got %q, want version %q", out, version)
	}
}

func TestRootRejectsUnknownCommand(t *testing.T) {
	if _, _, err := execute(t, "scan"); err == nil {
		t.Fatal("want error for unknown command")
	}
}

func TestRootPersistentFlagDefaults(t *testing.T) {
	t.Parallel()
	f := newRootCmd().PersistentFlags()
	for _, name := range []string{"region", "user", "user-id", "pass-token"} {
		v, err := f.GetString(name)
		if err != nil {
			t.Fatal(err)
		}
		if v != "" {
			t.Errorf("--%s: got %q, want empty", name, v)
		}
	}
	if v, err := f.GetBool("verbose"); err != nil || v {
		t.Errorf("--verbose: got %v, err %v", v, err)
	}
}

func TestLoginWithoutInputFails(t *testing.T) {
	_, _, err := execute(t, "login")
	if err == nil || !strings.Contains(err.Error(), "region") {
		t.Fatalf("got %v, want missing region input", err)
	}
}

func TestLoginPassTokenRequiresUserID(t *testing.T) {
	_, _, err := execute(t, "login", "--pass-token", "pt")
	if err == nil || !strings.Contains(err.Error(), "--user-id") {
		t.Fatalf("got %v, want hint about --user-id", err)
	}
}

func TestDataRejectsDaysOutOfRange(t *testing.T) {
	for _, days := range []string{"-1", "3651"} {
		_, _, err := execute(t, "data", "--days", days)
		if err == nil || !strings.Contains(err.Error(), "--days") {
			t.Errorf("--days %s: got %v", days, err)
		}
	}
}

func TestCommandsWithoutSessionFailAsAuth(t *testing.T) {
	for _, name := range []string{"devices", "data"} {
		_, _, err := execute(t, name)
		if !errors.Is(err, errNotLoggedIn) {
			t.Fatalf("%s: got %v, want errNotLoggedIn", name, err)
		}
		if code := exitCode(err); code != exitAuth {
			t.Errorf("%s: exit code %d, want %d", name, code, exitAuth)
		}
	}
}

func TestExitCode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, exitOK},
		{"generic", errors.New("boom"), exitError},
		{"not logged in", errNotLoggedIn, exitAuth},
		{"token rejected", fmt.Errorf("data: %w", cloud.ErrUnauthorized), exitAuth},
		{"wrong password", cloud.ErrInvalidCredentials, exitAuth},
		{"unknown user", fmt.Errorf("login: %w", cloud.ErrInvalidUser), exitAuth},
		{"second step", &cloud.VerificationError{}, exitAuth},
		{"explained", explainLogin(&cloud.VerificationError{}), exitAuth},
		{"verification", &cloud.NotificationError{URL: "u"}, exitAuth},
		{"captcha", &cloud.CaptchaError{URL: "u"}, exitAuth},
		{"wrong email code", cloud.ErrWrongCode, exitAuth},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := exitCode(tt.err); got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestExplainLoginPointsToCLIFlags(t *testing.T) {
	t.Parallel()
	for _, err := range []error{
		&cloud.NotificationError{URL: "https://account.example/n"},
		&cloud.CaptchaError{URL: "https://account.example/c"},
		&cloud.VerificationError{},
	} {
		msg := explainLogin(err).Error()
		if !strings.Contains(msg, "--user-id") || !strings.Contains(msg, "--pass-token") || strings.Contains(msg, "LoginWithPassToken") {
			t.Errorf("%T: %s", err, msg)
		}
	}
}
