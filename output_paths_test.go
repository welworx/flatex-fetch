package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/welworx/flatex-fetch/internal/config"
	"github.com/welworx/flatex-fetch/internal/portal"
)

func TestOutputPathTraversalRejectedBeforeLogin(t *testing.T) {
	original := newPortalClient
	newPortalClient = func(string, string, func(string, ...any)) (portalClient, error) {
		t.Fatal("unsafe output settings reached portal creation")
		return nil, nil
	}
	t.Cleanup(func() { newPortalClient = original })
	for _, name := range []string{"", ".", "..", "../outside", `..\outside`, "/tmp/outside", `C:\outside`, " .. "} {
		t.Run("profile="+name, func(t *testing.T) {
			if err := profileAdd(t.TempDir(), name, "flatex.at", "user", "password"); err == nil {
				t.Fatal("unsafe profile was accepted for storage")
			}
			// Also protect previously stored profiles that predate name validation.
			if err := fetchProfile(config.Profile{Name: name}, "password", t.TempDir(), "", "", time.Time{}, time.Time{}, false, false, false); err == nil {
				t.Fatal("unsafe stored profile was accepted for fetch")
			}
		})
	}
	for _, format := range []string{"../<filename>.pdf", "a/../<filename>.pdf", "/tmp/<filename>.pdf", `C:\<filename>.pdf`, `..\<filename>.pdf`, "<date ../YYYY>/<filename>.pdf", "<date /YYYY>/<filename>.pdf"} {
		t.Run("format="+format, func(t *testing.T) {
			if err := fetchProfile(config.Profile{Name: "main"}, "password", t.TempDir(), format, "", time.Time{}, time.Time{}, false, false, false); err == nil {
				t.Fatal("unsafe template was accepted")
			}
		})
	}
}

func TestOutputPathsStayUnderRoot(t *testing.T) {
	root := t.TempDir()
	for _, profile := range []string{"..", ".", "../outside", "Example Company GmbH", "personal.2025"} {
		for _, format := range []string{"", "<profile>/<date YYYY>/<filename>.pdf", "<profile>/<filename>/statement.pdf"} {
			for _, filename := range []string{"invoice.pdf", "..", ".", "../escape.pdf"} {
				dir, name := documentPathResolver(root, format, profile, portal.Document{Date: time.Now()})(filename)
				relative, err := filepath.Rel(root, filepath.Join(dir, name))
				if err != nil || !filepath.IsLocal(relative) {
					t.Fatalf("profile=%q format=%q filename=%q escapes root: %q (%v)", profile, format, filename, relative, err)
				}
			}
		}
	}
	for _, name := range []string{"Example Company GmbH", "personal.2025", "_main", "Müller"} {
		if err := validateProfileName(name); err != nil {
			t.Errorf("normal name %q rejected: %v", name, err)
		}
	}
	dir, name := documentPathResolver(root, "", "Example Company GmbH", portal.Document{})("invoice.pdf")
	if dir != filepath.Join(root, "Example Company GmbH") || name != "invoice.pdf" {
		t.Fatalf("normal path changed: %s/%s", dir, name)
	}
}
