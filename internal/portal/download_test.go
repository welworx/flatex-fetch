package portal

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
		if r.FormValue(fieldApplyFilter) == "true" {
			fmt.Fprint(w, `{"commands":[{"command":"replacePortions"}]}`)
			return
		}
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
	mux.HandleFunc("/banking-flatex.at/downloadData/1/challenge.pdf", func(w http.ResponseWriter, r *http.Request) {
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

// Classic archive row selections belong to the table produced by the last
// Apply Filter action. Date fields submitted with Download alone do not
// establish that table. Windowed listing leaves a different window active,
// so downloading an earlier window must explicitly apply its filter first.
func TestDownloadReappliesListedWindowBeforeSelecting(t *testing.T) {
	for _, domain := range []string{"flatex.at", "flatex.de"} {
		for _, idx := range []int{0, 2} {
			t.Run(fmt.Sprintf("%s/row%d", domain, idx), func(t *testing.T) {
				prefix := "/banking-flatex.at/"
				if domain == "flatex.de" {
					prefix = "/banking-flatex/"
				}
				first := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
				last := time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC)
				windows := map[string][]string{
					"01.03.2025": {"first-0.pdf", "first-1.pdf", "first-2.pdf"},
					"01.12.2025": {"last-0.pdf"},
				}
				var active []string
				var emptyZip bytes.Buffer
				if err := zip.NewWriter(&emptyZip).Close(); err != nil {
					t.Fatal(err)
				}
				mux := http.NewServeMux()
				mux.HandleFunc("POST "+prefix+"headerAreaFormAction.do", func(w http.ResponseWriter, _ *http.Request) {
					fmt.Fprint(w, `{"commands":[]}`)
				})
				mux.HandleFunc("POST "+prefix+"documentArchiveListFormAction.do", func(w http.ResponseWriter, r *http.Request) {
					if r.FormValue("applyFilterButton.clicked") == "true" {
						date := r.FormValue("dateRangeComponent.startDate.text")
						if r.FormValue("dateRangeComponent.endDate.text") != date {
							t.Error("download did not restore the exact listed window")
						}
						active = windows[date]
						var rows strings.Builder
						rows.WriteString(`<div id="documentArchiveListTable">`)
						for i, name := range active {
							fmt.Fprintf(&rows, `<tr class="Read" id="TID1_%d-0"><td class="C2">%s</td><td class="C4"><div class="Ellipsis">%s</div></td></tr>`, i, date, name)
						}
						rows.WriteString(`</div>`)
						if err := json.NewEncoder(w).Encode(map[string]any{"commands": []any{map[string]any{
							"command": "replacePortions", "deltasToApply": []string{"documentArchiveListTable", rows.String()},
						}}}); err != nil {
							t.Error(err)
						}
						return
					}
					if r.FormValue("btnDocumentDownload.clicked") != "true" {
						t.Error("unexpected archive action")
						http.Error(w, "unexpected archive action", http.StatusBadRequest)
						return
					}
					name := "empty.zip"
					for i, candidate := range active {
						if r.FormValue(fmt.Sprintf("documentArchiveListTable.rowSelectionSupport[%d].checked", i)) == "on" {
							name = candidate
						}
					}
					fmt.Fprintf(w, `{"commands":[{"command":"download","location":%q}]}`, prefix+"downloadData/1/"+name)
				})
				mux.HandleFunc("GET "+prefix+"downloadData/1/", func(w http.ResponseWriter, r *http.Request) {
					name := filepath.Base(r.URL.Path)
					if name == "empty.zip" {
						w.Header().Set("Content-Type", "application/zip")
						w.Write(emptyZip.Bytes())
						return
					}
					w.Header().Set("Content-Type", "application/pdf")
					fmt.Fprintf(w, "%%PDF-1.4 %s", name)
				})
				srv := httptest.NewServer(mux)
				defer srv.Close()
				c := newTestClient(t, srv, domain)
				docs, err := c.ListDocumentsDetailed(first, first)
				if err != nil || len(docs) != 3 {
					t.Fatalf("first listing: %d documents, err=%v", len(docs), err)
				}
				if _, err := c.ListDocumentsDetailed(last, last); err != nil {
					t.Fatal(err)
				}
				doc := docs[idx]
				path, skipped, err := c.Download(doc.WindowFrom, doc.WindowTo, doc.Index, flatResolvePath(t.TempDir()), map[string]bool{}, false)
				if err != nil || skipped {
					t.Fatalf("Download: skipped=%v err=%v", skipped, err)
				}
				got, err := os.ReadFile(path)
				want := "%PDF-1.4 " + doc.Name
				if err != nil || string(got) != want {
					t.Fatalf("downloaded content=%q, want %q; err=%v", got, want, err)
				}
			})
		}
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

func TestDownloadDedupAndOverwrite(t *testing.T) {
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
		fmt.Fprint(w, `{"commands":[{"command":"download","location":"/banking-flatex.at/downloadData/1/challenge.pdf"}]}`)
	})
	mux.HandleFunc("/banking-flatex.at/downloadData/1/challenge.pdf", func(w http.ResponseWriter, r *http.Request) {
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

// dispositionServer serves the archive download flow with a caller-chosen
// Content-Disposition on the file response, so tests can drive
// resolveFilename with a hostile server-supplied filename.
func dispositionServer(t *testing.T, disposition string, body []byte) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /banking-flatex.at/"+headerAreaAction, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"commands":[{"command":"replacePortions"}]}`)
	})
	mux.HandleFunc("POST /banking-flatex.at/"+archiveListAction, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"commands":[{"command":"download","location":"/banking-flatex.at/downloadData/1/doc.bin"}]}`)
	})
	mux.HandleFunc("GET /banking-flatex.at/downloadData/1/doc.bin", func(w http.ResponseWriter, r *http.Request) {
		if disposition != "" {
			w.Header().Set("Content-Disposition", disposition)
		}
		w.Write(body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestDownloadHostileFilenameStaysInDestDir drives resolveFilename with
// server-supplied filenames that try to escape the destination directory.
// The portal is the untrusted side of this boundary: whatever it sends, the
// file must land directly inside the directory ResolvePath chose.
func TestDownloadHostileFilenameStaysInDestDir(t *testing.T) {
	for _, tc := range []struct{ name, disposition string }{
		{"parent traversal", `attachment; filename="../../evil.pdf"`},
		{"absolute path", `attachment; filename="/etc/cron.d/evil.pdf"`},
		{"windows separators", `attachment; filename="..\\..\\evil.pdf"`},
		{"double dot", `attachment; filename=".."`},
		{"single dot", `attachment; filename="."`},
		{"empty", `attachment; filename=""`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := dispositionServer(t, tc.disposition, []byte("%PDF-1.4 payload"))
			c := newTestClient(t, srv)
			dir := t.TempDir()

			p, skipped, err := c.Download(testWindow.from, testWindow.to, 0, flatResolvePath(dir), map[string]bool{}, false)
			if err != nil || skipped {
				t.Fatalf("Download: err=%v skipped=%v", err, skipped)
			}
			if filepath.Dir(p) != dir {
				t.Fatalf("wrote %q, want a file directly inside %q", p, dir)
			}
			// sanitize() must strip separators from the server-supplied name.
			// filepath.Dir(p) != dir alone cannot catch a missing backslash
			// guard on Linux, where `\` is an ordinary character: the file
			// would still land inside dir, just named "..\..\evil.pdf".
			if b := filepath.Base(p); strings.ContainsAny(b, `\/`) {
				t.Fatalf("resolved name %q still contains a path separator", b)
			}
			if _, err := os.Stat(p); err != nil {
				t.Fatalf("stat %q: %v", p, err)
			}
		})
	}
}

// TestDownloadZipEntryTraversalRejected covers writeZipEntry's guard: a zip
// whose single entry name climbs out of the destination directory must be
// refused outright, not sanitized into some nearby path and written anyway.
func TestDownloadZipEntryTraversalRejected(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("../../evil.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("%PDF-1.4 evil")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	srv := dispositionServer(t, "", buf.Bytes())
	c := newTestClient(t, srv)
	dir := t.TempDir()

	_, _, err = c.Download(testWindow.from, testWindow.to, 0, flatResolvePath(dir), map[string]bool{}, false)
	if err == nil {
		t.Fatal("expected an error for a zip entry that escapes the destination directory")
	}
	if !strings.Contains(err.Error(), "unsafe zip entry name") {
		t.Fatalf("err = %v, want it to name the unsafe zip entry", err)
	}

	found, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Fatalf("destination dir is not empty after a rejected zip: %v", found)
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

// fixedResolvePath sends every document to the same destination name — the
// within-run collision a -format template can produce (e.g. two documents
// sharing a month and profile).
func fixedResolvePath(dir, name string) ResolvePath {
	return func(string) (string, string) { return dir, name }
}

// TestDownloadWithinRunCollisionSuffixes covers writeFile's seen-map branch
// and suffixed(): documents colliding on one destination within a single run
// get _2/_3 suffixes rather than overwriting each other.
func TestDownloadWithinRunCollisionSuffixes(t *testing.T) {
	srv := downloadServer(t,
		map[string][]byte{
			"/banking-flatex.at/downloadData/1/doc-0.bin": []byte("%PDF-1.4 row0"),
			"/banking-flatex.at/downloadData/1/doc-1.bin": []byte("%PDF-1.4 row1"),
			"/banking-flatex.at/downloadData/1/doc-2.bin": []byte("%PDF-1.4 row2"),
		},
		nil,
	)
	c := newTestClient(t, srv)
	dir := t.TempDir()
	seen := map[string]bool{}

	var paths []string
	for idx := 0; idx < 3; idx++ {
		p, skipped, err := c.Download(testWindow.from, testWindow.to, idx, fixedResolvePath(dir, "same.pdf"), seen, false)
		if err != nil || skipped {
			t.Fatalf("row %d: err=%v skipped=%v", idx, err, skipped)
		}
		paths = append(paths, p)
	}

	want := []string{
		filepath.Join(dir, "same.pdf"),
		filepath.Join(dir, "same_2.pdf"),
		filepath.Join(dir, "same_3.pdf"),
	}
	for i, w := range want {
		if paths[i] != w {
			t.Fatalf("row %d wrote %q, want %q", i, paths[i], w)
		}
	}

	// Every document's own bytes must survive — a collision must not let one
	// document's content overwrite another's.
	for i, p := range paths {
		got, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if w := fmt.Sprintf("%%PDF-1.4 row%d", i); string(got) != w {
			t.Fatalf("%s contains %q, want %q", p, got, w)
		}
	}
}
