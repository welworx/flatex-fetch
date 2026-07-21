package portal

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
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

func TestListDocumentsDetailed(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/banking-flatex.at/"+headerAreaAction, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"commands":[{"command":"replacePortions"}]}`)
	})
	mux.HandleFunc("/banking-flatex.at/"+archiveListAction, func(w http.ResponseWriter, r *http.Request) {
		// Shape confirmed from a live capture 2026-07-17: deltasToApply is a
		// flat [id, html, id, html, ...] list; only the html elements (odd
		// indices) matter. Read rows have no "Unread" class; unread rows do.
		rowsHTML := `<div id="documentArchiveListTable_rows"><table><tbody>` +
			`<tr class="I0 First Even" id="TID1_0-0">` +
			`<td class="C1 "><div class="Ellipsis">acct</div></td>` +
			`<td class="C2 ">16.07.2026</td>` +
			`<td class="C3 "><div class="Ellipsis">Hauptversammlung</div></td>` +
			`<td class="C4 "><div class="Ellipsis">Einladung Hauptversammlung</div></td>` +
			`<td class="C5 Last ">16.07.2026</td>` +
			`</tr>` +
			`<tr class="I0 First Unread Odd" id="TID1_1-0">` +
			`<td class="C1 "><div class="Ellipsis">acct</div></td>` +
			`<td class="C2 ">10.07.2026</td>` +
			`<td class="C3 "><div class="Ellipsis">Kontoauszug</div></td>` +
			`<td class="C4 "><div class="Ellipsis">Kontoauszug vom 10.07.2026</div></td>` +
			`<td class="C5 Last ">-</td>` +
			`</tr>` +
			`</tbody></table></div>`
		var resp struct {
			Commands []struct {
				Command       string   `json:"command"`
				DeltasToApply []string `json:"deltasToApply"`
			} `json:"commands"`
		}
		resp.Commands = []struct {
			Command       string   `json:"command"`
			DeltasToApply []string `json:"deltasToApply"`
		}{{Command: "replacePortions", DeltasToApply: []string{"someId", rowsHTML}}}
		b, _ := json.Marshal(resp)
		w.Write(b)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, srv)
	// mock's response includes tableMarker and no capWarning, so this
	// succeeds as a single query (parsing behavior); splitting on a
	// too-wide/capped response is covered by the tests below.
	from := time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC)
	docs, err := c.ListDocumentsDetailed(from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 {
		t.Fatalf("docs = %v, want 2", docs)
	}
	d0, d1 := docs[0], docs[1]
	if d0.Index != 0 || d0.Name != "Einladung Hauptversammlung" || d0.Category != "Hauptversammlung" || !d0.Read {
		t.Errorf("docs[0] = %+v, want index 0, read Hauptversammlung doc", d0)
	}
	if d0.Date.Format("2006-01-02") != "2026-07-16" {
		t.Errorf("docs[0].Date = %v, want 2026-07-16", d0.Date)
	}
	if d1.Index != 1 || d1.Name != "Kontoauszug vom 10.07.2026" || d1.Category != "Kontoauszug" || d1.Read {
		t.Errorf("docs[1] = %+v, want index 1, unread Kontoauszug doc", d1)
	}
	if d1.Date.Format("2006-01-02") != "2026-07-10" {
		t.Errorf("docs[1].Date = %v, want 2026-07-10", d1.Date)
	}
	if !d0.WindowFrom.Equal(from) || !d0.WindowTo.Equal(to) {
		t.Errorf("docs[0] window = %s..%s, want %s..%s", d0.WindowFrom, d0.WindowTo, from, to)
	}
}

