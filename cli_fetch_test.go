package main

import (
	"testing"
	"time"
)

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
