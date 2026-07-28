package main

import (
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

func TestApplyProfileFields(t *testing.T) {
	secrets := []config.Profile{
		{Name: "main", Username: "alice", Domain: "flatex.at", Password: "pw1"},
	}
	applyProfileFields(secrets, 0, "", "", "pw2")
	if secrets[0].Username != "alice" || secrets[0].Domain != "flatex.at" || secrets[0].Password != "pw2" {
		t.Fatalf("password-only update: got %+v", secrets[0])
	}
	applyProfileFields(secrets, 0, "flatex.de", "bob", "")
	if secrets[0].Username != "bob" || secrets[0].Domain != "flatex.de" || secrets[0].Password != "pw2" {
		t.Fatalf("username+domain update: got %+v", secrets[0])
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
	secrets, err := config.LoadSecrets(dir, []byte("pp"))
	if err != nil || len(secrets) != 1 || secrets[0].Username != "alice" || secrets[0].Password != "pw1" || secrets[0].Domain != "flatex.at" {
		t.Fatalf("secrets = %+v, err = %v", secrets, err)
	}
}

func TestRunProfileAddDomainFlag(t *testing.T) {
	isolateConfigDir(t)
	t.Setenv("FLATEX_FETCH_PASSPHRASE", "pp")
	t.Setenv("FLATEX_FETCH_USERNAME", "alice")
	t.Setenv("FLATEX_FETCH_PASSWORD", "pw1")

	if got := runProfile([]string{"add", "main", "-domain", "flatex.de"}); got != 0 {
		t.Fatalf("runProfile(add -domain) = %d, want 0", got)
	}
	dir, _ := config.Dir()
	secrets, err := config.LoadSecrets(dir, []byte("pp"))
	if err != nil || len(secrets) != 1 || secrets[0].Domain != "flatex.de" {
		t.Fatalf("secrets = %+v, err = %v", secrets, err)
	}
}

func TestRunProfileAddDuplicateRejected(t *testing.T) {
	isolateConfigDir(t)
	t.Setenv("FLATEX_FETCH_PASSPHRASE", "pp")
	t.Setenv("FLATEX_FETCH_USERNAME", "alice")
	t.Setenv("FLATEX_FETCH_PASSWORD", "pw1")

	runProfile([]string{"add", "main"})
	if got := runProfile([]string{"add", "main"}); got != 1 {
		t.Fatalf("duplicate runProfile(add) = %d, want 1", got)
	}
}

func TestRunProfileListEmptyNoPrompt(t *testing.T) {
	isolateConfigDir(t)
	// No FLATEX_FETCH_PASSPHRASE set, and no credentials.enc yet: must not
	// try to prompt (which would fail/hang with no TTY in a test).
	if got := runProfile([]string{"list"}); got != 0 {
		t.Fatalf("runProfile(list) on empty dir = %d, want 0", got)
	}
}

func TestRunProfileListAfterAdd(t *testing.T) {
	isolateConfigDir(t)
	t.Setenv("FLATEX_FETCH_PASSPHRASE", "pp")
	t.Setenv("FLATEX_FETCH_USERNAME", "alice")
	t.Setenv("FLATEX_FETCH_PASSWORD", "pw1")
	runProfile([]string{"add", "main"})

	if got := runProfile([]string{"list"}); got != 0 {
		t.Fatalf("runProfile(list) = %d, want 0", got)
	}
}

func TestRunProfileUpdateUsernameBlankKeepsCurrent(t *testing.T) {
	isolateConfigDir(t)
	t.Setenv("FLATEX_FETCH_PASSPHRASE", "pp")
	t.Setenv("FLATEX_FETCH_USERNAME", "alice")
	t.Setenv("FLATEX_FETCH_PASSWORD", "pw1")
	runProfile([]string{"add", "main"})

	t.Setenv("FLATEX_FETCH_USERNAME", "")
	t.Setenv("FLATEX_FETCH_PASSWORD", "pw2")
	withStdin(t, "\n")
	if got := runProfile([]string{"update", "main"}); got != 0 {
		t.Fatalf("runProfile(update) = %d, want 0", got)
	}
	dir, _ := config.Dir()
	secrets, err := config.LoadSecrets(dir, []byte("pp"))
	if err != nil || len(secrets) != 1 || secrets[0].Username != "alice" || secrets[0].Password != "pw2" {
		t.Fatalf("secrets = %+v, err = %v", secrets, err)
	}
}

func TestRunProfileUpdateDomainFlag(t *testing.T) {
	isolateConfigDir(t)
	t.Setenv("FLATEX_FETCH_PASSPHRASE", "pp")
	t.Setenv("FLATEX_FETCH_USERNAME", "alice")
	t.Setenv("FLATEX_FETCH_PASSWORD", "pw1")
	runProfile([]string{"add", "main"})

	if got := runProfile([]string{"update", "main", "-domain", "flatex.de"}); got != 0 {
		t.Fatalf("runProfile(update -domain) = %d, want 0", got)
	}
	dir, _ := config.Dir()
	secrets, err := config.LoadSecrets(dir, []byte("pp"))
	if err != nil || len(secrets) != 1 || secrets[0].Domain != "flatex.de" || secrets[0].Username != "alice" || secrets[0].Password != "pw1" {
		t.Fatalf("secrets = %+v, err = %v", secrets, err)
	}
}

func TestRunProfileUpdateMissing(t *testing.T) {
	isolateConfigDir(t)
	// No credentials.enc at all: must fail before any prompt.
	if got := runProfile([]string{"update", "ghost"}); got != 1 {
		t.Fatalf("runProfile(update ghost) on empty dir = %d, want 1", got)
	}
}

func TestRunProfileRemove(t *testing.T) {
	isolateConfigDir(t)
	t.Setenv("FLATEX_FETCH_PASSPHRASE", "pp")
	t.Setenv("FLATEX_FETCH_USERNAME", "alice")
	t.Setenv("FLATEX_FETCH_PASSWORD", "pw1")
	runProfile([]string{"add", "main"})

	if got := runProfile([]string{"remove", "main"}); got != 0 {
		t.Fatalf("runProfile(remove) = %d, want 0", got)
	}
	dir, _ := config.Dir()
	secrets, err := config.LoadSecrets(dir, []byte("pp"))
	if err != nil || len(secrets) != 0 {
		t.Fatalf("secrets = %+v, err = %v, want empty", secrets, err)
	}
}

func TestRunProfileRemoveMissing(t *testing.T) {
	isolateConfigDir(t)
	// No credentials.enc at all: must fail before any prompt.
	if got := runProfile([]string{"remove", "ghost"}); got != 1 {
		t.Fatalf("runProfile(remove ghost) on empty dir = %d, want 1", got)
	}
}
