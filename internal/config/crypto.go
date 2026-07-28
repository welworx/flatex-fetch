package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/crypto/argon2"
)

// credentials.enc layout: [1 byte version][16 salt][12 nonce][ciphertext].
// Key = argon2id(passphrase, salt, t=1, m=64MiB, p=4, len=32); AES-256-GCM.
//
// Version 1 (legacy): payload is map[string]string, profile name -> portal
// password only. profiles.json (plaintext, name/username/domain) lived
// alongside it.
//
// Version 2 (current): payload is []Profile, each entry carrying
// name/username/domain/password together. profiles.json is retired;
// LoadSecrets migrates a version-1 file (plus its profiles.json, if still
// present) to version 2 on first read.
const (
	legacyBlobVersion  = 1
	currentBlobVersion = 2
	saltLen            = 16
	nonceLen           = 12
	keyLen             = 32
	argonTime          = 1
	argonMemKiB        = 64 * 1024
	argonThreads       = 4
)

func credPath(dir string) string { return filepath.Join(dir, "credentials.enc") }

// CredentialsExist reports whether a credentials.enc file exists in dir.
func CredentialsExist(dir string) bool {
	_, err := os.Stat(credPath(dir))
	return err == nil
}

// LoadCredentials reads the legacy (version 1) password-only format.
// Superseded by LoadSecrets; kept until cli_profile.go stops calling it.
func LoadCredentials(dir string, passphrase []byte) (map[string]string, error) {
	blob, err := os.ReadFile(credPath(dir))
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	version, pt, err := decrypt(passphrase, blob)
	if err != nil {
		return nil, err
	}
	if version != legacyBlobVersion {
		return nil, errors.New("credentials file corrupt or unsupported version")
	}
	creds := map[string]string{}
	if err := json.Unmarshal(pt, &creds); err != nil {
		return nil, err
	}
	return creds, nil
}

// SaveCredentials writes the legacy (version 1) password-only format.
// Superseded by SaveSecrets; kept until cli_profile.go stops calling it.
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
	blob, err := encrypt(passphrase, legacyBlobVersion, pt)
	if err != nil {
		return err
	}
	return os.WriteFile(credPath(dir), blob, 0o600)
}

// LoadSecrets returns every profile (name, username, domain, password)
// stored in dir, decrypted with passphrase. A missing credentials.enc
// returns an empty slice, no error, no decryption attempted. A version-1
// file is transparently migrated to version 2 (see migrateLegacy) and
// re-saved before returning.
func LoadSecrets(dir string, passphrase []byte) ([]Profile, error) {
	blob, err := os.ReadFile(credPath(dir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	version, pt, err := decrypt(passphrase, blob)
	if err != nil {
		return nil, err
	}
	switch version {
	case currentBlobVersion:
		var profiles []Profile
		if err := json.Unmarshal(pt, &profiles); err != nil {
			return nil, err
		}
		return profiles, nil
	case legacyBlobVersion:
		return migrateLegacy(dir, passphrase, pt)
	default:
		return nil, fmt.Errorf("credentials file has unsupported version %d", version)
	}
}

// SaveSecrets encrypts and writes every profile in profiles to dir's
// credentials.enc, always in the current (version 2) format.
func SaveSecrets(dir string, passphrase []byte, profiles []Profile) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	pt, err := json.Marshal(profiles)
	if err != nil {
		return err
	}
	blob, err := encrypt(passphrase, currentBlobVersion, pt)
	if err != nil {
		return err
	}
	return os.WriteFile(credPath(dir), blob, 0o600)
}

// migrateLegacy folds a decrypted version-1 payload (name -> password) and
// dir's sibling profiles.json (if present) into version-2 []Profile,
// preserving profiles.json's order. A name with no profiles.json entry
// (profiles.json missing or out of sync) gets empty username/domain and a
// warning. The merged result is saved as version 2 immediately, and
// profiles.json is renamed to profiles.json.bak.
func migrateLegacy(dir string, passphrase []byte, legacyPT []byte) ([]Profile, error) {
	var passwords map[string]string
	if err := json.Unmarshal(legacyPT, &passwords); err != nil {
		return nil, err
	}

	metaPath := filepath.Join(dir, "profiles.json")
	meta, err := LoadProfiles(dir)
	if err != nil {
		return nil, err
	}

	fmt.Fprintf(os.Stderr, "migrating %s to combined format\n", credPath(dir))

	merged := make([]Profile, 0, len(meta))
	seen := make(map[string]bool, len(meta))
	for _, p := range meta {
		pw, ok := passwords[p.Name]
		if !ok {
			continue
		}
		merged = append(merged, Profile{Name: p.Name, Username: p.Username, Domain: p.Domain, Password: pw})
		seen[p.Name] = true
	}
	for name, pw := range passwords {
		if seen[name] {
			continue
		}
		fmt.Fprintf(os.Stderr, "warning: %s has no profiles.json entry, migrated with empty username/domain (run: flatex-fetch profile update %s)\n", name, name)
		merged = append(merged, Profile{Name: name, Password: pw})
	}

	if err := SaveSecrets(dir, passphrase, merged); err != nil {
		return nil, err
	}
	if _, err := os.Stat(metaPath); err == nil {
		if err := os.Rename(metaPath, metaPath+".bak"); err != nil {
			return nil, err
		}
	}
	return merged, nil
}

func deriveKey(passphrase, salt []byte) []byte {
	return argon2.IDKey(passphrase, salt, argonTime, argonMemKiB, argonThreads, keyLen)
}

func encrypt(passphrase []byte, version byte, plaintext []byte) ([]byte, error) {
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
	out := append([]byte{version}, salt...)
	out = append(out, nonce...)
	return gcm.Seal(out, nonce, plaintext, nil), nil
}

func decrypt(passphrase, blob []byte) (byte, []byte, error) {
	if len(blob) < 1+saltLen+nonceLen {
		return 0, nil, errors.New("credentials file corrupt or unsupported version")
	}
	version := blob[0]
	salt := blob[1 : 1+saltLen]
	nonce := blob[1+saltLen : 1+saltLen+nonceLen]
	block, err := aes.NewCipher(deriveKey(passphrase, salt))
	if err != nil {
		return 0, nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return 0, nil, err
	}
	pt, err := gcm.Open(nil, nonce, blob[1+saltLen+nonceLen:], nil)
	if err != nil {
		return 0, nil, errors.New("wrong passphrase or corrupt credentials file")
	}
	return version, pt, nil
}
