package cloud

import (
	"crypto/rc4"  //nolint:gosec // health cloud params are rc4-encrypted by protocol
	"crypto/sha1" //nolint:gosec // health cloud signatures are sha1 by protocol
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

var b64 = base64.StdEncoding

// newNonce mirrors i83.a: base64(int64 random || int32 big-endian minutes since epoch).
func newNonce(rnd io.Reader, now time.Time) (string, error) {
	var b [12]byte
	if _, err := io.ReadFull(rnd, b[:8]); err != nil {
		return "", err
	}
	binary.BigEndian.PutUint32(b[8:], uint32(now.UnixMilli()/60000)) //nolint:gosec // java int truncation of minutes
	return b64.EncodeToString(b[:]), nil
}

// signedNonce mirrors n83: base64(sha256(b64dec(ssecurity) || b64dec(nonce))).
func signedNonce(ssecurity, nonce string) (string, error) {
	sec, err := b64.DecodeString(ssecurity)
	if err != nil {
		return "", fmt.Errorf("ssecurity: %w", err)
	}
	n, err := b64.DecodeString(nonce)
	if err != nil {
		return "", fmt.Errorf("nonce: %w", err)
	}
	h := sha256.New()
	h.Write(sec)
	h.Write(n)
	return b64.EncodeToString(h.Sum(nil)), nil
}

// rc4Stream mirrors qye: rc4 keyed with the raw signed nonce, first 1024 keystream bytes dropped.
func rc4Stream(signed string) (*rc4.Cipher, error) {
	key, err := b64.DecodeString(signed)
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, errors.New("rc4 key length is invalid")
	}
	c, err := rc4.NewCipher(key) //nolint:gosec // protocol-defined cipher
	if err != nil {
		return nil, err
	}
	var drop [1024]byte
	c.XORKeyStream(drop[:], drop[:])
	return c, nil
}

// sha1Sign mirrors i83.b: base64(sha1("METHOD&path&k=v...&signedNonce")), params sorted by key.
func sha1Sign(method, path string, params map[string]string, signed string) string {
	parts := make([]string, 0, len(params)+3)
	parts = append(parts, strings.ToUpper(method), path)
	for _, k := range sortedKeys(params) {
		parts = append(parts, k+"="+params[k])
	}
	parts = append(parts, signed)
	sum := sha1.Sum([]byte(strings.Join(parts, "&"))) //nolint:gosec // protocol-defined signature
	return b64.EncodeToString(sum[:])
}

// encryptParams mirrors n83.c: returns the request params for an encrypted call
// (each non-empty param and rc4_hash__ rc4-encrypted, plus signature and _nonce).
func encryptParams(method, path string, params map[string]string, nonce, ssecurity string) (map[string]string, error) {
	signed, err := signedNonce(ssecurity, nonce)
	if err != nil {
		return nil, err
	}
	plain := map[string]string{}
	for k, v := range params {
		if k != "" && v != "" {
			plain[k] = v
		}
	}
	plain["rc4_hash__"] = sha1Sign(method, path, plain, signed)

	c, err := rc4Stream(signed)
	if err != nil {
		return nil, err
	}
	// the keystream continues across values in sorted key order
	enc := map[string]string{}
	for _, k := range sortedKeys(plain) {
		b := []byte(plain[k])
		c.XORKeyStream(b, b)
		enc[k] = b64.EncodeToString(b)
	}
	out := map[string]string{
		"signature": sha1Sign(method, path, enc, signed),
		"_nonce":    nonce,
	}
	for k, v := range enc {
		out[k] = v
	}
	return out, nil
}

// decryptBody mirrors n83.b: a fresh rc4 stream over the base64-decoded response body.
func decryptBody(body, nonce, ssecurity string) ([]byte, error) {
	signed, err := signedNonce(ssecurity, nonce)
	if err != nil {
		return nil, err
	}
	raw, err := b64.DecodeString(strings.TrimSpace(body))
	if err != nil {
		return nil, fmt.Errorf("response is not base64: %w", err)
	}
	c, err := rc4Stream(signed)
	if err != nil {
		return nil, err
	}
	c.XORKeyStream(raw, raw)
	return raw, nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
