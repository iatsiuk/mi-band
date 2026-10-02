package cloud

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("dial tcp: connection refused")
}

func TestTransportErrorsDoNotLeakSignedQuery(t *testing.T) {
	s, err := Resume(&Session{UserID: "1", CUserID: "c", ServiceToken: "tok", Ssecurity: testSsecurity},
		&Options{DeviceID: testDeviceID, HealthURL: "https://example.invalid", HTTPClient: &http.Client{Transport: failingTransport{}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Devices(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if msg := err.Error(); strings.Contains(msg, "signature") || strings.Contains(msg, "rc4_hash__") || strings.Contains(msg, "?") {
		t.Fatalf("error leaks the signed query: %s", msg)
	}
}

func TestVerboseLogDoesNotPrintResponseBodies(t *testing.T) {
	const secret = "SECRET-ENCRYPT-KEY-0123456789abcdef" //nolint:gosec // test fixture, not a credential
	f := newFakeCloud(t)
	f.pages = map[string]string{"": fmt.Sprintf(`{"code":0,"result":{"list":[{"sid":"1","model":"m","detail":{"encrypt_key":%q}}],"page_next":""}}`, secret)}
	var logged strings.Builder
	o := f.opts()
	o.Logf = func(format string, args ...any) { fmt.Fprintf(&logged, format+"\n", args...) }
	s, err := Login(context.Background(), testUser, testPassword, o)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Devices(context.Background()); err != nil {
		t.Fatal(err)
	}
	if logged.Len() == 0 {
		t.Fatal("expected some debug log")
	}
	if strings.Contains(logged.String(), secret) {
		t.Fatalf("debug log contains the device key: %s", logged.String())
	}
}
