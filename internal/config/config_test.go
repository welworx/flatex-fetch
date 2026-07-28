package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestProfilesRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "flatex-fetch")
	ps := []Profile{{Name: "main", Username: "alice", Domain: "flatex.at"}}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(ps)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "profiles.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadProfiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != ps[0] {
		t.Fatalf("got %+v, want %+v", got, ps)
	}
}

func TestLoadProfilesMissingFile(t *testing.T) {
	got, err := LoadProfiles(t.TempDir())
	if err != nil || got != nil {
		t.Fatalf("missing file: got %v, %v; want nil, nil", got, err)
	}
}

func TestDirRespectsConfigDirOverride(t *testing.T) {
	want := filepath.Join(t.TempDir(), "custom")
	t.Setenv("FLATEX_FETCH_CONFIG_DIR", want)
	got, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Dir() = %q, want %q", got, want)
	}
}
