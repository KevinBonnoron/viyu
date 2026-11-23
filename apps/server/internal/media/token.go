package media

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"sync"
)

const maxResourceTokenSize = 32 * 1024

var errResourceTokenTooLarge = errors.New("resource token too large")

type resourceToken struct {
	URL        string `json:"u"`
	Parent     string `json:"p,omitempty"`
	Semantic   string `json:"s,omitempty"`
	Generation uint64 `json:"g"`
}

type resourceTokens struct {
	once sync.Once
	aead cipher.AEAD
	err  error
}

func (t *resourceTokens) initialize() {
	t.once.Do(func() {
		var key [32]byte
		if _, t.err = rand.Read(key[:]); t.err != nil {
			return
		}
		var block cipher.Block
		block, t.err = aes.NewCipher(key[:])
		if t.err == nil {
			t.aead, t.err = cipher.NewGCM(block)
		}
	})
}

func validResourceTokenURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func (t *resourceTokens) seal(value resourceToken) (string, error) {
	if value.Generation == 0 || !validResourceTokenURL(value.URL) {
		return "", errors.New("invalid resource token payload")
	}
	if len(value.URL)+len(value.Parent)+len(value.Semantic) > maxResourceTokenSize {
		return "", errResourceTokenTooLarge
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	t.initialize()
	if t.err != nil {
		return "", t.err
	}
	if 2+base64.RawURLEncoding.EncodedLen(t.aead.NonceSize()+len(payload)+t.aead.Overhead()) > maxResourceTokenSize {
		return "", errResourceTokenTooLarge
	}
	nonce := make([]byte, t.aead.NonceSize(), t.aead.NonceSize()+len(payload)+t.aead.Overhead())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := t.aead.Seal(nonce, nonce, payload, nil)
	encoded := make([]byte, 2, 2+base64.RawURLEncoding.EncodedLen(len(sealed)))
	copy(encoded, "s.")
	encoded = base64.RawURLEncoding.AppendEncode(encoded, sealed)
	return string(encoded), nil
}

func (t *resourceTokens) open(id string) (resourceToken, bool) {
	var value resourceToken
	if len(id) > maxResourceTokenSize || !strings.HasPrefix(id, "s.") {
		return value, false
	}
	sealed, err := base64.RawURLEncoding.DecodeString(id[2:])
	if err != nil {
		return value, false
	}
	t.initialize()
	if t.err != nil || len(sealed) < t.aead.NonceSize()+t.aead.Overhead() {
		return value, false
	}
	nonce := sealed[:t.aead.NonceSize()]
	ciphertext := sealed[t.aead.NonceSize():]
	payload, err := t.aead.Open(ciphertext[:0], nonce, ciphertext, nil)
	if err != nil || json.Unmarshal(payload, &value) != nil || value.Generation == 0 || !validResourceTokenURL(value.URL) {
		return resourceToken{}, false
	}
	return value, true
}
