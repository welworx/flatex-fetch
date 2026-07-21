package main

import (
	"testing"
	"time"

	"github.com/welworx/flatex-fetch/internal/config"
	"github.com/welworx/flatex-fetch/internal/portal"
)

func TestDescribeDocument(t *testing.T) {
	d := portal.Document{
		Index:    7,
		Name:     "Kontoauszug vom 10.07.2026",
		Category: "Kontoauszug",
		Date:     time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC),
	}
	got := describeDocument(d)
	want := `row 7 (2026-07-10, Kontoauszug, "Kontoauszug vom 10.07.2026")`
	if got != want {
		t.Fatalf("describeDocument = %q, want %q", got, want)
	}
}

func TestResolveProfilesAndCredsDefaultsToFirstProfile(t *testing.T) {
	isolateConfigDir(t)
	t.Setenv("FLATEX_FETCH_PASSPHRASE", "pp")

	cfgDir, err := config.Dir()
	if err != nil {
		t.Fatal(err)
	}
	if err := profileAdd(cfgDir, "a", "flatex.at", "alice", "pw-a"); err != nil {
		t.Fatal(err)
	}
	if err := profileAdd(cfgDir, "b", "flatex.at", "bob", "pw-b"); err != nil {
		t.Fatal(err)
	}

	profiles, _, err := resolveProfilesAndCreds("", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 1 || profiles[0].Name != "a" {
		t.Fatalf("profiles = %+v, want first profile \"a\"", profiles)
	}
}

func TestResolveProfilesAndCredsEnvBypassesProfiles(t *testing.T) {
	isolateConfigDir(t)
	t.Setenv("FLATEX_FETCH_USERNAME", "alice")
	t.Setenv("FLATEX_FETCH_PASSWORD", "pw")

	// -profile is ignored; no profiles.json/passphrase needed at all.
	profiles, creds, err := resolveProfilesAndCreds("someprofile", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 1 || profiles[0].Name != "from-env" || profiles[0].Username != "alice" || profiles[0].Domain != "flatex.at" {
		t.Fatalf("profiles = %+v, want single from-env/alice/flatex.at", profiles)
	}
	if creds["from-env"] != "pw" {
		t.Fatalf("creds = %+v, want from-env -> pw", creds)
	}

	t.Setenv("FLATEX_FETCH_DOMAIN", "flatex.de")
	profiles, _, err = resolveProfilesAndCreds("", false)
	if err != nil {
		t.Fatal(err)
	}
	if profiles[0].Domain != "flatex.de" {
		t.Fatalf("domain = %q, want flatex.de override", profiles[0].Domain)
	}
}

func TestRunFetchSinceLastRejectsExplicitRange(t *testing.T) {
	for _, args := range [][]string{
		{"-since-last", "-days", "30"},
		{"-since-last", "-from", "2026-01-01", "-to", "2026-06-30"},
	} {
		if got := runFetch(args); got != 2 {
			t.Fatalf("runFetch(%v) = %d, want 2", args, got)
		}
	}
}

func TestRunFetchRejectsUnexpectedArgument(t *testing.T) {
	// A stray positional argument (e.g. a typo'd repeated "fetch") makes Go's
	// flag package stop parsing right there — anything after it, including
	// real flags like -out/-format, would otherwise be silently ignored
	// rather than applied or rejected.
	if got := runFetch([]string{"-profile", "main", "fetch", "-out", "./downloads"}); got != 2 {
		t.Fatalf("runFetch(...) = %d, want 2", got)
	}
}

func TestDateRange(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)

	// default: last N days
	from, to, err := dateRange(10, "", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if from.Format("2006-01-02") != "2026-07-06" || to.Format("2006-01-02") != "2026-07-16" {
		t.Fatalf("days: got %s..%s", from, to)
	}

	// explicit range
	from, to, err = dateRange(90, "2026-01-01", "2026-06-30", now)
	if err != nil {
		t.Fatal(err)
	}
	if from.Format("2006-01-02") != "2026-01-01" || to.Format("2006-01-02") != "2026-06-30" {
		t.Fatalf("explicit: got %s..%s", from, to)
	}

	// -from without -to (and vice versa) is an error
	if _, _, err := dateRange(90, "2026-01-01", "", now); err == nil {
		t.Fatal("from without to accepted")
	}
	// from after to is an error
	if _, _, err := dateRange(90, "2026-06-30", "2026-01-01", now); err == nil {
		t.Fatal("inverted range accepted")
	}
	// bad date format is an error
	if _, _, err := dateRange(90, "01.01.2026", "2026-06-30", now); err == nil {
		t.Fatal("bad date format accepted")
	}
}
