package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/welworx/flatex-fetch/internal/config"
	"github.com/welworx/flatex-fetch/internal/portal"
)

// fakePortal is a portalClient standing in for a real portal session:
// ListDocumentsDetailed filters its fixed document set by the requested
// range (the portal's own behavior, and what -since-last's correctness
// depends on), and Download writes a stub PDF unless the row index is in
// failIdx.
type fakePortal struct {
	docs    []portal.Document
	failIdx map[int]error

	// downloads records the row indices Download was called with, in order,
	// so tests can assert both which documents were attempted and in what
	// order.
	downloads []int
}

func (f *fakePortal) Login(username, password string) error { return nil }

func (f *fakePortal) ListDocumentsDetailed(from, to time.Time) ([]portal.Document, error) {
	var out []portal.Document
	for _, d := range f.docs {
		if !d.Date.Before(from) && !d.Date.After(to) {
			out = append(out, d)
		}
	}
	return out, nil
}

func (f *fakePortal) Download(from, to time.Time, idx int, resolvePath portal.ResolvePath, seen map[string]bool, overwrite bool) (string, bool, error) {
	f.downloads = append(f.downloads, idx)
	if err := f.failIdx[idx]; err != nil {
		return "", false, err
	}
	dir, name := resolvePath(fmt.Sprintf("doc-%d.pdf", idx))
	dest := filepath.Join(dir, name)
	if _, err := os.Stat(dest); err == nil && !overwrite {
		return dest, true, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", false, err
	}
	if err := os.WriteFile(dest, []byte("%PDF-1.4 fake"), 0o600); err != nil {
		return "", false, err
	}
	seen[dest] = true
	return dest, false, nil
}

// installFakePortal makes fetchProfile use f instead of a real portal
// session for the duration of the test.
func installFakePortal(t *testing.T, f *fakePortal) {
	t.Helper()
	orig := newPortalClient
	newPortalClient = func(domain, userAgent string, log func(string, ...any)) (portalClient, error) {
		return f, nil
	}
	t.Cleanup(func() { newPortalClient = orig })
}

func testDoc(idx int, date string, name string) portal.Document {
	d, err := time.Parse("2006-01-02", date)
	if err != nil {
		panic(err)
	}
	return portal.Document{Index: idx, Name: name, Date: d, WindowFrom: d, WindowTo: d}
}

// TestFetchProfileDownloadsOldestFirst covers the happy path and the
// ordering invariant fetchProfile documents but never asserted: documents
// must be downloaded oldest-first so an interrupted run leaves -since-last's
// frontier gapless.
func TestFetchProfileDownloadsOldestFirst(t *testing.T) {
	f := &fakePortal{docs: []portal.Document{
		testDoc(2, "2026-03-01", "March"),
		testDoc(0, "2026-01-05", "January"),
		testDoc(1, "2026-02-10", "February"),
	}}
	installFakePortal(t, f)

	out := t.TempDir()
	p := config.Profile{Name: "main", Username: "alice", Domain: "flatex.at", Password: "pw"}
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)

	if err := fetchProfile(p, p.Password, out, "", "", from, to, false, false, false); err != nil {
		t.Fatalf("fetchProfile: %v", err)
	}

	want := []int{0, 1, 2}
	if len(f.downloads) != len(want) {
		t.Fatalf("downloads = %v, want %v", f.downloads, want)
	}
	for i, idx := range want {
		if f.downloads[i] != idx {
			t.Fatalf("downloads = %v, want oldest-first %v", f.downloads, want)
		}
	}

	entries, err := readDownloadLog(out)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, group := range entries {
		total += len(group)
	}
	if total != 3 {
		t.Fatalf("log has %d entries, want 3", total)
	}
	for _, name := range []string{"doc-0.pdf", "doc-1.pdf", "doc-2.pdf"} {
		if _, err := os.Stat(filepath.Join(out, "main", name)); err != nil {
			t.Fatalf("expected %s under %s/main: %v", name, out, err)
		}
	}
}

