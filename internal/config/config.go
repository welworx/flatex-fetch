// Package config owns ~/.config/flatex-fetch: credentials.enc, a single
// passphrase-encrypted file holding every profile's name/username/domain/
// password. profiles.json is a legacy, plaintext file that only exists
// transiently on upgrade from the old format — see migrateLegacy in
// crypto.go — and is renamed to profiles.json.bak once migrated.
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
	Password string `json:"password"`
}

// Dir returns the config directory (not created yet). Honors
// FLATEX_FETCH_CONFIG_DIR when set, so credentials.enc can live somewhere
// other than the OS default (e.g. an encrypted volume) — some setups want
// the encrypted credentials off the default disk location entirely.
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

// LoadProfiles reads the legacy plaintext profiles.json. Only used by
// migrateLegacy (crypto.go) to fold old metadata into credentials.enc.
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
