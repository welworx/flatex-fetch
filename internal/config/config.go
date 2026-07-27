// Package config owns ~/.config/flatex-fetch: profiles.json (plaintext
// metadata) and credentials.enc (passphrase-encrypted passwords).
package config

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

type Profile struct {
	Name     string `json:"name"`
	Username string `json:"username"`
	Domain   string `json:"domain"`
}

// Dir returns the config directory (not created yet). Honors
// FLATEX_FETCH_CONFIG_DIR when set, so profiles.json/credentials.enc can
// live somewhere other than the OS default — profiles.json holds usernames
// and domains in plaintext, which some setups want off the default disk
// location entirely (e.g. an encrypted volume).
func Dir() (string, error) {
	if d := os.Getenv("FLATEX_FETCH_CONFIG_DIR"); d != "" {
		return d, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "flatex-fetch"), nil
}

func LoadProfiles(dir string) ([]Profile, error) {
	data, err := os.ReadFile(filepath.Join(dir, "profiles.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ps []Profile
	if err := json.Unmarshal(data, &ps); err != nil {
		return nil, err
	}
	return ps, nil
}

func SaveProfiles(dir string, ps []Profile) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// MkdirAll doesn't tighten permissions on a pre-existing dir; credentials
	// are stored here too, so enforce 0700 explicitly.
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(ps, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "profiles.json"), data, 0o600)
}
