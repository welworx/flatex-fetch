package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/welworx/flatex-fetch/internal/portal"
)

func TestLogDownloadAppends(t *testing.T) {
	dir := t.TempDir()
	d := portal.Document{Index: 3, Category: "Kontoauszug", Name: "Kontoauszug vom 10.07.2026", Date: time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)}

	if err := logDownload(dir, "main", filepath.Join(dir, "main", "doc1.pdf"), d); err != nil {
		t.Fatal(err)
	}
	if err := logDownload(dir, "main", filepath.Join(dir, "main", "doc2.pdf"), d); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(filepath.Join(dir, ".fetch-log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var lines []downloadLogEntry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e downloadLogEntry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("bad log line %q: %v", sc.Text(), err)
		}
		lines = append(lines, e)
	}
	if len(lines) != 2 {
		t.Fatalf("got %d log lines, want 2", len(lines))
	}
	if lines[0].Profile != "main" || lines[0].Index != 3 || lines[0].Category != "Kontoauszug" ||
		lines[0].Date != "2026-07-10" || lines[0].Path != filepath.Join(dir, "main", "doc1.pdf") {
		t.Fatalf("entry = %+v", lines[0])
	}
	if _, err := time.Parse(time.RFC3339, lines[0].Time); err != nil {
		t.Errorf("Time not RFC3339: %v", err)
	}
}

func TestAlreadyLoggedUnambiguousMatchOnDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.pdf")
	if err := os.WriteFile(path, []byte("pdf"), 0600); err != nil {
		t.Fatal(err)
	}
	d := portal.Document{Category: "Kontoauszug", Name: "Kontoauszug vom 10.07.2026", Date: time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)}
	if err := logDownload(dir, "main", path, d); err != nil {
		t.Fatal(err)
	}

	entries, err := readDownloadLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := alreadyLogged(entries, "main", d)
	if !ok || got != path {
		t.Fatalf("alreadyLogged = (%q, %v), want (%q, true)", got, ok, path)
	}
}

func TestAlreadyLoggedFileMissing(t *testing.T) {
	dir := t.TempDir()
	// never actually written to disk, unlike TestLogDownloadAppends
	path := filepath.Join(dir, "gone.pdf")
	d := portal.Document{Category: "Kontoauszug", Name: "Kontoauszug vom 10.07.2026", Date: time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)}
	if err := logDownload(dir, "main", path, d); err != nil {
		t.Fatal(err)
	}

	entries, err := readDownloadLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := alreadyLogged(entries, "main", d); ok {
		t.Fatal("alreadyLogged = true for a file that no longer exists on disk")
	}
}

func TestAlreadyLoggedAmbiguousMatch(t *testing.T) {
	dir := t.TempDir()
	d := portal.Document{Category: "Kauf Fonds/Zertifikate", Name: "Kauf", Date: time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)}
	for _, name := range []string{"a.pdf", "b.pdf"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("pdf"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := logDownload(dir, "main", path, d); err != nil {
			t.Fatal(err)
		}
	}

	entries, err := readDownloadLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	// two distinct documents logged under the identical date/category/name
	// key -> ambiguous, must not be treated as "already have it"
	if _, ok := alreadyLogged(entries, "main", d); ok {
		t.Fatal("alreadyLogged = true for an ambiguous (2-entry) key")
	}
}

func TestLastDocumentDate(t *testing.T) {
	dir := t.TempDir()
	// Backfill order: an old document (2021) gets fetched AFTER (later
	// "time") a newer one (2026) already on disk. lastDocumentDate must go
	// by the document's own Date, not fetch order/Time, or -since-last
	// would regress to the old document's date on the next run.
	newDoc := portal.Document{Category: "Kontoauszug", Name: "new", Date: time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC)}
	if err := logDownload(dir, "main", filepath.Join(dir, "1.pdf"), newDoc); err != nil {
		t.Fatal(err)
	}
	oldDoc := portal.Document{Category: "Kontoauszug", Name: "old", Date: time.Date(2021, 7, 10, 0, 0, 0, 0, time.UTC)}
	if err := logDownload(dir, "main", filepath.Join(dir, "2.pdf"), oldDoc); err != nil {
		t.Fatal(err)
	}
	newerOtherProfile := portal.Document{Category: "Kontoauszug", Name: "newer", Date: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)}
	if err := logDownload(dir, "other", filepath.Join(dir, "3.pdf"), newerOtherProfile); err != nil {
		t.Fatal(err)
	}

	entries, err := readDownloadLog(dir)
	if err != nil {
		t.Fatal(err)
	}

	got, ok := lastDocumentDate(entries, "main")
	if !ok {
		t.Fatal("lastDocumentDate ok = false, want true")
	}
	if want := newDoc.Date; !got.Equal(want) {
		t.Errorf("lastDocumentDate = %v, want %v (newest document date, not fetch order)", got, want)
	}

	if _, ok := lastDocumentDate(entries, "nonexistent"); ok {
		t.Error("lastDocumentDate ok = true for a profile with no entries")
	}
}

func TestReadDownloadLogMissingFile(t *testing.T) {
	entries, err := readDownloadLog(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %v, want empty for a directory with no log yet", entries)
	}
}
