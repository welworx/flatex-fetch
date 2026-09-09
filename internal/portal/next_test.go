package portal

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// nextDesktopSegment is the flatex-next path segment these fixtures emulate,
// matching what newTestClient's domain ("flatex.at") derives via
// nextDesktopSegmentFor.
const nextDesktopSegment = "next-desktop.at"

// TestLoginDetectsNextVariant exercises the full flatex-next login sequence
// captured live 2026-07-26: the /login.at/sso POST's redirect chain lands
// on next-desktop.at, which Login must detect, then the resumeLogin
// dance — including one fullPageReplace resync round-trip via
// getAjaxFollowingReplace — must complete before Login returns.
func TestLoginDetectsNextVariant(t *testing.T) {
	var progressCalls atomic.Int32
	var gotResumeLogin atomic.Bool

	mux := http.NewServeMux()
	mux.HandleFunc("GET /login.at/loginIFrameFormAction.do", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `webcore.setTokenId( "tok-1");`)
	})
	mux.HandleFunc("POST /login.at/sso", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "flatexSession", Value: "x", Path: "/"})
		http.Redirect(w, r, "/next-desktop.at/loginCommand?loginData=abc123", http.StatusFound)
	})
	mux.HandleFunc("GET /next-desktop.at/loginCommand", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/"+nextDesktopSegment+"/"+loginProgressAction, http.StatusFound)
	})
	mux.HandleFunc("GET /"+nextDesktopSegment+"/"+loginProgressAction, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Requested-With") != "XMLHttpRequest" {
			// The auto-followed redirect's final GET — plain page load, not ajax.
			fmt.Fprint(w, `<html>shell</html>`)
			return
		}
		if progressCalls.Add(1) == 1 {
			fmt.Fprint(w, `{"commands":[{"command":"fullPageReplace","fetchLocation":"/`+nextDesktopSegment+`/fetchCachedPage?windowId=W999"}]}`)
			return
		}
		fmt.Fprint(w, `{"commands":[]}`)
	})
	mux.HandleFunc("GET /"+nextDesktopSegment+"/fetchCachedPage", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `webcore.setTokenId( "tok-next");`)
	})
	mux.HandleFunc("POST /"+nextDesktopSegment+"/"+ajaxCommandAction, func(w http.ResponseWriter, r *http.Request) {
		if r.FormValue(fieldCommand) == cmdResumeLogin {
			gotResumeLogin.Store(true)
		}
		fmt.Fprint(w, `{"commands":[]}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.Login("alice", "s3cret"); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if c.variant != variantNext {
		t.Fatalf("variant = %v, want variantNext", c.variant)
	}
	if c.archiveListPath != "/"+nextDesktopSegment+"/"+nextArchiveAction {
		t.Fatalf("archiveListPath = %q", c.archiveListPath)
	}
	if c.tokenID != "tok-next" {
		t.Fatalf("tokenID = %q, want tok-next", c.tokenID)
	}
	if c.windowID != "W999" {
		t.Fatalf("windowID = %q, want W999 (from fullPageReplace's fetchLocation)", c.windowID)
	}
	if !gotResumeLogin.Load() {
		t.Fatal("resumeLogin was never called")
	}
	if progressCalls.Load() < 2 {
		t.Fatalf("loginProgressFormAction.do ajax GETs = %d, want >=2 (resync retry)", progressCalls.Load())
	}
}

// nextEntryHTML renders one flatex-next archive-row fragment, matching the
// shape confirmed from live capture (see reNextEntry/reNextDescription).
func nextEntryHTML(idx int, name string, unread bool) string {
	class := "DocumentArchiveListEntryWidget EntryWidget"
	if unread {
		class += " Unread"
	}
	return fmt.Sprintf(
		`<div class="%s" data-widgetname="fullScreenSecondLevelWidgetList[0].secondLevelContentWidget.children[%d].btnOpenDocument">`+
			`<div class="DocumentArchiveListEntryWidgetEntryRow"><div class="Description">%s</div></div></div>`,
		class, idx, name)
}

func nextDateHeaderHTML(date string) string {
	return `<div class="DocumentDate CategoryLabel">` + date + `</div>`
}

// nextArchiveResponse wraps rowsHTML in the {"commands":[{"command":"replacePortions",...}]}
// envelope nextListDocuments/nextDownload parse.
func nextArchiveResponse(rowsHTML string) string {
	b, _ := json.Marshal(ajaxResponse{Commands: []ajaxCommand{
		{Command: "replacePortions", DeltasToApply: []string{"anchor-id", rowsHTML}},
	}})
	return string(b)
}

func TestNextListDocumentsPaginatesUntilPlateau(t *testing.T) {
	batch1 := nextDateHeaderHTML("20.07.2026") + nextEntryHTML(0, "Kontoauszug Juli", true) + nextEntryHTML(1, "Steuerbescheinigung", false)
	batch2 := batch1 + nextEntryHTML(2, "Wertpapierabrechnung", false)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /"+nextDesktopSegment+"/"+nextArchiveAction, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.FormValue(fieldNextOpenArchive) == "true":
			fmt.Fprint(w, nextArchiveResponse(""))
		case r.FormValue(fieldNextDateRangeCustom) == "true":
			fmt.Fprint(w, nextArchiveResponse("")) // opening the sub-dialog, no rows yet
		case r.FormValue(fieldNextDateApply) == "true":
			fmt.Fprint(w, nextArchiveResponse(batch1))
		case r.FormValue(fieldNextReload) == "true":
			scroll := r.FormValue(fieldNextScrollPos)
			if scroll == fmt.Sprint(nextScrollStep) {
				fmt.Fprint(w, nextArchiveResponse(batch2))
			} else {
				fmt.Fprint(w, nextArchiveResponse(batch2)) // plateau: no further growth
			}
		default:
			http.Error(w, "unexpected form", http.StatusBadRequest)
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, srv)
	c.variant = variantNext
	c.archiveListPath = "/" + nextDesktopSegment + "/" + nextArchiveAction

	docs, err := c.ListDocumentsDetailed(testWindow.from, testWindow.to)
	if err != nil {
		t.Fatalf("ListDocumentsDetailed: %v", err)
	}
	if len(docs) != 3 {
		t.Fatalf("got %d documents, want 3: %+v", len(docs), docs)
	}
	if docs[0].Name != "Kontoauszug Juli" {
		t.Fatalf("docs[0].Name = %q, want Kontoauszug Juli", docs[0].Name)
	}
	if docs[0].Read {
		t.Fatalf("docs[0].Read = true, want false (Unread class present)")
	}
	if !docs[1].Read {
		t.Fatalf("docs[1].Read = false, want true")
	}
	wantDate := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	for _, d := range docs {
		if !d.Date.Equal(wantDate) {
			t.Errorf("doc %q date = %v, want %v", d.Name, d.Date, wantDate)
		}
		if !d.WindowFrom.Equal(testWindow.from) || !d.WindowTo.Equal(testWindow.to) {
			t.Errorf("doc %q window = %v..%v", d.Name, d.WindowFrom, d.WindowTo)
		}
	}
}

func TestNextDownload(t *testing.T) {
	batch := nextDateHeaderHTML("20.07.2026") + nextEntryHTML(4, "Fondsthesaurierung", true)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /"+nextDesktopSegment+"/"+nextArchiveAction, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.FormValue(fieldNextOpenArchive) == "true":
			fmt.Fprint(w, nextArchiveResponse(""))
		case r.FormValue(fieldNextDateRangeCustom) == "true":
			fmt.Fprint(w, nextArchiveResponse(""))
		case r.FormValue(fieldNextDateApply) == "true":
			fmt.Fprint(w, nextArchiveResponse(batch))
		case r.FormValue(fmt.Sprintf(fieldNextOpenDocFmt, 4)) == "true":
			fmt.Fprint(w, `{"commands":[{"command":"execute","script":"DocumentViewer.display(\"/`+nextDesktopSegment+`/downloadData/1/doc.pdf\", \"application/pdf\")"}]}`)
		default:
			http.Error(w, "unexpected form", http.StatusBadRequest)
		}
	})
	mux.HandleFunc("GET /"+nextDesktopSegment+"/downloadData/1/doc.pdf", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "%PDF-1.4 fake content")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, srv)
	c.variant = variantNext
	c.archiveListPath = "/" + nextDesktopSegment + "/" + nextArchiveAction
	dir := t.TempDir()

	p, skipped, err := c.Download(testWindow.from, testWindow.to, 4, flatResolvePath(dir), map[string]bool{}, false)
	if err != nil || skipped {
		t.Fatalf("Download: err=%v skipped=%v", err, skipped)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "%PDF-1.4 fake content" {
		t.Fatalf("content = %q", got)
	}
}

// nextScrollServer serves the flatex-next archive flow, answering the Nth
// reload request with batches[N] (the initial apply response is batches[0]).
// Requests past the end of batches repeat the last batch — a plateau.
// Every observed scrollposition is recorded in order.
func nextScrollServer(t *testing.T, batches []string, scrolls *[]string, mu *sync.Mutex) *httptest.Server {
	t.Helper()
	reloads := 0
	mux := http.NewServeMux()
	mux.HandleFunc("POST /"+nextDesktopSegment+"/"+nextArchiveAction, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.FormValue(fieldNextOpenArchive) == "true":
			fmt.Fprint(w, nextArchiveResponse(""))
		case r.FormValue(fieldNextDateRangeCustom) == "true":
			fmt.Fprint(w, nextArchiveResponse(""))
		case r.FormValue(fieldNextDateApply) == "true":
			fmt.Fprint(w, nextArchiveResponse(batches[0]))
		case r.FormValue(fieldNextReload) == "true":
			*scrolls = append(*scrolls, r.FormValue(fieldNextScrollPos))
			reloads++
			i := reloads
			if i >= len(batches) {
				i = len(batches) - 1
			}
			fmt.Fprint(w, nextArchiveResponse(batches[i]))
		default:
			http.Error(w, "unexpected form", http.StatusBadRequest)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newNextTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	c := newTestClient(t, srv)
	c.variant = variantNext
	c.archiveListPath = "/" + nextDesktopSegment + "/" + nextArchiveAction
	return c
}

// nextEntriesHTML renders n cumulative flatex-next entries under one date header.
func nextEntriesHTML(n int) string {
	s := nextDateHeaderHTML("20.07.2026")
	for i := 0; i < n; i++ {
		s += nextEntryHTML(i, fmt.Sprintf("Dokument %d", i), false)
	}
	return s
}

// TestNextListDocumentsScrollPositionAdvances pins that each pagination
// request actually moves scrollposition forward by nextScrollStep. Nothing
// asserted this before, so a loop that re-requested the same position — and
// therefore silently returned only the first batch — would have passed.
func TestNextListDocumentsScrollPositionAdvances(t *testing.T) {
	var mu sync.Mutex
	var scrolls []string
	srv := nextScrollServer(t, []string{nextEntriesHTML(2), nextEntriesHTML(4), nextEntriesHTML(6), nextEntriesHTML(6)}, &scrolls, &mu)
	c := newNextTestClient(t, srv)

	docs, err := c.ListDocumentsDetailed(testWindow.from, testWindow.to)
	if err != nil {
		t.Fatalf("ListDocumentsDetailed: %v", err)
	}
	if len(docs) != 6 {
		t.Fatalf("got %d documents, want 6", len(docs))
	}

	mu.Lock()
	defer mu.Unlock()
	want := []string{"5000", "10000", "15000"}
	if len(scrolls) != len(want) {
		t.Fatalf("scrollpositions = %v, want %v", scrolls, want)
	}
	for i, w := range want {
		if scrolls[i] != w {
			t.Fatalf("scrollpositions = %v, want %v", scrolls, want)
		}
	}
}

// TestNextListDocumentsRejectsShrinkingPage covers the blind spot the old
// mock could not reach: if flatex-next's scroll responses are NOT cumulative,
// a page comes back shorter than its predecessor. The old code took that as
// "no more results" and returned the short list as complete — silent data
// loss. It must be reported instead.
func TestNextListDocumentsRejectsShrinkingPage(t *testing.T) {
	var mu sync.Mutex
	var scrolls []string
	srv := nextScrollServer(t, []string{nextEntriesHTML(4), nextEntriesHTML(6), nextEntriesHTML(2)}, &scrolls, &mu)
	c := newNextTestClient(t, srv)

	_, err := c.ListDocumentsDetailed(testWindow.from, testWindow.to)
	if err == nil {
		t.Fatal("expected an error when a scroll page returns fewer entries than the previous one")
	}
	if !strings.Contains(err.Error(), "fewer") {
		t.Fatalf("err = %v, want it to explain that a page came back with fewer entries", err)
	}
}

// TestNextListDocumentsEmptyReloadEndsPaging covers a reload response that
// carries no replacePortions command at all — parseNextDocuments then
// returns 0 documents with a nil error (see replacePortionsHTML), which
// nextScrollAll must treat as the portal declining to answer an
// out-of-range scrollposition (end of results), not as a shrinking,
// possibly-non-cumulative page. Before the fix this hard-failed every
// flatex-next listing, including small ones well under a single batch.
func TestNextListDocumentsEmptyReloadEndsPaging(t *testing.T) {
	var mu sync.Mutex
	var scrolls []string
	srv := nextScrollServer(t, []string{nextEntriesHTML(2), ""}, &scrolls, &mu)
	c := newNextTestClient(t, srv)

	docs, err := c.ListDocumentsDetailed(testWindow.from, testWindow.to)
	if err != nil {
		t.Fatalf("ListDocumentsDetailed: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("got %d documents, want 2 (the pre-scroll batch)", len(docs))
	}
}

// TestNextListDocumentsScrollCapped bounds the loop: a portal that keeps
// claiming growth must not page forever (each request is paced ~750ms in
// production).
func TestNextListDocumentsScrollCapped(t *testing.T) {
	batches := make([]string, nextMaxScrollPages+5)
	for i := range batches {
		batches[i] = nextEntriesHTML(i + 1)
	}
	var mu sync.Mutex
	var scrolls []string
	srv := nextScrollServer(t, batches, &scrolls, &mu)
	c := newNextTestClient(t, srv)

	_, err := c.ListDocumentsDetailed(testWindow.from, testWindow.to)
	if err == nil {
		t.Fatal("expected an error once the scroll loop hits its request cap")
	}
	if !strings.Contains(err.Error(), "giving up") {
		t.Fatalf("err = %v, want it to say it gave up rather than looping forever", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(scrolls) > nextMaxScrollPages {
		t.Fatalf("made %d scroll requests, want at most %d", len(scrolls), nextMaxScrollPages)
	}
}
