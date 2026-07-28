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

// TestProfileChangePassphraseGuardRequiresCredentials checks the precondition
// profileChangePassphrase relies on (CredentialsExist(dir)) after a normal
// profileAdd. It does not call profileChangePassphrase itself — its own
// new-passphrase step is TTY-only; see TestSecretsSurviveRekey for the
// actual rekey round trip.
func TestProfileChangePassphraseGuardRequiresCredentials(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FLATEX_FETCH_PASSPHRASE", "old-pass")
	if err := profileAdd(dir, "main", "flatex.at", "alice", "pw1"); err != nil {
		t.Fatal(err)
	}
	if !config.CredentialsExist(dir) {
		t.Fatal("credentials.enc should exist after profileAdd")
	}
}

func TestProfileChangePassphraseNoCredentials(t *testing.T) {
	dir := t.TempDir()
	if err := profileChangePassphrase(dir); err == nil {
		t.Fatal("expected error rekeying a directory with no credentials.enc")
	}
}

func TestRunProfileEmptyArgs(t *testing.T) {
	if got := runProfile(nil); got != 2 {
		t.Fatalf("runProfile(nil) = %d, want 2 (usage)", got)
	}
}

func TestRunProfileDirError(t *testing.T) {
	// No FLATEX_FETCH_CONFIG_DIR override and no $HOME: config.Dir() must
	// fail, and runProfile must surface that before dispatching. On Linux,
	// os.UserConfigDir() checks $XDG_CONFIG_HOME before $HOME, so that
	// must be cleared too (see TestDirErrorsWithoutHome in internal/config).
	t.Setenv("FLATEX_FETCH_CONFIG_DIR", "")
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	if got := runProfile([]string{"list"}); got != 1 {
		t.Fatalf("runProfile(list) with no $HOME = %d, want 1", got)
	}
}

func TestRunProfileAddUsage(t *testing.T) {
	if got := runProfile([]string{"add"}); got != 2 {
		t.Fatalf("runProfile(add) with no name = %d, want 2", got)
	}
}

func TestRunProfileAddWrongPassphrase(t *testing.T) {
	isolateConfigDir(t)
	t.Setenv("FLATEX_FETCH_PASSPHRASE", "right")
	t.Setenv("FLATEX_FETCH_USERNAME", "alice")
	t.Setenv("FLATEX_FETCH_PASSWORD", "pw1")
	runProfile([]string{"add", "main"})

	t.Setenv("FLATEX_FETCH_PASSPHRASE", "wrong")
	if got := runProfile([]string{"add", "second"}); got != 1 {
		t.Fatalf("runProfile(add) with wrong passphrase = %d, want 1", got)
	}
}

func TestRunProfileAddUsernamePromptError(t *testing.T) {
	isolateConfigDir(t)
	t.Setenv("FLATEX_FETCH_PASSPHRASE", "pp")
	t.Setenv("FLATEX_FETCH_USERNAME", "")
	t.Setenv("FLATEX_FETCH_PASSWORD", "pw1")
	withStdin(t, "") // no trailing newline: promptLine's ReadString hits EOF
	if got := runProfile([]string{"add", "main"}); got != 1 {
		t.Fatalf("runProfile(add) with username prompt EOF = %d, want 1", got)
	}
}

func TestRunProfileUpdateUsage(t *testing.T) {
	if got := runProfile([]string{"update"}); got != 2 {
		t.Fatalf("runProfile(update) with no name = %d, want 2", got)
	}
}

func TestRunProfileUpdateWrongPassphrase(t *testing.T) {
	isolateConfigDir(t)
	t.Setenv("FLATEX_FETCH_PASSPHRASE", "right")
	t.Setenv("FLATEX_FETCH_USERNAME", "alice")
	t.Setenv("FLATEX_FETCH_PASSWORD", "pw1")
	runProfile([]string{"add", "main"})

	t.Setenv("FLATEX_FETCH_PASSPHRASE", "wrong")
	if got := runProfile([]string{"update", "main"}); got != 1 {
		t.Fatalf("runProfile(update) with wrong passphrase = %d, want 1", got)
	}
}

func TestRunProfileUpdateNotFoundWithCredentials(t *testing.T) {
	isolateConfigDir(t)
	t.Setenv("FLATEX_FETCH_PASSPHRASE", "pp")
	t.Setenv("FLATEX_FETCH_USERNAME", "alice")
	t.Setenv("FLATEX_FETCH_PASSWORD", "pw1")
	runProfile([]string{"add", "main"})

	// credentials.enc exists, but "ghost" isn't in it: distinct from
	// TestRunProfileUpdateMissing, which has no credentials.enc at all.
	if got := runProfile([]string{"update", "ghost"}); got != 1 {
		t.Fatalf("runProfile(update ghost) = %d, want 1", got)
	}
}

func TestRunProfileUpdateUsernamePromptError(t *testing.T) {
	isolateConfigDir(t)
	t.Setenv("FLATEX_FETCH_PASSPHRASE", "pp")
	t.Setenv("FLATEX_FETCH_USERNAME", "alice")
	t.Setenv("FLATEX_FETCH_PASSWORD", "pw1")
	runProfile([]string{"add", "main"})

	t.Setenv("FLATEX_FETCH_USERNAME", "")
	withStdin(t, "")
	if got := runProfile([]string{"update", "main"}); got != 1 {
		t.Fatalf("runProfile(update) with username prompt EOF = %d, want 1", got)
	}
}

