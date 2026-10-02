package cloud

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	deviceListPath = "/app/v1/source/get_source_list"
	devicePageSize = 50
	maxDevicePages = 20
)

// Device is one entry of the health cloud device list.
type Device struct {
	DID       string `json:"did"`
	Model     string `json:"model"`
	Name      string `json:"name"`
	ProductID int    `json:"product_id"`
	MAC       string `json:"mac,omitempty"`
	SN        string `json:"sn,omitempty"`
	// EncryptKey is the BLE auth token (hex) used by auth V2 for cloud-bound wearables.
	EncryptKey string `json:"encrypt_key,omitempty"`
}

// APIError is a non-zero code in a health cloud response.
type APIError struct {
	Code int
	Msg  string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("xiaomi health cloud: code %d: %s", e.Code, e.Msg)
}

// ErrUnauthorized means the service token was rejected; log in again.
var ErrUnauthorized = errors.New("xiaomi health cloud: service token rejected (HTTP 401)")

type wireDevice struct {
	DID       string `json:"sid"`
	Model     string `json:"model"`
	Name      string `json:"name"`
	ProductID int    `json:"pd_id"`
	Detail    *struct {
		MAC        string `json:"mac"`
		RandomMAC  string `json:"random_mac"`
		SN         string `json:"sn"`
		EncryptKey string `json:"encrypt_key"`
	} `json:"detail"`
}

// Devices returns all devices bound to the account (DeviceRequest.getDevices paging). A repeated
// page_next cursor is ErrIncompletePages and running out of pages ErrTooManyPages; the devices
// read so far are returned with either.
func (s *Session) Devices(ctx context.Context) ([]Device, error) {
	var out []Device
	next := ""
	seen := map[string]bool{"": true}
	for range maxDevicePages {
		data := map[string]any{"page_size": devicePageSize}
		if next != "" {
			data["page_start"] = next
		}
		var res struct {
			List     []wireDevice `json:"list"`
			PageNext string       `json:"page_next"`
		}
		if err := s.call(ctx, http.MethodPost, endpoint{path: deviceListPath}, data, &res); err != nil {
			return nil, err
		}
		for _, d := range res.List {
			out = append(out, d.convert())
		}
		if res.PageNext == "" {
			return out, nil
		}
		if seen[res.PageNext] {
			return out, ErrIncompletePages
		}
		seen[res.PageNext] = true
		next = res.PageNext
	}
	return out, ErrTooManyPages
}

func (d wireDevice) convert() Device {
	dev := Device{DID: d.DID, Model: d.Model, Name: d.Name, ProductID: d.ProductID}
	if d.Detail != nil {
		// Device.getMac prefers random_mac
		dev.MAC = strings.ToUpper(d.Detail.MAC)
		if d.Detail.RandomMAC != "" {
			dev.MAC = strings.ToUpper(d.Detail.RandomMAC)
		}
		dev.SN = d.Detail.SN
		dev.EncryptKey = d.Detail.EncryptKey
	}
	return dev
}

// endpoint is a health cloud API path. prefix is the @Secret pathPrefix of its service, which
// CloudInterceptor strips from the url path before signing.
type endpoint struct {
	path   string
	prefix string
}

