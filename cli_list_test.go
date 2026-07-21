package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/welworx/flatex-fetch/internal/portal"
)

func TestProfileFlagsValid(t *testing.T) {
	cases := []struct {
		name string
		all  bool
		want bool
	}{
		{"", false, true},
		{"", true, true},
		{"x", false, true},
		{"x", true, false},
	}
	for _, c := range cases {
		if got := profileFlagsValid(c.name, c.all); got != c.want {
			t.Errorf("profileFlagsValid(%q, %v) = %v, want %v", c.name, c.all, got, c.want)
		}
	}
}

func testDocs() []portal.Document {
	return []portal.Document{
		{Index: 0, Name: "Einladung Hauptversammlung", Date: time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC), Category: "Hauptversammlung", Read: true},
		{Index: 1, Name: "Kontoauszug vom 10.07.2026", Date: time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC), Category: "Kontoauszug", Read: false},
	}
}

func captureStdout(t *testing.T, f func(w *os.File)) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	f(w)
	w.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestWriteDocumentsTable(t *testing.T) {
	out := captureStdout(t, func(w *os.File) { writeDocumentsTable(w, testDocs(), "main") })
	if !strings.Contains(out, "Einladung Hauptversammlung") || !strings.Contains(out, "Kontoauszug vom 10.07.2026") {
		t.Fatalf("table missing document names: %s", out)
	}
	if !strings.Contains(out, "2026-07-16") || !strings.Contains(out, "true") || !strings.Contains(out, "false") {
		t.Fatalf("table missing date/read columns: %s", out)
	}
	if !strings.Contains(out, "main") {
		t.Fatalf("table missing profile column: %s", out)
	}
}

func TestWriteDocumentsCSV(t *testing.T) {
	out := captureStdout(t, func(w *os.File) {
		if err := writeDocumentsCSV(w, testDocs(), "main"); err != nil {
			t.Fatal(err)
		}
	})
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("csv lines = %d, want 3 (header + 2 rows): %s", len(lines), out)
	}
	if lines[0] != "profile,index,date,category,read,name" {
		t.Fatalf("csv header = %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "main,") || !strings.HasPrefix(lines[2], "main,") {
		t.Fatalf("csv rows missing profile: %v", lines[1:])
	}
	if !strings.Contains(lines[1], "Hauptversammlung") || !strings.Contains(lines[2], "Kontoauszug") {
		t.Fatalf("csv rows = %v", lines[1:])
	}
}

func TestWriteDocumentsJSON(t *testing.T) {
	out := captureStdout(t, func(w *os.File) {
		if err := writeDocumentsJSON(w, testDocs(), "main"); err != nil {
			t.Fatal(err)
		}
	})
	var rows []documentRow
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if len(rows) != 2 || rows[0].Index != 0 || rows[0].Date != "2026-07-16" || !rows[0].Read {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].Profile != "main" || rows[1].Profile != "main" {
		t.Fatalf("rows missing profile: %+v", rows)
	}
	if rows[1].Index != 1 || rows[1].Read {
		t.Fatalf("rows[1] = %+v", rows[1])
	}
}

func TestRunListCSVAndJSONMutuallyExclusive(t *testing.T) {
	if got := runList([]string{"-all-profiles", "-csv", "-json"}); got != 2 {
		t.Fatalf("runList(-csv -json) = %d, want 2", got)
	}
}

func TestRunListProfileAndAllProfilesMutuallyExclusive(t *testing.T) {
	if got := runList([]string{"-profile", "x", "-all-profiles"}); got != 2 {
		t.Fatalf("runList(-profile x -all-profiles) = %d, want 2", got)
	}
}

func TestRunListRejectsUnexpectedArgument(t *testing.T) {
	if got := runList([]string{"-profile", "main", "bogus"}); got != 2 {
		t.Fatalf("runList(...) = %d, want 2", got)
	}
}

func TestRunListDefaultsToFirstProfileWhenNoneConfigured(t *testing.T) {
	isolateConfigDir(t)
	if got := runList(nil); got != 1 {
		t.Fatalf("runList() = %d, want 1 (no profiles configured)", got)
	}
}
