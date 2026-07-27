package portal

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
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
	mux.HandleFunc("GET "+pathLoginPage, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `webcore.setTokenId( "tok-1");`)
	})
	mux.HandleFunc("POST "+pathSSO, func(w http.ResponseWriter, r *http.Request) {
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