// signPath mirrors CloudInterceptor.subpath plus the leading "/" added by getEncryptedParams.
func (e endpoint) signPath() string {
	p := e.path
	if e.prefix == "" {
		if i := strings.IndexByte(p, '/'); i >= 0 {
			p = p[i:]
		}
	} else if i := strings.Index(p, e.prefix); i >= 0 {
		p = p[i+len(e.prefix):]
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

func (s *Session) ensureOptions() error {
	if s.opts.HTTPClient != nil {
		return nil
	}
	o, err := s.opts.withDefaults()
	if err != nil {
		return err
	}
	s.opts = o
	return nil
}

// call sends data as the encrypted, signed "data" param (form for POST, query for GET) and
// decodes the result of the {code, msg, result} envelope into v.
func (s *Session) call(ctx context.Context, method string, ep endpoint, data, v any) error {
	if err := s.ensureOptions(); err != nil {
		return err
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	body, err := s.authorized(ctx, method, ep, map[string]string{"data": string(raw)})
	if err != nil {
		return err
	}
	if s.opts.Logf != nil {
		// bodies hold device keys, signed urls and health data, so only sizes are logged
		s.opts.Logf("%s %s%s -> %d bytes", method, s.opts.HealthURL, ep.path, len(body))
	}
	return decodeEnvelope(body, v)
}

// authorized sends a request and maps a final 401 to ErrUnauthorized.
func (s *Session) authorized(ctx context.Context, method string, ep endpoint, values map[string]string) ([]byte, error) {
	body, err := s.request(ctx, method, ep, values)
	var he *HTTPError
	if errors.As(err, &he) && he.Status == http.StatusUnauthorized && s.timeDiff != 0 {
		// VerifyToken retries a 401 after aligning the nonce clock with the server Date header
		body, err = s.request(ctx, method, ep, values)
	}
	if errors.As(err, &he) && he.Status == http.StatusUnauthorized {
		return nil, ErrUnauthorized
	}
	return body, err
}

func decodeEnvelope(body []byte, v any) error {
	var env struct {
		Code    int             `json:"code"`
		Msg     string          `json:"msg"`
		Message string          `json:"message"`
		Result  json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("xiaomi health cloud: bad response: %w", err)
	}
	if env.Code != 0 {
		if env.Msg == "" {
			env.Msg = env.Message
		}
		return &APIError{Code: env.Code, Msg: env.Msg}
	}
	if v == nil || len(env.Result) == 0 {
		return nil
	}
	return json.Unmarshal(env.Result, v)
}

// request signs and encrypts values (form for POST, query for GET) and decrypts the reply.
func (s *Session) request(ctx context.Context, method string, ep endpoint, values map[string]string) ([]byte, error) {
	nonce, err := newNonce(rand.Reader, time.Now().Add(s.timeDiff))
	if err != nil {
		return nil, err
	}
	params, err := encryptParams(method, ep.signPath(), values, nonce, s.Ssecurity)
	if err != nil {
		return nil, err
	}
	req, err := s.newRequest(ctx, method, ep, params)
	if err != nil {
		return nil, err
	}
	// VerifyToken sets cUserId and serviceToken, ParameterInterceptor appends locale
	cookie := cookieHeader("cUserId", s.CUserID, "serviceToken", s.ServiceToken, "locale", healthLocale(s.opts.Locale))
	reply, err := s.opts.send(req, healthUserAgent(s.opts.DeviceID, s.UserID), cookie)
	if err != nil {
		if reply != nil && reply.status == http.StatusUnauthorized {
			if t, perr := http.ParseTime(reply.header.Get("Date")); perr == nil {
				s.timeDiff = time.Until(t)
			}
		}
		return nil, err
	}
	plain, err := decryptBody(string(reply.body), nonce, s.Ssecurity)
	if err != nil {
		// error replies are not always encrypted
		if t := strings.TrimSpace(string(reply.body)); strings.HasPrefix(t, "{") {
			return []byte(t), nil
		}
		return nil, fmt.Errorf("xiaomi health cloud: decrypt failed: %w", err)
	}
	return plain, nil
}

// newRequest builds the health cloud request: params as form for POST, query for GET.
func (s *Session) newRequest(ctx context.Context, method string, ep endpoint, params map[string]string) (*http.Request, error) {
	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}
	var req *http.Request
	var err error
	if method == http.MethodGet {
		req, err = http.NewRequestWithContext(ctx, method, s.opts.HealthURL+ep.path+"?"+form.Encode(), http.NoBody)
	} else {
		req, err = http.NewRequestWithContext(ctx, method, s.opts.HealthURL+ep.path, strings.NewReader(form.Encode()))
	}
	if err != nil {
		// a GET url carries the signed query, so only the endpoint is named
		return nil, fmt.Errorf("xiaomi health cloud: bad request url for %s", ep.path)
	}
	if method != http.MethodGet {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	region := s.opts.Region
	if region == "" {
		region = "cn"
	}
	req.Header.Set("region_tag", region)
	req.Header.Set("HandleParams", "true")
	return req, nil
}