// TestFetchProfileSinceLastRetriesFailedDocument pins the -since-last
// frontier invariant. Run 1: an older document fails, a newer one succeeds.
// Run 2 with -since-last must still re-list and retry the failed one — if
// the newer document's log entry is allowed to advance lastDocumentDate past
// the failure, the older document is skipped forever and silently lost.
func TestFetchProfileSinceLastRetriesFailedDocument(t *testing.T) {
	failing := testDoc(0, "2026-01-05", "January")
	ok := testDoc(1, "2026-03-01", "March")

	f := &fakePortal{
		docs:    []portal.Document{failing, ok},
		failIdx: map[int]error{0: portal.ErrChallenged},
	}
	installFakePortal(t, f)

	out := t.TempDir()
	p := config.Profile{Name: "main", Username: "alice", Domain: "flatex.at", Password: "pw"}
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)

	// Run 1: row 0 fails, row 1 succeeds. fetchProfile reports the failure.
	if err := fetchProfile(p, p.Password, out, "", "", from, to, true, false, false); err == nil {
		t.Fatal("run 1: expected an error for the failed document")
	}
	if len(f.downloads) != 2 {
		t.Fatalf("run 1: downloads = %v, want both rows attempted", f.downloads)
	}

	// The frontier must not have moved past the failed document.
	entries, err := readDownloadLog(out)
	if err != nil {
		t.Fatal(err)
	}
	if last, found := lastDocumentDate(entries, "main"); found && last.After(failing.Date) {
		t.Fatalf("frontier advanced to %s, past the failed document dated %s — the next -since-last run would skip it forever",
			last.Format("2006-01-02"), failing.Date.Format("2006-01-02"))
	}

	// Run 2 with -since-last: the failed document must be listed and retried.
	f.downloads = nil
	f.failIdx = nil // the transient failure has cleared
	if err := fetchProfile(p, p.Password, out, "", "", from, to, true, false, false); err != nil {
		t.Fatalf("run 2: %v", err)
	}
	retried := false
	for _, idx := range f.downloads {
		if idx == 0 {
			retried = true
		}
	}
	if !retried {
		t.Fatalf("run 2: downloads = %v, want the previously failed row 0 retried", f.downloads)
	}
	if _, err := os.Stat(filepath.Join(out, "main", "doc-0.pdf")); err != nil {
		t.Fatalf("run 2: previously failed document was never written: %v", err)
	}
}

// TestFetchProfileSkippedDocumentNotLoggedAfterEarlierFailure pins the other
// half of the -since-last frontier fix: once an earlier document's failure
// in this run has turned logging off, a later document that Download
// reports as already-on-disk (wasSkipped) must not be backfilled into the
// log either. If it were, lastDocumentDate would see it and move the
// frontier past the still-unlogged failure — exactly the gap the "logging"
// guard exists to prevent.
func TestFetchProfileSkippedDocumentNotLoggedAfterEarlierFailure(t *testing.T) {
	failing := testDoc(0, "2026-01-05", "January")
	skippedNewer := testDoc(1, "2026-03-01", "March")

	f := &fakePortal{
		docs:    []portal.Document{failing, skippedNewer},
		failIdx: map[int]error{0: portal.ErrChallenged},
	}
	installFakePortal(t, f)

	out := t.TempDir()
	p := config.Profile{Name: "main", Username: "alice", Domain: "flatex.at", Password: "pw"}
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)

	// Pre-create the newer document's file on disk so fakePortal.Download
	// reports it as wasSkipped=true (already present) instead of downloading
	// it fresh.
	dir := filepath.Join(out, "main")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "doc-1.pdf"), []byte("%PDF-1.4 fake"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Documents are processed oldest-first, so the failure (January) is
	// handled before the already-on-disk document (March).
	if err := fetchProfile(p, p.Password, out, "", "", from, to, false, false, false); err == nil {
		t.Fatal("expected an error for the failed document")
	}

	entries, err := readDownloadLog(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, group := range entries {
		for _, e := range group {
			if e.Name == skippedNewer.Name {
				t.Fatalf("log contains an entry for the skipped newer document even though an earlier document in the same run failed: %+v", e)
			}
		}
	}
}
