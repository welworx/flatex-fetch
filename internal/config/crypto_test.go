package config

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestSecretsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	pass := []byte("hunter2")
	profiles := []Profile{
		{Name: "main", Username: "alice", Domain: "flatex.at", Password: "pw-a"},
		{Name: "second", Username: "bob", Domain: "flatex.at", Password: "pw-b"},
	}
	if err := SaveSecrets(dir, pass, profiles); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSecrets(dir, pass)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != profiles[0] || got[1] != profiles[1] {
		t.Fatalf("got %+v, want %+v", got, profiles)
	}
}

func TestSecretsWrongPassphrase(t *testing.T) {
	dir := t.TempDir()
	if err := SaveSecrets(dir, []byte("right"), []Profile{{Name: "a", Password: "b"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSecrets(dir, []byte("wrong")); err == nil {
		t.Fatal("wrong passphrase decrypted successfully")
	}
}

func TestSecretsMissingFile(t *testing.T) {
	got, err := LoadSecrets(t.TempDir(), []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %v, want empty", got)
	}
}

// legacyFixture writes a v1-format credentials.enc directly (bypassing
// SaveCredentials, so this test doesn't depend on that function surviving
// future cleanup) and, if profilesJSON is non-nil, a sibling profiles.json.
func legacyFixture(t *testing.T, dir string, pass []byte, passwords map[string]string, profilesJSON []Profile) {
	t.Helper()
	legacyPT, err := json.Marshal(passwords)
	if err != nil {
		t.Fatal(err)
	}
	blob, err := encrypt(pass, legacyBlobVersion, legacyPT)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(credPath(dir), blob, 0o600); err != nil {
		t.Fatal(err)
	}
	if profilesJSON != nil {
		data, err := json.Marshal(profilesJSON)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "profiles.json"), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSecretsMigrationFromLegacy(t *testing.T) {
	dir := t.TempDir()
	pass := []byte("hunter2")
	legacyFixture(t, dir, pass,
		map[string]string{"main": "pw-a", "second": "pw-b"},
		[]Profile{
			{Name: "main", Username: "alice", Domain: "flatex.at"},
			{Name: "second", Username: "bob", Domain: "flatex.at"},
		})

	got, err := LoadSecrets(dir, pass)
	if err != nil {
		t.Fatal(err)
	}
	want := []Profile{
		{Name: "main", Username: "alice", Domain: "flatex.at", Password: "pw-a"},
		{Name: "second", Username: "bob", Domain: "flatex.at", Password: "pw-b"},
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %+v, want %+v", got, want)
	}

	if _, err := os.Stat(filepath.Join(dir, "profiles.json")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("profiles.json should be renamed away, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "profiles.json.bak")); err != nil {
		t.Fatalf("profiles.json.bak missing: %v", err)
	}

	// second load reads v2 directly, no re-migration.
	got2, err := LoadSecrets(dir, pass)
	if err != nil {
		t.Fatal(err)
	}
	if len(got2) != 2 || got2[0] != want[0] || got2[1] != want[1] {
		t.Fatalf("second load: got %+v, want %+v", got2, want)
	}
}

func TestSecretsMigrationMissingProfilesJSON(t *testing.T) {
	dir := t.TempDir()
	pass := []byte("hunter2")
	legacyFixture(t, dir, pass, map[string]string{"orphan": "pw-o"}, nil)

	got, err := LoadSecrets(dir, pass)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "orphan" || got[0].Password != "pw-o" || got[0].Username != "" || got[0].Domain != "" {
		t.Fatalf("got %+v, want orphan/pw-o with empty username/domain", got)
	}
}
