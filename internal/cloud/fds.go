package cloud

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1" //nolint:gosec // fds object names hash the did with sha1 by protocol
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"miband/internal/fitness"
)

// FDSApiService (@BaseUrl healthapp/, @Secret pathPrefix "healthapp/")
var fdsDownloadURL = endpoint{path: "/healthapp/service/gen_download_url", prefix: "healthapp/"}

// AESCoder: AES/CBC/PKCS5Padding with a fixed iv
var fdsIV = []byte("1234567887654321")

// ErrNoRawFile means the cloud returned no download url for the requested file.
var ErrNoRawFile = errors.New("xiaomi health cloud: no stored file for this id")

// fdsItem is FDSItem; fdsRequest is FDSRequestParam (fields in Gson order).
type fdsItem struct {
	Suffix    string `json:"suffix"`
	Timestamp int64  `json:"timestamp"`
}

type fdsRequest struct {
	Items []fdsItem `json:"items"`
	DID   string    `json:"did"`
}

type fdsResult struct {
	ExpiresTime int64  `json:"expires_time"`
	Method      string `json:"method"`
	ObjKey      string `json:"obj_key"`
	ObjName     string `json:"obj_name"`
	URL         string `json:"url"`
}

// fdsKeyBytes mirrors FitnessFDSUploader.genDataIdKeyBytes: ts u32 LE, tz i8, type byte (no version).
func fdsKeyBytes(id fitness.ID) []byte {
	b := id.Bytes()
	return []byte{b[0], b[1], b[2], b[3], b[4], b[6]}
}

// fdsItemFor mirrors FitnessFDSUploader.genFdsUrlItem: b64(key bytes) + "_" + b64(sha1(did)),
// android Base64 flags 11 (URL_SAFE | NO_WRAP | NO_PADDING).
func fdsItemFor(did string, id fitness.ID) fdsItem {
	sum := sha1.Sum([]byte(did)) //nolint:gosec // protocol-defined object name hash
	enc := base64.RawURLEncoding
	return fdsItem{Suffix: enc.EncodeToString(fdsKeyBytes(id)) + "_" + enc.EncodeToString(sum[:]), Timestamp: id.Time.Unix()}
}

// serverKey mirrors FDSItem.toServerKey, the key of the gen_download_url result map.
func (it fdsItem) serverKey() string {
	return it.Suffix + "_" + strconv.FormatInt(it.Timestamp, 10)
}

// androidB64Decode mirrors android.util.Base64.decode: characters outside the alphabet
// (quotes, whitespace) are skipped and '=' ends the data.
func androidB64Decode(s string, urlSafe bool) ([]byte, error) {
	var sb strings.Builder
	for _, c := range s {
		if c == '=' {
			break
		}
		if isB64Char(c, urlSafe) {
			sb.WriteRune(c)
		}
	}
	if urlSafe {
		return base64.RawURLEncoding.DecodeString(sb.String())
	}
	return base64.RawStdEncoding.DecodeString(sb.String())
}

func isB64Char(c rune, urlSafe bool) bool {
	switch {
	case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		return true
	case urlSafe:
		return c == '-' || c == '_'
	default:
		return c == '+' || c == '/'
	}
}

