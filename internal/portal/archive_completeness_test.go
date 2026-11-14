package portal

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Die Portalantworten sind synthetisch; der Leertext entspricht der beobachteten Oberfläche.
func TestListDocumentsDetailedRecognizesExplicitEmptyRange(t *testing.T) {
	for name, fragment := range map[string]string{
		"vollständiges Statuselement": `<div class="RetrieveStatus"><!-- synthetic -->Keine Dokumente vorhanden.</div>`,
		"inkrementelles Textdelta":    "Keine Dokumente vorhanden.",
	} {
		t.Run(name, func(t *testing.T) {
			filterCalls := 0
			mux := http.NewServeMux()
			mux.HandleFunc("/banking-flatex.at/"+headerAreaAction, func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, `{"commands":[{"command":"replacePortions"}]}`)
			})
			mux.HandleFunc("/banking-flatex.at/"+archiveListAction, func(w http.ResponseWriter, r *http.Request) {
				filterCalls++
				if filterCalls > 1 {
					http.Error(w, "unerwartete zusätzliche Abfrage", http.StatusBadRequest)
					return
				}
				json.NewEncoder(w).Encode(ajaxResponse{Commands: []ajaxCommand{{
					Command:       "replacePortions",
					DeltasToApply: []string{"synthetic-results", fragment},
				}}})
			})
			srv := httptest.NewServer(mux)
			defer srv.Close()
			c := newTestClient(t, srv)
			from := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
			to := time.Date(2006, 9, 18, 0, 0, 0, 0, time.UTC)
			docs, err := c.ListDocumentsDetailed(from, to)
			if err != nil || len(docs) != 0 || filterCalls != 1 {
				t.Fatalf("Expliziter Leerbereich: Dokumente=%d Aufrufe=%d Fehler=%v; erwartet sind null Dokumente und eine Abfrage", len(docs), filterCalls, err)
			}
		})
	}
}

func TestListDocumentsDetailedRejectsUnresolvedSingleDay(t *testing.T) {
	cases := map[string]string{
		"fehlendes Ergebnisfragment": `<div>date picker widget only</div>`,
		"unbeteiligter Leertext":     `<script>const hint = "Keine Dokumente vorhanden.";</script>`,
		"explizite Limitwarnung":     `<div>` + capWarning + `</div>`,
		"Leertext mit Limitwarnung":  `<div class="RetrieveStatus">Keine Dokumente vorhanden.</div><div>` + capWarning + `</div>`,
	}
	var rows strings.Builder
	rows.WriteString(`<div id="documentArchiveListTable_rows"><table><tbody>`)
	for i := 0; i < capLimit; i++ {
		fmt.Fprintf(&rows, `<tr class="I%d First Even" id="TID1_0-%d"><td class="C2 ">01.07.2026</td><td class="C4 "><div class="Ellipsis">Synthetisches Dokument %d</div></td></tr>`, i, i, i)
	}
	rows.WriteString(`</tbody></table></div>`)
	cases["Zeilenanzahl am Limit"] = rows.String()
	for name, fragment := range cases {
		t.Run(name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/banking-flatex.at/"+headerAreaAction, func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, `{"commands":[{"command":"replacePortions"}]}`)
			})
			mux.HandleFunc("/banking-flatex.at/"+archiveListAction, func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(ajaxResponse{Commands: []ajaxCommand{{Command: "replacePortions", DeltasToApply: []string{"synthetic-results", fragment}}}})
			})
			srv := httptest.NewServer(mux)
			defer srv.Close()
			c := newTestClient(t, srv)
			day := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
			if _, err := c.ListDocumentsDetailed(day, day); err == nil {
				t.Fatal("Uneindeutiger Tagesabruf wurde als vollständiges Ergebnis akzeptiert")
			}
		})
	}
}
