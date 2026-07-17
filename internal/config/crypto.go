package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/crypto/argon2"
)

// credentials.enc layout: [1 byte version=1][16 salt][12 nonce][ciphertext].
// Key = argon2id(passphrase, salt, t=1, m=64MiB, p=4, len=32); AES-256-GCM.
const (
	blobVersion  = 1
	saltLen      = 16
	nonceLen     = 12
	keyLen       = 32
	argonTime    = 1
	argonMemKiB  = 64 * 1024
	argonThreads = 4
)

func credPath(dir string) string { return filepath.Join(dir, "credentials.enc") }

// CredentialsExist reports whether a credentials.enc file exists in dir.
func CredentialsExist(dir string) bool {
	_, err := os.Stat(credPath(dir))
	return err == nil
}

func LoadCredentials(dir string, passphrase []byte) (map[string]string, error) {
	blob, err := os.ReadFile(credPath(dir))
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	pt, err := decrypt(passphrase, blob)
	if err != nil {
		return nil, err
	}
	creds := map[string]string{}
	if err := json.Unmarshal(pt, &creds); err != nil {
		return nil, err
	}
	return creds, nil
}

func SaveCredentials(dir string, passphrase []byte, creds map[string]string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	pt, err := json.Marshal(creds)
	if err != nil {
		return err
	}
	blob, err := encrypt(passphrase, pt)
	if err != nil {
		return err
	}
	return os.WriteFile(credPath(dir), blob, 0o600)
}

func deriveKey(passphrase, salt []byte) []byte {
	return argon2.IDKey(passphrase, salt, argonTime, argonMemKiB, argonThreads, keyLen)
}

func encrypt(passphrase, plaintext []byte) ([]byte, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	nonce := make([]byte, nonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(deriveKey(passphrase, salt))
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	out := append([]byte{blobVersion}, salt...)
	out = append(out, nonce...)
	return gcm.Seal(out, nonce, plaintext, nil), nil
}

func decrypt(passphrase, blob []byte) ([]byte, error) {
	if len(blob) < 1+saltLen+nonceLen || blob[0] != blobVersion {
		return nil, errors.New("credentials file corrupt or unsupported version")
	}
	salt := blob[1 : 1+saltLen]
	nonce := blob[1+saltLen : 1+saltLen+nonceLen]
	block, err := aes.NewCipher(deriveKey(passphrase, salt))
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	pt, err := gcm.Open(nil, nonce, blob[1+saltLen+nonceLen:], nil)
	if err != nil {
		return nil, errors.New("wrong passphrase or corrupt credentials file")
	}
	return pt, nil
}
