package main

import "testing"

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
