package cloud

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"strconv"
	"testing"
	"time"
)

// golden.json is produced by research/golden/cloud/run.sh from the Mi Fitness classes
type golden struct {
	Nonce []struct {
		Random string `json:"random"`
		UnixMS string `json:"unix_ms"`
		Out    string `json:"out"`
	} `json:"nonce"`
	SignedNonce []struct {
		Ssecurity, Nonce, Out string
	} `json:"signed_nonce"`
	RC4 []struct {
		Key string   `json:"key"`
		In  []string `json:"in"`
		Out []string `json:"out"`
	} `json:"rc4"`
	SHA1Sign []struct {
		Method, Path, Key, Out string
		Params                 map[string]string
	} `json:"sha1_sign"`
	Encrypt []struct {
		Method, Path, Nonce, Ssecurity string
		Params, Out                    map[string]string
	} `json:"encrypt"`
	Decrypt []struct {
		Nonce, Ssecurity, Body, Out string
	} `json:"decrypt"`
	PasswordHash []struct {
		In, Out string
	} `json:"password_hash"`
	ClientSign []struct {
		Nonce, Ssecurity, Out string
	} `json:"client_sign"`
}

func loadGolden(t *testing.T) golden {
	t.Helper()
	raw, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var g golden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	return g
}

func TestNonceMatchesApp(t *testing.T) {
	g := loadGolden(t)
	if len(g.Nonce) == 0 {
		t.Fatal("no vectors")
	}
	for _, v := range g.Nonce {
		r, _ := strconv.ParseInt(v.Random, 10, 64)
		ms, _ := strconv.ParseInt(v.UnixMS, 10, 64)
		var rb [8]byte
		binary.BigEndian.PutUint64(rb[:], uint64(r)) //nolint:gosec // bit pattern of a java long
		got, err := newNonce(bytes.NewReader(rb[:]), time.UnixMilli(ms))
		if err != nil {
			t.Fatal(err)
		}
		if got != v.Out {
			t.Errorf("nonce(%d): got %s want %s", r, got, v.Out)
		}
	}
}

func TestSignedNonceMatchesApp(t *testing.T) {
	for _, v := range loadGolden(t).SignedNonce {
		got, err := signedNonce(v.Ssecurity, v.Nonce)
		if err != nil {
			t.Fatal(err)
		}
		if got != v.Out {
			t.Errorf("got %s want %s", got, v.Out)
		}
	}
}

func TestRC4StreamContinuesAcrossValues(t *testing.T) {
	for _, v := range loadGolden(t).RC4 {
		c, err := rc4Stream(v.Key)
		if err != nil {
			t.Fatal(err)
		}
		for i, in := range v.In {
			b := []byte(in)
			c.XORKeyStream(b, b)
			if got := b64.EncodeToString(b); got != v.Out[i] {
				t.Errorf("value %d: got %s want %s", i, got, v.Out[i])
			}
		}
	}
}

func TestSHA1SignMatchesApp(t *testing.T) {
	for _, v := range loadGolden(t).SHA1Sign {
		if got := sha1Sign(v.Method, v.Path, v.Params, v.Key); got != v.Out {
			t.Errorf("%s %s: got %s want %s", v.Method, v.Path, got, v.Out)
		}
	}
}

func TestEncryptParamsMatchesApp(t *testing.T) {
	g := loadGolden(t)
	if len(g.Encrypt) == 0 {
		t.Fatal("no vectors")
	}
	for _, v := range g.Encrypt {
		got, err := encryptParams(v.Method, v.Path, v.Params, v.Nonce, v.Ssecurity)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != len(v.Out) {
			t.Errorf("%s: got %v want %v", v.Path, got, v.Out)
		}
		for k, want := range v.Out {
			if got[k] != want {
				t.Errorf("%s %s: got %q want %q", v.Path, k, got[k], want)
			}
		}
	}
}

func TestDecryptBodyMatchesApp(t *testing.T) {
	for _, v := range loadGolden(t).Decrypt {
		got, err := decryptBody(v.Body, v.Nonce, v.Ssecurity)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != v.Out {
			t.Errorf("got %s want %s", got, v.Out)
		}
	}
}

func TestPasswordHashMatchesAccountSDK(t *testing.T) {
	g := loadGolden(t)
	if len(g.PasswordHash) == 0 {
		t.Fatal("no vectors")
	}
	for _, v := range g.PasswordHash {
		if got := passwordHash(v.In); got != v.Out {
			t.Errorf("%q: got %s want %s", v.In, got, v.Out)
		}
	}
}

func TestClientSignMatchesAccountSDK(t *testing.T) {
	g := loadGolden(t)
	if len(g.ClientSign) == 0 {
		t.Fatal("no vectors")
	}
	for _, v := range g.ClientSign {
		if got := clientSign(v.Nonce, v.Ssecurity); got != v.Out {
			t.Errorf("nonce %s: got %s want %s", v.Nonce, got, v.Out)
		}
	}
}
