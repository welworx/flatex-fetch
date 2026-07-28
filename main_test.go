package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/welworx/flatex-fetch/internal/config"
)

// isolateConfigDir points config.Dir() at a fresh temp directory for the
// duration of the test. HOME alone isn't enough: os.UserConfigDir() prefers
// $XDG_CONFIG_HOME on Linux, and CI runners commonly have it set, which
// would otherwise make every "isolated" test share one real config dir.
func isolateConfigDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
}

// withStdin temporarily replaces os.Stdin with content, for tests that
// exercise promptLine (which reads one line from stdin). Restored via
// t.Cleanup. Not usable for promptSecret, which requires a real TTY.
func withStdin(t *testing.T, content string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString(content); err != nil {
		t.Fatal(err)
	}
	w.Close()
	orig := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = orig
		r.Close()
	})
}

func TestRunVersion(t *testing.T) {
	if got := run([]string{"-version"}); got != 0 {
		t.Fatalf("run(-version) = %d, want 0", got)
	}
}

func TestRunNoArgs(t *testing.T) {
	if got := run(nil); got != 2 {
		t.Fatalf("run() = %d, want 2 (usage error)", got)
	}
}

func TestRunHelp(t *testing.T) {
	for _, arg := range []string{"-help", "--help", "help"} {
		if got := run([]string{arg}); got != 0 {
			t.Fatalf("run(%q) = %d, want 0", arg, got)
		}
	}
}

func TestRunConfigDirFlag(t *testing.T) {
	isolateConfigDir(t)
	t.Setenv("FLATEX_FETCH_CONFIG_DIR", "")
	custom := t.TempDir()
	t.Setenv("FLATEX_FETCH_PASSPHRASE", "pp")
	t.Setenv("FLATEX_FETCH_USERNAME", "alice")
	t.Setenv("FLATEX_FETCH_PASSWORD", "pw1")

	if got := run([]string{"-config-dir", custom, "profile", "add", "main"}); got != 0 {
		t.Fatalf("run(-config-dir) = %d, want 0", got)
	}
	if _, err := os.Stat(filepath.Join(custom, "credentials.enc")); err != nil {
		t.Fatalf("credentials.enc not written under -config-dir: %v", err)
	}

	dir, err := config.Dir()
	if err != nil || dir != custom {
		t.Fatalf("config.Dir() = %q, %v; want %q", dir, err, custom)
	}
}

func TestRunConfigDirFlagMissingValue(t *testing.T) {
	if got := run([]string{"-config-dir"}); got != 2 {
		t.Fatalf("run(-config-dir with no value) = %d, want 2", got)
	}
}
