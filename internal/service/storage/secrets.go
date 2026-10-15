package storage

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// Credentials are passed directly to the SDK, never returned through the API.
type Credentials struct {
	AccessKeyID     string
	SecretAccessKey string
}
type credentialCipher struct{ path string }

func newCredentialCipher(path string) *credentialCipher { return &credentialCipher{path: path} }

var errKey = errors.New("storage encryption key is missing or invalid; restore storage.key with the database")
var credentialAAD = []byte("dd-portal/storage-credentials/v1")

func (c *credentialCipher) key(create bool) ([]byte, error) {
	info, err := os.Lstat(c.path)
	if os.IsNotExist(err) && create {
		key := make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, errKey
		}
		// Publish a fully written private file atomically; never replace another key.
		f, e := os.CreateTemp(filepath.Dir(c.path), ".storage-key-*")
		if e != nil {
			return nil, errKey
		}
		defer func() { _ = os.Remove(f.Name()) }()
		if _, e = f.Write(key); e == nil {
			e = f.Sync()
		}
		closeErr := f.Close()
		if e != nil || closeErr != nil {
			return nil, errKey
		}
		if e = os.Link(f.Name(), c.path); e != nil && !os.IsExist(e) {
			return nil, errKey
		}
		info, err = os.Lstat(c.path)
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return nil, errKey
	}
	// #nosec G304 -- Derived from the server's database path; type/inode/permissions are checked.
	f, err := os.Open(c.path)
	if err != nil {
		return nil, errKey
	}
	defer func() { _ = f.Close() }()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, errKey
	}
	key, err := io.ReadAll(io.LimitReader(f, 33))
	if err != nil || len(key) != 32 {
		return nil, errKey
	}
	return key, nil
}
func (c *credentialCipher) aead(create bool) (cipher.AEAD, error) {
	key, err := c.key(create)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errKey
	}
	return cipher.NewGCM(block)
}
func (c *credentialCipher) Seal(creds Credentials) ([]byte, error) {
	a, err := c.aead(true)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(creds)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, a.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	out := append([]byte{1}, nonce...)
	return a.Seal(out, nonce, data, credentialAAD), nil
}
func (c *credentialCipher) Open(data []byte) (Credentials, error) {
	var creds Credentials
	a, err := c.aead(false)
	if err != nil {
		return creds, err
	}
	n := a.NonceSize()
	if len(data) < 1+n+a.Overhead() || data[0] != 1 {
		return creds, errKey
	}
	plain, err := a.Open(nil, data[1:1+n], data[1+n:], credentialAAD)
	if err != nil {
		return creds, errKey
	}
	if json.Unmarshal(plain, &creds) != nil || creds.AccessKeyID == "" || creds.SecretAccessKey == "" {
		return Credentials{}, errKey
	}
	return creds, nil
}