func TestRunProfileListWrongPassphrase(t *testing.T) {
	isolateConfigDir(t)
	t.Setenv("FLATEX_FETCH_PASSPHRASE", "right")
	t.Setenv("FLATEX_FETCH_USERNAME", "alice")
	t.Setenv("FLATEX_FETCH_PASSWORD", "pw1")
	runProfile([]string{"add", "main"})

	t.Setenv("FLATEX_FETCH_PASSPHRASE", "wrong")
	if got := runProfile([]string{"list"}); got != 1 {
		t.Fatalf("runProfile(list) with wrong passphrase = %d, want 1", got)
	}
}

func TestRunProfileRemoveUsage(t *testing.T) {
	if got := runProfile([]string{"remove"}); got != 2 {
		t.Fatalf("runProfile(remove) with no name = %d, want 2", got)
	}
}

func TestRunProfileRemoveWrongPassphrase(t *testing.T) {
	isolateConfigDir(t)
	t.Setenv("FLATEX_FETCH_PASSPHRASE", "right")
	t.Setenv("FLATEX_FETCH_USERNAME", "alice")
	t.Setenv("FLATEX_FETCH_PASSWORD", "pw1")
	runProfile([]string{"add", "main"})

	t.Setenv("FLATEX_FETCH_PASSPHRASE", "wrong")
	if got := runProfile([]string{"remove", "main"}); got != 1 {
		t.Fatalf("runProfile(remove) with wrong passphrase = %d, want 1", got)
	}
}

func TestRunProfileRemoveNotFoundAmongExisting(t *testing.T) {
	isolateConfigDir(t)
	t.Setenv("FLATEX_FETCH_PASSPHRASE", "pp")
	t.Setenv("FLATEX_FETCH_USERNAME", "alice")
	t.Setenv("FLATEX_FETCH_PASSWORD", "pw1")
	runProfile([]string{"add", "main"})
	t.Setenv("FLATEX_FETCH_USERNAME", "bob")
	t.Setenv("FLATEX_FETCH_PASSWORD", "pw2")
	runProfile([]string{"add", "second"})

	// credentials.enc has entries, but not "ghost": exercises the kept-loop
	// (non-matching entries get re-appended) and the not-found return.
	if got := runProfile([]string{"remove", "ghost"}); got != 1 {
		t.Fatalf("runProfile(remove ghost) among existing profiles = %d, want 1", got)
	}
	dir, _ := config.Dir()
	secrets, err := config.LoadSecrets(dir, []byte("pp"))
	if err != nil || len(secrets) != 2 {
		t.Fatalf("secrets should be unchanged: %+v, err = %v", secrets, err)
	}
}

func TestRunProfilePassphraseNoCredentials(t *testing.T) {
	isolateConfigDir(t)
	if got := runProfile([]string{"passphrase"}); got != 1 {
		t.Fatalf("runProfile(passphrase) with no credentials.enc = %d, want 1", got)
	}
}

func TestRunProfileUnknownSubcommand(t *testing.T) {
	if got := runProfile([]string{"bogus"}); got != 2 {
		t.Fatalf("runProfile(bogus) = %d, want 2 (usage)", got)
	}
}

// TestRunProfileRejectsLeftoverArgs covers finding 1: unrecognized/leftover
// args after a profile subcommand must hit usage() (exit 2), not be
// silently dropped. All of these fail on arg-shape validation before ever
// touching credentials.enc, so none need isolateConfigDir.
func TestRunProfileRejectsLeftoverArgs(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"add with -config-dir leftover", []string{"add", "main", "-config-dir", "/x"}},
		{"add with typo'd -domian flag", []string{"add", "main", "-domian", "flatex.de"}},
		{"remove with extra arg", []string{"remove", "main", "extra"}},
		{"list with extra arg", []string{"list", "extra"}},
		{"passphrase with extra arg", []string{"passphrase", "extra"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runProfile(tc.args); got != 2 {
				t.Fatalf("runProfile(%v) = %d, want 2 (usage)", tc.args, got)
			}
		})
	}
}

func TestSecretsSurviveRekey(t *testing.T) {
	dir := t.TempDir()
	old := []byte("old-pass")
	profiles := []config.Profile{{Name: "main", Username: "alice", Domain: "flatex.at", Password: "pw1"}}
	if err := config.SaveSecrets(dir, old, profiles); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.LoadSecrets(dir, old)
	if err != nil {
		t.Fatal(err)
	}

	newPass := []byte("new-pass")
	if err := config.SaveSecrets(dir, newPass, loaded); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadSecrets(dir, old); err == nil {
		t.Fatal("old passphrase still works after rekey")
	}
	got, err := config.LoadSecrets(dir, newPass)
	if err != nil || len(got) != 1 || got[0] != profiles[0] {
		t.Fatalf("got %+v, err = %v", got, err)
	}
}
