package main

import (
	"os"
	"testing"

	"github.com/welworx/flatex-fetch/internal/config"
)

func TestReadPassphraseFromEnv(t *testing.T) {
	t.Setenv("FLATEX_FETCH_PASSPHRASE", "envpass")
	got, err := readPassphrase(false)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "envpass" {
		t.Fatalf("got %q", got)
	}
}

func TestRunProfileAddFromEnv(t *testing.T) {
	isolateConfigDir(t)
	t.Setenv("FLATEX_FETCH_PASSPHRASE", "pp")
	t.Setenv("FLATEX_FETCH_USERNAME", "alice")
	t.Setenv("FLATEX_FETCH_PASSWORD", "pw1")

	if got := runProfile([]string{"add", "main"}); got != 0 {
		t.Fatalf("runProfile(add) = %d, want 0", got)
	}
	dir, err := config.Dir()
	if err != nil {
		t.Fatal(err)
	}
	ps, err := config.LoadProfiles(dir)
	if err != nil || len(ps) != 1 || ps[0].Username != "alice" {
		t.Fatalf("profiles = %+v, err = %v", ps, err)
	}
	creds, err := config.LoadCredentials(dir, []byte("pp"))
	if err != nil || creds["main"] != "pw1" {
		t.Fatalf("creds = %v, err = %v", creds, err)
	}
}

func TestProfileAddRemoveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FLATEX_FETCH_PASSPHRASE", "pp")

	if err := profileAdd(dir, "main", "flatex.at", "alice", "pw1"); err != nil {
		t.Fatal(err)
	}
	ps, err := config.LoadProfiles(dir)
	if err != nil || len(ps) != 1 || ps[0].Name != "main" {
		t.Fatalf("profiles = %+v, err = %v", ps, err)
	}
	creds, err := config.LoadCredentials(dir, []byte("pp"))
	if err != nil || creds["main"] != "pw1" {
		t.Fatalf("creds = %v, err = %v", creds, err)
	}

	// duplicate name rejected
	if err := profileAdd(dir, "main", "flatex.at", "bob", "pw2"); err == nil {
		t.Fatal("duplicate profile add succeeded")
	}

	if err := profileRemove(dir, "main"); err != nil {
		t.Fatal(err)
	}
	ps, _ = config.LoadProfiles(dir)
	if len(ps) != 0 {
		t.Fatalf("profiles after remove = %+v", ps)
	}
	creds, _ = config.LoadCredentials(dir, []byte("pp"))
	if len(creds) != 0 {
		t.Fatalf("creds after remove = %v", creds)
	}
	_ = os.Unsetenv("FLATEX_FETCH_PASSPHRASE")
}
