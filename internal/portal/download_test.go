package portal

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var testWindow = struct{ from, to time.Time }{
	from: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
	to:   time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC),
}

// flatResolvePath reproduces the pre-templating behavior: every document
// lands directly in dir under its resolved name.
func flatResolvePath(dir string) ResolvePath {
	return func(name string) (string, string) { return dir, name }
}

// downloadServer serves the two-step archive-download flow: the archive
// endpoint returns a "download" command pointing at a location, and that
// location serves the actual file content (pdf or zip, chosen by loc).
func downloadServer(t *testing.T, content map[string][]byte, contentType map[string]string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /banking-flatex.at/"+headerAreaAction, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"commands":[{"command":"replacePortions"}]}`)
	})
	mux.HandleFunc("POST /banking-flatex.at/"+archiveListAction, func(w http.ResponseWriter, r *http.Request) {
		if r.FormValue(fieldDownloadClicked) != "true" {
			http.Error(w, "no download requested", http.StatusBadRequest)
			return
		}
		idx := "0"
		for k, v := range r.Form {
			if v[0] == "on" {
				idx = k[len(rowSelectionPrefix) : len(k)-len("].checked")]
			}
		}
		loc := "/banking-flatex.at/downloadData/1/doc-" + idx + ".bin"
		fmt.Fprintf(w, `{"commands":[{"command":"download","location":%q}]}`, loc)
	})
	mux.HandleFunc("/banking-flatex.at/downloadData/", func(w http.ResponseWriter, r *http.Request) {
		body, ok := content[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if ct := contentType[r.URL.Path]; ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		w.Write(body)
	})
	mux.HandleFunc("/challenge", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `<html>myracloud verification</html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestDownloadSinglePDF(t *testing.T) {
	srv := downloadServer(t,
		map[string][]byte{"/banking-flatex.at/downloadData/1/doc-0.bin": []byte("%PDF-1.4 fake content")},
		nil,
	)
	c := newTestClient(t, srv)
	dir := t.TempDir()

	p, skipped, err := c.Download(testWindow.from, testWindow.to, 0, flatResolvePath(dir), map[string]bool{}, false)
	if err != nil || skipped {
		t.Fatalf("err=%v skipped=%v", err, skipped)
	}
	if filepath.Dir(p) != dir {
		t.Fatalf("path escaped destDir: %q", p)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "%PDF-1.4 fake content" {
		t.Fatalf("content = %q", got)
	}
}

func TestDownloadZipBundle(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, err := zw.Create("Abrechnung_123.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("%PDF-1.4 zipped content")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	srv := downloadServer(t,
		map[string][]byte{"/banking-flatex.at/downloadData/1/doc-0.bin": buf.Bytes()},
		map[string]string{"/banking-flatex.at/downloadData/1/doc-0.bin": "application/zip"},
	)
	c := newTestClient(t, srv)
	dir := t.TempDir()

	p, skipped, err := c.Download(testWindow.from, testWindow.to, 0, flatResolvePath(dir), map[string]bool{}, false)
	if err != nil || skipped {
		t.Fatalf("err=%v skipped=%v", err, skipped)
	}
	if filepath.Base(p) != "Abrechnung_123.pdf" {
		t.Fatalf("got %q, want Abrechnung_123.pdf", filepath.Base(p))
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "%PDF-1.4 zipped content" {
		t.Fatalf("content = %q", got)
	}
}

func TestDownloadZipWithMultipleEntriesErrors(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range []string{"a.pdf", "b.pdf"} {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte("%PDF-1.4")); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	srv := downloadServer(t,
		map[string][]byte{"/banking-flatex.at/downloadData/1/doc-0.bin": buf.Bytes()},
		map[string]string{"/banking-flatex.at/downloadData/1/doc-0.bin": "application/zip"},
	)
	c := newTestClient(t, srv)
	dir := t.TempDir()

	_, _, err := c.Download(testWindow.from, testWindow.to, 0, flatResolvePath(dir), map[string]bool{}, false)
	if err == nil {
		t.Fatal("expected error for unexpected multi-entry zip")
	}
}

func TestDownloadDedupAndCollision(t *testing.T) {
	srv := downloadServer(t,
		map[string][]byte{
			"/banking-flatex.at/downloadData/1/doc-0.bin": []byte("%PDF-1.4 row0"),
			"/banking-flatex.at/downloadData/1/doc-1.bin": []byte("%PDF-1.4 row1"),
		},
		nil,
	)
	c := newTestClient(t, srv)
	dir := t.TempDir()

	// First run: downloads row 0 (resolves to a hash-stem name, since no
	// Content-Disposition/query filename/pdf-suffixed path is present).
	seen := map[string]bool{}
	p1, skipped, err := c.Download(testWindow.from, testWindow.to, 0, flatResolvePath(dir), seen, false)
	if err != nil || skipped {
		t.Fatalf("first: err=%v skipped=%v", err, skipped)
	}

	// New run (fresh seen): existing file → dedup skip.
	if _, skipped, _ := c.Download(testWindow.from, testWindow.to, 0, flatResolvePath(dir), map[string]bool{}, false); !skipped {
		t.Fatal("re-run should skip existing file")
	}
	// New run with overwrite: downloads again to the same path.
	p2, skipped, err := c.Download(testWindow.from, testWindow.to, 0, flatResolvePath(dir), map[string]bool{}, true)
	if err != nil || skipped {
		t.Fatalf("overwrite: err=%v skipped=%v", err, skipped)
	}
	if p1 != p2 {
		t.Fatalf("overwrite path = %q, want %q", p2, p1)
	}
}

func TestDownloadDetectsChallenge(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/banking-flatex.at/"+headerAreaAction, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"commands":[{"command":"replacePortions"}]}`)
	})
	mux.HandleFunc("/banking-flatex.at/"+archiveListAction, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"commands":[{"command":"download","location":"/challenge"}]}`)
	})
	mux.HandleFunc("/challenge", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `<html>myracloud verification</html>`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, srv)
	dir := t.TempDir()
	_, _, err := c.Download(testWindow.from, testWindow.to, 0, flatResolvePath(dir), map[string]bool{}, false)
	if err == nil || !errors.Is(err, ErrChallenged) {
		t.Fatalf("err = %v, want ErrChallenged", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatal("challenge response was written to disk")
	}
}

func TestDownloadNoDownloadInResponse(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/banking-flatex.at/"+headerAreaAction, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"commands":[{"command":"replacePortions"}]}`)
	})
	mux.HandleFunc("/banking-flatex.at/"+archiveListAction, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"commands":[{"command":"replacePortions"}]}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, srv)
	dir := t.TempDir()
	_, _, err := c.Download(testWindow.from, testWindow.to, 0, flatResolvePath(dir), map[string]bool{}, false)
	if err == nil {
		t.Fatal("expected error when response has no download command")
	}
}