// decryptFDS mirrors FitnessFDSUploader.download: the object body is the Gson-quoted
// b64url(AES-CBC(raw)) string uploaded by the app; the key is b64url(obj_key).
func decryptFDS(body, objKey string) ([]byte, error) {
	key, err := androidB64Decode(objKey, true)
	if err != nil {
		return nil, fmt.Errorf("fds obj_key: %w", err)
	}
	ct, err := androidB64Decode(body, true)
	if err != nil {
		return nil, fmt.Errorf("fds body: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("fds obj_key: %w", err)
	}
	if len(ct) == 0 || len(ct)%aes.BlockSize != 0 {
		return nil, errors.New("fds body: bad ciphertext length")
	}
	cipher.NewCBCDecrypter(block, fdsIV).CryptBlocks(ct, ct)
	return unpadPKCS7(ct)
}

func unpadPKCS7(b []byte) ([]byte, error) {
	n := int(b[len(b)-1])
	if n == 0 || n > aes.BlockSize || n > len(b) {
		return nil, errors.New("fds body: bad padding")
	}
	for _, p := range b[len(b)-n:] {
		if int(p) != n {
			return nil, errors.New("fds body: bad padding")
		}
	}
	return b[:len(b)-n], nil
}

// RawFile downloads a band file that Mi Fitness stored unparsed in Xiaomi FDS: sport record,
// GPS and recover-rate files (fitness.FileRecord, FileGPS, FileRecoverRate), ECG files and the
// sleep assist blob. did is the device id of the record (Workout.DID); id needs time, timezone,
// data type, type and file type (the version is not part of the object name). The result is the
// file in server form: server id (6 bytes daily, 7 sport) followed by the file body; see ParseRawFile.
func (s *Session) RawFile(ctx context.Context, did string, id fitness.ID) ([]byte, error) {
	it := fdsItemFor(did, id)
	var res map[string]fdsResult
	if err := s.call(ctx, http.MethodGet, fdsDownloadURL, fdsRequest{Items: []fdsItem{it}, DID: did}, &res); err != nil {
		return nil, err
	}
	v, ok := res[it.serverKey()]
	// the server signs a url for any name; without obj_key the app cannot download either
	// (FitnessFDSUploader throws), so treat it as unavailable
	if !ok || v.URL == "" || v.ObjKey == "" {
		return nil, ErrNoRawFile
	}
	// FitnessFDSUploader.downloadFromUrl: a plain OkHttp client, no cookies, redirects followed
	body, err := download(ctx, &http.Client{Transport: s.opts.HTTPClient.Transport, Timeout: s.opts.HTTPClient.Timeout}, v.URL, maxFDSObject)
	if err != nil {
		return nil, err
	}
	return decryptFDS(string(body), v.ObjKey)
}

// maxFDSObject bounds a downloaded FDS object; workout files are far smaller.
const maxFDSObject = 64 << 20

// download GETs a pre-signed object url and requires a final 200; errors never include the
// url query, which carries the signature.
func download(ctx context.Context, c *http.Client, rawURL string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return nil, errors.New("xiaomi fds: bad download url")
	}
	where := req.URL.Host + req.URL.Path
	// an explicitly empty value stops net/http from adding its own user agent
	req.Header.Set("User-Agent", "")
	resp, err := c.Do(req)
	if err != nil {
		return nil, &requestError{op: "xiaomi fds: GET " + where, cause: err}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("xiaomi fds: GET %s: HTTP %d", where, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, &requestError{op: "xiaomi fds: GET " + where, cause: err}
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("xiaomi fds: object larger than %d bytes", limit)
	}
	return body, nil
}

// serverIDLen mirrors FitnessDataId.getServerDataIdLen.
func serverIDLen(id fitness.ID) int {
	if id.IsSport() {
		return 7
	}
	return 6
}

// ParseRawFile decodes a file returned by RawFile. id describes the requested file; its version
// is taken from the file. The server id must match id (time, timezone and sport type).
func ParseRawFile(id fitness.ID, data []byte) (*fitness.File, error) {
	n := serverIDLen(id)
	if len(data) < n {
		return nil, fmt.Errorf("xiaomi health cloud: raw file shorter than its %d-byte id", n)
	}
	want := id.Bytes()
	if binary.LittleEndian.Uint32(data) != binary.LittleEndian.Uint32(want) || data[4] != want[4] ||
		(id.IsSport() && data[6] != id.Type) {
		return nil, fmt.Errorf("xiaomi health cloud: raw file id %x does not match %s", data[:n], id)
	}
	id.Version = data[5]
	return fitness.ParseBody(id, data[n:])
}