// TestListDocumentsDetailedWindowsWideRange verifies a range the portal
// can't answer in one query gets bisected until every sub-window
// succeeds, rather than sent as one request the portal would silently
// return nothing useful for (see windowedDocuments's doc comment on
// tableMarker). The mock stands in for that real behavior: any query
// wider than maxOKDays gets the real failure signature back (no
// tableMarker, no capWarning — just other page fragments re-rendering);
// anything at or under it succeeds.
func TestListDocumentsDetailedWindowsWideRange(t *testing.T) {
	const maxOKDays = 20
	var allCalls, successCalls []string // "DD.MM.YYYY|DD.MM.YYYY"
	mux := http.NewServeMux()
	mux.HandleFunc("/banking-flatex.at/"+headerAreaAction, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"commands":[{"command":"replacePortions"}]}`)
	})
	mux.HandleFunc("/banking-flatex.at/"+archiveListAction, func(w http.ResponseWriter, r *http.Request) {
		fromStr, toStr := r.FormValue(fieldDateFrom), r.FormValue(fieldDateTo)
		allCalls = append(allCalls, fromStr+"|"+toStr)
		from, _ := time.Parse("02.01.2006", fromStr)
		to, _ := time.Parse("02.01.2006", toStr)
		if int(to.Sub(from).Hours()/24) > maxOKDays {
			// Too wide for the mock portal to answer: matches the real
			// failure mode (neither tableMarker nor capWarning present).
			fmt.Fprint(w, `{"commands":[{"command":"replacePortions","deltasToApply":["id","<div>date picker widget only</div>"]}]}`)
			return
		}
		successCalls = append(successCalls, fromStr+"|"+toStr)
		// one distinct document per successful sub-window, identified by
		// its own window's start date, so we can confirm every window's
		// result made it into the concatenated output
		rowsHTML := `<div id="documentArchiveListTable_rows"><table><tbody><tr class="I0 First Even" id="TID1_0-0">` +
			`<td class="C1 "><div class="Ellipsis">acct</div></td>` +
			`<td class="C2 ">` + fromStr + `</td>` +
			`<td class="C3 "><div class="Ellipsis">Kontoauszug</div></td>` +
			`<td class="C4 "><div class="Ellipsis">doc for ` + fromStr + `</div></td>` +
			`<td class="C5 Last ">-</td></tr></tbody></table></div>`
		var resp struct {
			Commands []struct {
				Command       string   `json:"command"`
				DeltasToApply []string `json:"deltasToApply"`
			} `json:"commands"`
		}
		resp.Commands = []struct {
			Command       string   `json:"command"`
			DeltasToApply []string `json:"deltasToApply"`
		}{{Command: "replacePortions", DeltasToApply: []string{"id", rowsHTML}}}
		b, _ := json.Marshal(resp)
		w.Write(b)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, srv)
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC) // 201 days, well over maxOKDays
	docs, err := c.ListDocumentsDetailed(from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(allCalls) < 2 {
		t.Fatalf("filter calls = %v, want more than one (the initial full-range attempt should fail and trigger a split)", allCalls)
	}
	if len(docs) != len(successCalls) {
		t.Fatalf("got %d documents for %d successful sub-window calls, want one document per success (no gaps/overlaps/duplicates)", len(docs), len(successCalls))
	}
	for _, d := range docs {
		if d.WindowFrom.After(d.WindowTo) {
			t.Errorf("document %+v has an inverted window", d)
		}
		if d.WindowFrom.Before(from) || d.WindowTo.After(to) {
			t.Errorf("document %+v window %s..%s escapes the requested range %s..%s", d, d.WindowFrom, d.WindowTo, from, to)
		}
		if int(d.WindowTo.Sub(d.WindowFrom).Hours()/24) > maxOKDays {
			t.Errorf("document %+v came from a window wider than the mock's success threshold — shouldn't have produced a document", d)
		}
	}
	// sub-windows must be contiguous and non-overlapping, and together
	// cover exactly the originally requested range: sorted by WindowFrom,
	// each one's WindowTo should be the day before the next one's
	// WindowFrom, the first should start at from, the last should end at to.
	sort.Slice(docs, func(i, j int) bool { return docs[i].WindowFrom.Before(docs[j].WindowFrom) })
	if !docs[0].WindowFrom.Equal(from) {
		t.Errorf("first window starts %s, want %s", docs[0].WindowFrom, from)
	}
	if !docs[len(docs)-1].WindowTo.Equal(to) {
		t.Errorf("last window ends %s, want %s", docs[len(docs)-1].WindowTo, to)
	}
	for i := 1; i < len(docs); i++ {
		wantNext := docs[i-1].WindowTo.AddDate(0, 0, 1)
		if !docs[i].WindowFrom.Equal(wantNext) {
			t.Errorf("window %d starts %s, want %s (day after window %d ends %s) — gap or overlap",
				i, docs[i].WindowFrom, wantNext, i-1, docs[i-1].WindowTo)
		}
	}
}

func TestListDocumentsDetailedBisectsCappedWindow(t *testing.T) {
	var filterRanges []string
	mux := http.NewServeMux()
	mux.HandleFunc("/banking-flatex.at/"+headerAreaAction, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"commands":[{"command":"replacePortions"}]}`)
	})
	mux.HandleFunc("/banking-flatex.at/"+archiveListAction, func(w http.ResponseWriter, r *http.Request) {
		from, to := r.FormValue(fieldDateFrom), r.FormValue(fieldDateTo)
		filterRanges = append(filterRanges, from+"|"+to)
		if from == to {
			// narrowed to a single day -> not capped, one real document
			rowsHTML := `<div id="documentArchiveListTable_rows"><table><tbody><tr class="I0 First Even" id="TID1_0-0">` +
				`<td class="C1 "><div class="Ellipsis">acct</div></td>` +
				`<td class="C2 ">` + from + `</td>` +
				`<td class="C3 "><div class="Ellipsis">Kontoauszug</div></td>` +
				`<td class="C4 "><div class="Ellipsis">doc</div></td>` +
				`<td class="C5 Last ">-</td></tr></tbody></table></div>`
			var resp struct {
				Commands []struct {
					Command       string   `json:"command"`
					DeltasToApply []string `json:"deltasToApply"`
				} `json:"commands"`
			}
			resp.Commands = []struct {
				Command       string   `json:"command"`
				DeltasToApply []string `json:"deltasToApply"`
			}{{Command: "replacePortions", DeltasToApply: []string{"id", rowsHTML}}}
			b, _ := json.Marshal(resp)
			w.Write(b)
			return
		}
		// any multi-day window in this test is "capped" -> must bisect
		fmt.Fprint(w, `{"commands":[{"command":"replacePortions","html":"<div class=\"InfoText\">Es werden nur die ersten 100 Dokumente dargestellt.</div>"}]}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, srv)
	from := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC) // 3-day range, capped
	docs, err := c.ListDocumentsDetailed(from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 3 {
		t.Fatalf("got %d documents, want 3 (one per day, after bisecting the capped 3-day window)", len(docs))
	}
}

// TestListDocumentsDetailedBisectsOnRowCountCap reproduces a real
// regression from a live run: a response with exactly capLimit rows, a
// present tableMarker, but WITHOUT capWarning's exact text, was accepted
// as a complete result instead of triggering a further split — silently
// under-counting a year's documents by dozens. Row count alone, not just
// the warning text, must trigger a split.
func TestListDocumentsDetailedBisectsOnRowCountCap(t *testing.T) {
	const wideThresholdDays = 5 // windows wider than this return the "capped" mock response
	mux := http.NewServeMux()
	mux.HandleFunc("/banking-flatex.at/"+headerAreaAction, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"commands":[{"command":"replacePortions"}]}`)
	})
	mux.HandleFunc("/banking-flatex.at/"+archiveListAction, func(w http.ResponseWriter, r *http.Request) {
		fromStr, toStr := r.FormValue(fieldDateFrom), r.FormValue(fieldDateTo)
		from, _ := time.Parse("02.01.2006", fromStr)
		to, _ := time.Parse("02.01.2006", toStr)
		days := int(to.Sub(from).Hours() / 24)

		var rowsHTML string
		if days > wideThresholdDays {
			// The exact shape that slipped through live: capLimit rows,
			// tableMarker present, no capWarning text anywhere.
			var b strings.Builder
			b.WriteString(`<div id="documentArchiveListTable_rows"><table><tbody>`)
			for i := 0; i < capLimit; i++ {
				fmt.Fprintf(&b, `<tr class="I0 First Even" id="TID1_%d-0">`+
					`<td class="C1 "><div class="Ellipsis">acct</div></td>`+
					`<td class="C2 ">%s</td>`+
					`<td class="C3 "><div class="Ellipsis">Kontoauszug</div></td>`+
					`<td class="C4 "><div class="Ellipsis">capped doc %d</div></td>`+
					`<td class="C5 Last ">-</td></tr>`, i, fromStr, i)
			}
			b.WriteString(`</tbody></table></div>`)
			rowsHTML = b.String()
		} else {
			rowsHTML = `<div id="documentArchiveListTable_rows"><table><tbody><tr class="I0 First Even" id="TID1_0-0">` +
				`<td class="C1 "><div class="Ellipsis">acct</div></td>` +
				`<td class="C2 ">` + fromStr + `</td>` +
				`<td class="C3 "><div class="Ellipsis">Kontoauszug</div></td>` +
				`<td class="C4 "><div class="Ellipsis">doc for ` + fromStr + `</div></td>` +
				`<td class="C5 Last ">-</td></tr></tbody></table></div>`
		}
		var resp struct {
			Commands []struct {
				Command       string   `json:"command"`
				DeltasToApply []string `json:"deltasToApply"`
			} `json:"commands"`
		}
		resp.Commands = []struct {
			Command       string   `json:"command"`
			DeltasToApply []string `json:"deltasToApply"`
		}{{Command: "replacePortions", DeltasToApply: []string{"id", rowsHTML}}}
		b, _ := json.Marshal(resp)
		w.Write(b)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, srv)
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC) // 19 days: initial query looks "capped"
	docs, err := c.ListDocumentsDetailed(from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) == 0 {
		t.Fatal("got no documents at all")
	}
	if len(docs) == capLimit {
		t.Fatalf("got exactly capLimit (%d) documents — accepted a capped response as complete instead of splitting further", capLimit)
	}
	for _, d := range docs {
		if int(d.WindowTo.Sub(d.WindowFrom).Hours()/24) > wideThresholdDays {
			t.Errorf("document %+v came from a window wider than the mock's cap threshold — shouldn't have produced a document directly", d)
		}
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
