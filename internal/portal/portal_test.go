package portal

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newTestClient points a Client at an httptest server with pacing off.
func newTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	c, err := New("flatex.at", "")
	if err != nil {
		t.Fatal(err)
	}
	c.baseURL = srv.URL
	c.delay = 0
	return c
}

func TestLoginSuccess(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+pathLoginPage, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `webcore.setTokenId( "tok-1");`)
	})
	mux.HandleFunc("POST "+pathSSO, func(w http.ResponseWriter, r *http.Request) {
		if r.FormValue(fieldUserID) != "alice" || r.FormValue(fieldPassword) != "s3cret" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "flatexSession", Value: "abc123", Path: "/"})
	})
	mux.HandleFunc("/banking-flatex.at/"+accountOverviewAction, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `webcore.setTokenId( "tok-2");`)
	})
	mux.HandleFunc("/banking-flatex.at/"+ajaxCommandAction, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"commands":[]}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.Login("alice", "s3cret"); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if c.tokenID != "tok-2" {
		t.Fatalf("tokenID = %q, want tok-2", c.tokenID)
	}
	if c.windowID == "" {
		t.Fatal("windowID not generated")
	}
}

func TestLoginBadCredentials(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+pathLoginPage, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `webcore.setTokenId( "tok-1");`)
	})
	mux.HandleFunc("POST "+pathSSO, func(w http.ResponseWriter, r *http.Request) {
		// no session cookie on bad credentials
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.Login("alice", "wrong"); err == nil {
		t.Fatal("Login with bad credentials succeeded, want error")
	}
}

func TestLoginPostHasNoAjaxHeaders(t *testing.T) {
	var gotXRequestedWith, gotXTokenID string
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+pathLoginPage, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `webcore.setTokenId( "tok-1");`)
	})
	mux.HandleFunc("POST "+pathSSO, func(w http.ResponseWriter, r *http.Request) {
		gotXRequestedWith = r.Header.Get("X-Requested-With")
		gotXTokenID = r.Header.Get("X-tokenId")
		http.SetCookie(w, &http.Cookie{Name: "flatexSession", Value: "x", Path: "/"})
	})
	mux.HandleFunc("/banking-flatex.at/"+accountOverviewAction, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `webcore.setTokenId( "tok-2");`)
	})
	mux.HandleFunc("/banking-flatex.at/"+ajaxCommandAction, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"commands":[]}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.Login("a", "b"); err != nil {
		t.Fatal(err)
	}
	// The real login form submits via $form[0].submit(), bypassing jQuery's
	// AJAX layer entirely — so none of its headers should be present here.
	if gotXRequestedWith != "" {
		t.Errorf("X-Requested-With = %q, want empty (login bypasses the AJAX layer)", gotXRequestedWith)
	}
	if gotXTokenID != "" {
		t.Errorf("X-tokenId = %q, want empty (login bypasses the AJAX layer)", gotXTokenID)
	}
}

func TestUserAgentSent(t *testing.T) {
	gotUA := ""
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+pathLoginPage, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `webcore.setTokenId( "tok-1");`)
	})
	mux.HandleFunc("POST "+pathSSO, func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		http.SetCookie(w, &http.Cookie{Name: "flatexSession", Value: "x", Path: "/"})
	})
	mux.HandleFunc("/banking-flatex.at/"+accountOverviewAction, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `webcore.setTokenId( "tok-2");`)
	})
	mux.HandleFunc("/banking-flatex.at/"+ajaxCommandAction, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"commands":[]}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, srv)
	c.ua = "custom-agent/1.0"
	if err := c.Login("a", "b"); err != nil {
		t.Fatal(err)
	}
	if gotUA != "custom-agent/1.0" {
		t.Fatalf("User-Agent = %q, want custom-agent/1.0", gotUA)
	}
}

