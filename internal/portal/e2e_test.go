//go:build e2e

package portal

import (
	"net/url"
	"os"
	"testing"
	"time"
)

// Requires real credentials:
//
//	FLATEX_E2E_USER, FLATEX_E2E_PASS [, FLATEX_E2E_DOMAIN (default flatex.at)]
//
// Run: go test -tags e2e -run TestE2ELogin -v ./internal/portal/
func TestE2ELogin(t *testing.T) {
	user, pass := os.Getenv("FLATEX_E2E_USER"), os.Getenv("FLATEX_E2E_PASS")
	if user == "" || pass == "" {
		t.Skip("FLATEX_E2E_USER/FLATEX_E2E_PASS not set")
	}
	domain := os.Getenv("FLATEX_E2E_DOMAIN")
	if domain == "" {
		domain = "flatex.at"
	}

	c, err := New(domain, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Login(user, pass); err != nil {
		t.Fatalf("live login failed: %v", err)
	}
	t.Logf("login OK: tokenId/windowId acquired (token %d chars, window %d chars)",
		len(c.tokenID), len(c.windowID))
}

// fieldRetrieveMore is the archive's "load more" button field, per a live
// HAR capture of the browser's own scroll-triggered load-more: that
// request sends *only* this field. Dead end for this package's purposes —
// see TestE2EPagination — so it lives here rather than markup.go: nothing
// outside this research spike references it.
const fieldRetrieveMore = "btnRetrieveMore.clicked"

// TestE2EPagination is a research spike, NOT wired into
// ListDocuments/ListDocumentsDetailed. Two live attempts, both failed:
//
//  1. (2026-07-20) fieldRetrieveMore merged into the full archiveFilterForm:
//     returned 0 rows and appeared to disrupt the session for subsequent
//     requests too.
//  2. (2026-07-21) fieldRetrieveMore sent bare (no filter fields), per a
//     HAR capture of the browser's own scroll-triggered load-more: still
//     returned 0 rows. The HAR session never applied a custom date filter
//     before scrolling — it opened the default view and scrolled — so it
//     only proved the bare shape works there.
//
// Root cause found afterward (not via this test): the portal's own UI
// shows "Es werden nur die ersten 100 Dokumente dargestellt." for any
// custom date-range filter, with no load-more control available in that
// mode at all — scrolling for more only ever worked on the unfiltered
// default view. fetch/list always filter by date, so this mechanism
// isn't usable for them; the actual fix (ListDocumentsDetailed splitting
// a wide range into sub-windows) doesn't use fieldRetrieveMore at all.
// Kept as a record of what was tried.
//
// Run: go test -tags e2e -run TestE2EPagination -v ./internal/portal/
func TestE2EPagination(t *testing.T) {
	user, pass := os.Getenv("FLATEX_E2E_USER"), os.Getenv("FLATEX_E2E_PASS")
	if user == "" || pass == "" {
		t.Skip("FLATEX_E2E_USER/FLATEX_E2E_PASS not set")
	}
	domain := os.Getenv("FLATEX_E2E_DOMAIN")
	if domain == "" {
		domain = "flatex.at"
	}

	c, err := New(domain, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Login(user, pass); err != nil {
		t.Fatalf("live login failed: %v", err)
	}

	from := time.Now().AddDate(-5, 0, 0) // wide enough to likely exceed one page
	to := time.Now()
	page1, err := c.filterArchive(from, to)
	if err != nil {
		t.Fatalf("live listing failed: %v", err)
	}
	rows1 := rowIndices(page1)
	t.Logf("page 1: %d rows", len(rows1))
	if len(rows1) == 0 {
		t.Skip("no documents in the last 5 years; cannot spike pagination")
	}

	page2, err := c.postForm(c.archiveListPath, url.Values{fieldRetrieveMore: {"true"}})
	if err != nil {
		t.Fatalf("load-more request failed: %v", err)
	}
	rows2 := rowIndices(page2)
	t.Logf("after load-more: %d rows", len(rows2))
	if len(rows2) <= len(rows1) {
		t.Logf("SPIKE RESULT: load-more did not add rows (%d -> %d) — either already all rows, or the request shape needs correction (see fieldRetrieveMore in markup.go)", len(rows1), len(rows2))
	} else {
		t.Logf("SPIKE RESULT: load-more works, %d -> %d rows", len(rows1), len(rows2))
	}
}

// Run: go test -tags e2e -run TestE2EListDownload -v ./internal/portal/
func TestE2EListDownload(t *testing.T) {
	user, pass := os.Getenv("FLATEX_E2E_USER"), os.Getenv("FLATEX_E2E_PASS")
	if user == "" || pass == "" {
		t.Skip("FLATEX_E2E_USER/FLATEX_E2E_PASS not set")
	}
	domain := os.Getenv("FLATEX_E2E_DOMAIN")
	if domain == "" {
		domain = "flatex.at"
	}

	c, err := New(domain, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Login(user, pass); err != nil {
		t.Fatalf("live login failed: %v", err)
	}

	from := time.Now().AddDate(0, 0, -90)
	to := time.Now()
	rows, err := c.ListDocuments(from, to)
	if err != nil {
		t.Fatalf("live listing failed: %v", err)
	}
	t.Logf("listed %d document rows", len(rows))
	if len(rows) == 0 {
		t.Skip("no documents in the last 90 days; cannot spike download")
	}

	dir := t.TempDir()
	path, skipped, err := c.Download(from, to, rows[0], flatResolvePath(dir), map[string]bool{}, false)
	if err != nil {
		t.Fatalf("live download failed: %v", err)
	}
	t.Logf("download OK: %s (skipped=%v)", path, skipped)
}

// TestE2EWindowedListingAndDownload exercises the actual fix for the
// "only first page" gap: ListDocumentsDetailed adaptively bisecting a wide
// range whenever a query comes back capped or empty (see windowedDocuments
// and tableMarker in portal.go), and fetchProfile's Download call using
// each Document's own WindowFrom/WindowTo rather than the outer requested
// range. Neither TestE2EListDownload nor
// TestE2EListDocumentsDetailed exercises this — they both predate it and
// use ListDocuments (unwindowed) or a narrow range that never splits.
// This is the one live gap called out when the fix shipped (PR #8):
// window-splitting and cap-bisection were only unit-tested against a mock
// server.
//
// Run: go test -tags e2e -run TestE2EWindowedListingAndDownload -v ./internal/portal/
func TestE2EWindowedListingAndDownload(t *testing.T) {
	user, pass := os.Getenv("FLATEX_E2E_USER"), os.Getenv("FLATEX_E2E_PASS")
	if user == "" || pass == "" {
		t.Skip("FLATEX_E2E_USER/FLATEX_E2E_PASS not set")
	}
	domain := os.Getenv("FLATEX_E2E_DOMAIN")
	if domain == "" {
		domain = "flatex.at"
	}

	c, err := New(domain, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Login(user, pass); err != nil {
		t.Fatalf("live login failed: %v", err)
	}

	// Wide enough that a single query is expected to fail and force at
	// least one split.
	from := time.Now().AddDate(-1, 0, 0)
	to := time.Now()
	docs, err := c.ListDocumentsDetailed(from, to)
	if err != nil {
		t.Fatalf("live windowed listing failed: %v", err)
	}
	t.Logf("listed %d documents across the last year", len(docs))
	if len(docs) == 0 {
		t.Skip("no documents in the last year; cannot verify windowing/download")
	}

	windows := map[string]bool{}
	for _, d := range docs {
		windows[d.WindowFrom.Format("2006-01-02")+".."+d.WindowTo.Format("2006-01-02")] = true
		if d.WindowFrom.IsZero() || d.WindowTo.IsZero() {
			t.Errorf("document %+v has an unset window", d)
		}
	}
	t.Logf("documents came from %d distinct sub-window(s)", len(windows))
	if len(windows) < 2 {
		t.Logf("only 1 window seen for a 1-year range — either a light account, or splitting needs a look")
	}

	// The actual correctness fix: Download must use the document's own
	// window, not the outer [from, to] — confirm it works end-to-end.
	d := docs[0]
	dir := t.TempDir()
	path, skipped, err := c.Download(d.WindowFrom, d.WindowTo, d.Index, flatResolvePath(dir), map[string]bool{}, false)
	if err != nil {
		t.Fatalf("live download using document's own window failed: %v", err)
	}
	t.Logf("download OK using WindowFrom/WindowTo: %s (skipped=%v)", path, skipped)
}

// Run: go test -tags e2e -run TestE2EListDocumentsDetailed -v ./internal/portal/
func TestE2EListDocumentsDetailed(t *testing.T) {
	user, pass := os.Getenv("FLATEX_E2E_USER"), os.Getenv("FLATEX_E2E_PASS")
	if user == "" || pass == "" {
		t.Skip("FLATEX_E2E_USER/FLATEX_E2E_PASS not set")
	}
	domain := os.Getenv("FLATEX_E2E_DOMAIN")
	if domain == "" {
		domain = "flatex.at"
	}

	c, err := New(domain, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Login(user, pass); err != nil {
		t.Fatalf("live login failed: %v", err)
	}

	from := time.Now().AddDate(0, 0, -90)
	to := time.Now()
	docs, err := c.ListDocumentsDetailed(from, to)
	if err != nil {
		t.Fatalf("live listing failed: %v", err)
	}
	t.Logf("listed %d documents", len(docs))
	if len(docs) == 0 {
		t.Skip("no documents in the last 90 days; cannot verify metadata parsing")
	}
	for _, d := range docs {
		if d.Name == "" || d.Category == "" || d.Date.IsZero() {
			t.Errorf("document at index %d missing metadata: %+v", d.Index, d)
		}
	}
}
