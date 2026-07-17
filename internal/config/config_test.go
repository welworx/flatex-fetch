package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProfilesRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "flatex-fetch")
	ps := []Profile{{Name: "main", Username: "alice", Domain: "flatex.at"}}
	if err := SaveProfiles(dir, ps); err != nil {
		t.Fatal(err)
	}
	got, err := LoadProfiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != ps[0] {
		t.Fatalf("got %+v, want %+v", got, ps)
	}
	info, err := os.Stat(filepath.Join(dir, "profiles.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("profiles.json mode = %o, want 600", info.Mode().Perm())
	}
}

func TestLoadProfilesMissingFile(t *testing.T) {
	got, err := LoadProfiles(t.TempDir())
	if err != nil || got != nil {
		t.Fatalf("missing file: got %v, %v; want nil, nil", got, err)
	}
}
