package main

import (
	"path/filepath"
	"testing"
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
