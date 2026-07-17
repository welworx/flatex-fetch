//go:build e2e

package portal

import (
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
