package portal

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

// Literal routes deliberately do not use production path constants. German
// login routes are from the public iframe/form (2026-09-09), and the archive
// route was observed in the classic UI. Mock responses test request routing,
// not live German session or archive compatibility.
func TestCountryLoginAndArchiveRoutes(t *testing.T) {
	for _, tc := range []struct{ domain, login, banking string }{
		{"flatex.at", "/login.at", "/banking-flatex.at"},
		{"flatex.de", "/login", "/banking-flatex"},
	} {
		t.Run(tc.domain, func(t *testing.T) {
			var seen []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				route := r.Method + " " + r.URL.Path
				seen = append(seen, route)
				switch route {
				case "GET " + tc.login + "/loginIFrameFormAction.do":
					fmt.Fprint(w, `webcore.setTokenId("login-token");`)
				case "POST " + tc.login + "/sso":
					if r.FormValue("userId") != "test-user" || r.FormValue("password") != "test-password" {
						t.Error("login form did not carry expected credentials")
					}
					http.SetCookie(w, &http.Cookie{Name: "flatexSession", Value: "test-session", Path: "/"})
					http.Redirect(w, r, tc.banking+"/accountOverviewFormAction.do", http.StatusFound)
				case "GET " + tc.banking + "/accountOverviewFormAction.do":
					fmt.Fprint(w, `webcore.setTokenId("banking-token");`)
				case "POST " + tc.banking + "/ajaxCommandServlet",
					"POST " + tc.banking + "/headerAreaFormAction.do":
					fmt.Fprint(w, `{"commands":[]}`)
				case "POST " + tc.banking + "/documentArchiveListFormAction.do":
					fmt.Fprint(w, `{"commands":[{"command":"replacePortions","deltasToApply":["documentArchiveListTable","<tr class=\"Read\" id=\"TID1_0-0\"><td class=\"C2\">01.01.2025</td><td class=\"C4\"><div class=\"Ellipsis\">Test statement</div></td></tr>"]}]}`)
				default:
					t.Errorf("unexpected route %s", route)
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			c, err := New(tc.domain, "")
			if err != nil {
				t.Fatal(err)
			}
			if c.baseURL != "https://konto."+tc.domain {
				t.Fatalf("baseURL = %q", c.baseURL)
			}
			c = newTestClient(t, srv, tc.domain)
			if err := c.Login("test-user", "test-password"); err != nil {
				t.Fatal(err)
			}
			date := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
			docs, err := c.ListDocumentsDetailed(date, date)
			if err != nil {
				t.Fatal(err)
			}
			if len(docs) != 1 || docs[0].Name != "Test statement" {
				t.Fatalf("documents = %+v", docs)
			}
			want := []string{
				"GET " + tc.login + "/loginIFrameFormAction.do",
				"POST " + tc.login + "/sso",
				"GET " + tc.banking + "/accountOverviewFormAction.do",
				"GET " + tc.banking + "/accountOverviewFormAction.do",
				"POST " + tc.banking + "/ajaxCommandServlet",
				"POST " + tc.banking + "/headerAreaFormAction.do",
				"POST " + tc.banking + "/documentArchiveListFormAction.do",
			}
			if !reflect.DeepEqual(seen, want) {
				t.Fatalf("requests = %v, want %v", seen, want)
			}
		})
	}
}