func TestListDocuments(t *testing.T) {
	var filterCalls []string // "from|to" per filter POST
	mux := http.NewServeMux()
	mux.HandleFunc("POST /banking-flatex.at/"+headerAreaAction, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"commands":[{"command":"replacePortions"}]}`)
	})
	mux.HandleFunc("POST /banking-flatex.at/"+archiveListAction, func(w http.ResponseWriter, r *http.Request) {
		if r.FormValue(fieldApplyFilter) != "true" {
			t.Errorf("applyFilterButton.clicked not sent")
		}
		if r.FormValue(fieldAccount) != idxAccountDefault {
			t.Errorf("accountSelection not sent explicitly")
		}
		if r.FormValue(fieldRetrievalPeriod) != idxRetrievalPeriodCustom {
			t.Errorf("retrievalPeriodSelection not set to custom")
		}
		filterCalls = append(filterCalls, r.FormValue(fieldDateFrom)+"|"+r.FormValue(fieldDateTo))
		fmt.Fprint(w, `{"commands":[{"command":"replacePortions","html":"<div id=\"documentArchiveListTable_rows\"><span id=\"documentArchiveListTable_rowSelectionSupport[0]_container\"></span><span id=\"documentArchiveListTable_rowSelectionSupport[1]_container\"></span></div>"}]}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, srv)
	from := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	rows, err := c.ListDocuments(from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0] != 0 || rows[1] != 1 {
		t.Fatalf("rows = %v, want [0 1]", rows)
	}
	if len(filterCalls) != 1 || filterCalls[0] != "01.06.2026|10.06.2026" {
		t.Fatalf("filter calls = %v, want one 01.06.2026|10.06.2026", filterCalls)
	}
}

func TestListDocumentsNoResults(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/banking-flatex.at/"+headerAreaAction, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"commands":[{"command":"replacePortions"}]}`)
	})
	mux.HandleFunc("/banking-flatex.at/"+archiveListAction, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"commands":[{"command":"replacePortions","html":"<div>no documents</div>"}]}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, srv)
	from := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	rows, err := c.ListDocuments(from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %v, want none", rows)
	}
}

func TestPostFormFollowsFullPageReplaceAndRetries(t *testing.T) {
	var archiveCalls int
	var resyncGET bool
	var resyncHeaders http.Header
	var retryWindowID string
	mux := http.NewServeMux()
	mux.HandleFunc("/banking-flatex.at/"+headerAreaAction, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"commands":[{"command":"replacePortions"}]}`)
	})
	mux.HandleFunc("/banking-flatex.at/fetchCachedPage", func(w http.ResponseWriter, r *http.Request) {
		resyncGET = true
		resyncHeaders = r.Header.Clone()
		fmt.Fprint(w, `<html>resynced page</html>`)
	})
	mux.HandleFunc("/banking-flatex.at/"+ajaxCommandAction, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"commands":[]}`)
	})
	mux.HandleFunc("/banking-flatex.at/"+archiveListAction, func(w http.ResponseWriter, r *http.Request) {
		archiveCalls++
		if archiveCalls == 1 {
			fmt.Fprint(w, `{"commands":[{"command":"fullPageReplace","fetchLocation":"/banking-flatex.at/fetchCachedPage?windowId=W1"}]}`)
			return
		}
		retryWindowID = r.Header.Get("X-windowId")
		fmt.Fprint(w, `{"commands":[{"command":"replacePortions","html":"<div><span id=\"rowSelectionSupport[0]_container\"></span></div>"}]}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, srv)
	originalWindowID := c.windowID
	from := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	rows, err := c.ListDocuments(from, to)
	if err != nil {
		t.Fatal(err)
	}
	if !resyncGET {
		t.Fatal("resync GET on fetchLocation was never issued")
	}
	if resyncHeaders.Get("X-Requested-With") != "" || resyncHeaders.Get("X-AJAX") != "" {
		t.Errorf("resync GET carried AJAX headers, want a plain navigation-style GET")
	}
	if archiveCalls != 2 {
		t.Fatalf("archive endpoint called %d times, want 2 (initial + retry)", archiveCalls)
	}
	if originalWindowID == "W1" {
		t.Fatal("test setup invalid: client's original windowID already equals the server-issued one")
	}
	if retryWindowID != "W1" {
		t.Fatalf("retry's X-windowId = %q, want %q (adopted from fetchLocation, not the stale original %q)",
			retryWindowID, "W1", originalWindowID)
	}
	if c.windowID != "W1" {
		t.Fatalf("client windowID after retry = %q, want %q", c.windowID, "W1")
	}
	if len(rows) != 1 || rows[0] != 0 {
		t.Fatalf("rows = %v, want [0] (from the retried request)", rows)
	}
}
