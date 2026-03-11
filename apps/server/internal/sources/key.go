package sources

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/KevinBonnoron/viyu/apps/server/internal/apierr"
)

// keyFileEnv names the master key file when Options.KeyFile is empty.
const keyFileEnv = "VIYU_SOURCE_KEY_FILE"

// defaultKeyFile is the master key's name inside the data directory.
const defaultKeyFile = "source_connections.key"

// sealedPrefix versions the stored ciphertext.
const sealedPrefix = "v1."

// newCipher builds the AEAD of the master key.
func newCipher(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// masterKey returns the key that encrypts every stored source session. It lives in override (an absolute
// path), or in <dataDir>/source_connections.key when override is empty: keeping it elsewhere (another
// volume, a secret mount) means a copy or backup of the data directory alone does not contain both the
// ciphertext and its key.
//
// A complete key is hard-linked into place, never overwritten. Concurrent initializers therefore cannot
// read a partly written key or replace one used to encrypt an existing connection. Tempfiles and the
// installed key are 0600.
func masterKey(dataDir, override string) ([]byte, error) {
	path := filepath.Join(dataDir, defaultKeyFile)
	if override != "" {
		if !filepath.IsAbs(override) {
			return nil, fmt.Errorf("%s must be an absolute path", keyFileEnv)
		}
		path = filepath.Clean(override)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
	}
	readKey := func() ([]byte, error) {
		f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
			return nil, fmt.Errorf("source encryption key must be a private regular file")
		}
		key, err := io.ReadAll(io.LimitReader(f, 33))
		if err != nil || len(key) != 32 {
			return nil, fmt.Errorf("source encryption key is invalid")
		}
		return key, nil
	}
	if key, err := readKey(); err == nil {
		return key, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".source-key-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(key); err != nil {
		_ = f.Close()
		return nil, err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	if err = os.Link(f.Name(), path); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	return readKey()
}

// aad binds a ciphertext to the account and platform it was made for, so a record copied onto another
// account or platform does not decrypt.
func aad(userID, platformID string) []byte {
	return []byte("viyu-source-v1\x00" + userID + "\x00" + platformID)
}

func (s *Service) encrypt(userID, platformID, cookies string) (string, error) {
	nonce := make([]byte, s.cipher.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", apierr.RequestFailed().WithCause(err)
	}
	sealed := s.cipher.Seal(nonce, nonce, []byte(cookies), aad(userID, platformID))
	return sealedPrefix + base64.RawStdEncoding.EncodeToString(sealed), nil
}

func (s *Service) decrypt(encoded, userID, platformID string) (string, error) {
	if !strings.HasPrefix(encoded, sealedPrefix) {
		return "", apierr.RequestFailed().WithCause(errors.New("unknown source ciphertext version"))
	}
	sealed, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(encoded, sealedPrefix))
	if err != nil || len(sealed) < s.cipher.NonceSize()+s.cipher.Overhead() {
		return "", apierr.RequestFailed().WithCause(errors.New("malformed source ciphertext"))
	}
	nonce := sealed[:s.cipher.NonceSize()]
	plain, err := s.cipher.Open(nil, nonce, sealed[s.cipher.NonceSize():], aad(userID, platformID))
	if err != nil {
		return "", apierr.RequestFailed().WithCause(err)
	}
	return string(plain), nil
}
