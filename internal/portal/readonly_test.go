package portal

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testLoginFields(t *testing.T) url.Values {
	t.Helper()
	device, err := deviceDataJSON(DefaultUserAgent)
	if err != nil {
		t.Fatal(err)
	}
	return url.Values{fieldUserID: {"alice"}, fieldPassword: {"secret"}, fieldDeviceDetails: {device}, fieldWindowWidth: {"1470"}, fieldWindowHeight: {"956"}}
}

func TestDocumentPolicyRejectsBeforeNetwork(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); fmt.Fprint(w, "ok") }))
	defer srv.Close()
	c := newTestClient(t, srv)
	for _, path := range []string{
		"/banking-flatex.at/orderFormAction.do", "/banking-flatex.at/transferFormAction.do", "/banking-flatex.at/settingsFormAction.do",
		"/next-desktop.at/overviewFormAction.do?command=trade", "/banking-flatex.at/documentArchiveListFormAction.do?action=transfer",
		"/banking-flatex.at/downloadData/1/doc.pdf?action=transfer", "/banking-flatex.at/fetchCachedPage?windowId=W1&windowId=W2",
		"/banking-flatex.at/fetchCachedPage?windowId=W1;action=transfer", "/banking-flatex.at/downloadData/../orderFormAction.do",
		"/banking-flatex.at/downloadData/1/%2e%2e", "/banking-flatex.at/downloadData/1/%252e%252e", "/banking-flatex.at/downloadData/1/doc.pdf;command=trade",
		"/banking-flatex.at/downloadData/1%2ftrade/doc.pdf", "/banking-flatex.at/downloadData/1/doc%5c.pdf",
		"/banking-flatex/documentArchiveListFormAction.do", "/next-desktop.de/overviewFormAction.do",
	} {
		t.Run(path, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
			if response, err := c.hc.Do(req); err == nil {
				_ = response.Body.Close()
				t.Fatal("prohibited direct request accepted")
			}
		})
	}
	for _, method := range []string{"PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"} {
		req, _ := http.NewRequest(method, srv.URL+c.archiveListPath, nil)
		if response, err := c.hc.Do(req); err == nil {
			_ = response.Body.Close()
			t.Errorf("accepted %s", method)
		}
	}
	for _, header := range []string{"X-HTTP-Method-Override", "X-HTTP-Method", "X-Method-Override", "X-Original-URL", "X-Rewrite-URL"} {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+c.loginPagePath, nil)
		req.Header.Set(header, "DELETE")
		if response, err := c.hc.Do(req); err == nil {
			_ = response.Body.Close()
			t.Errorf("accepted %s", header)
		}
	}
	req, _ := http.NewRequest(http.MethodGet, srv.URL+c.loginPagePath, nil)
	req.Host = "evil.example"
	if response, err := c.hc.Do(req); err == nil {
		_ = response.Body.Close()
		t.Error("accepted Host override")
	}
	for _, mutate := range []func(*http.Request){
		func(r *http.Request) { r.Header.Add("Content-Type", "application/json") },
		func(r *http.Request) { r.Trailer = http.Header{"X-Http-Method-Override": {"DELETE"}} },
		func(r *http.Request) { r.TransferEncoding = []string{"chunked"} },
		func(r *http.Request) { r.ContentLength++ },
	} {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+c.ssoPath, strings.NewReader(testLoginFields(t).Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		mutate(req)
		if response, err := c.hc.Do(req); err == nil {
			_ = response.Body.Close()
			t.Error("accepted ambiguous request framing or headers")
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("%d prohibited requests reached server", calls.Load())
	}
}

func TestDocumentPolicyRejectsUnsafeForms(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); fmt.Fprint(w, "ok") }))
	defer srv.Close()
	c := newTestClient(t, srv)
	archive := func() url.Values {
		f := archiveFilterForm(time.Now(), time.Now())
		f.Set(fieldApplyFilter, "true")
		return f
	}
	for _, field := range []string{"order.clicked", "transfer.clicked", "settings.clicked", "sessionPassword", "tan", "command", "_method"} {
		f := archive()
		f.Set(field, "true")
		if _, err := c.postFormOnce(c.archiveListPath, f); err == nil {
			t.Errorf("accepted %s", field)
		}
	}
	for _, value := range []string{"on", "true", "", "off&command=trade"} {
		f := archive()
		f.Set(fieldStoreSettings, value)
		if _, err := c.postFormOnce(c.archiveListPath, f); err == nil {
			t.Errorf("accepted storeSettings %q", value)
		}
	}
	f := archive()
	f.Add(fieldStoreSettings, "on")
	if _, err := c.postFormOnce(c.archiveListPath, f); err == nil {
		t.Error("accepted duplicate field")
	}
	for _, command := range []string{"trade", "transfer", "changeSettings", "resumeLogin"} {
		if _, err := c.postAjaxCommand(command, nil); err == nil {
			t.Errorf("accepted classical %s", command)
		}
	}
	for _, extra := range []url.Values{{fieldCommand: {cmdResumeLogin, "trade"}}, {fieldCommand: {cmdResumeLogin}, "tan": {"1234"}}} {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/next-desktop.at/ajaxCommandServlet", strings.NewReader(extra.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if response, err := c.hc.Do(req); err == nil {
			_ = response.Body.Close()
			t.Error("accepted unsafe Next command")
		}
	}
	for _, field := range []string{"order.clicked", "transfer.clicked", "settings.clicked"} {
		f := url.Values{fieldNextOverviewIdx: {"0"}, fieldNextOpenArchive: {"true"}, field: {"true"}}
		if _, err := c.postFormOnce("/next-desktop.at/overviewFormAction.do", f); err == nil {
			t.Errorf("accepted Next %s", field)
		}
	}
	f = testLoginFields(t)
	f.Set("sessionPassword", "true")
	if _, err := c.postFormOnce(c.ssoPath, f); err == nil {
		t.Error("accepted session authorization")
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, _ := mw.CreateFormFile(fieldUserID, "payload")
	_, _ = part.Write([]byte("alice"))
	_ = mw.Close()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+c.ssoPath, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if response, err := c.hc.Do(req); err == nil {
		_ = response.Body.Close()
		t.Error("accepted file part")
	}
	req, _ = http.NewRequest(http.MethodPost, srv.URL+c.ssoPath, strings.NewReader(strings.Repeat("x", 65537)))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if response, err := c.hc.Do(req); err == nil {
		_ = response.Body.Close()
		t.Error("accepted oversized body")
	}
	if calls.Load() != 0 {
		t.Fatalf("%d prohibited requests reached server", calls.Load())
	}
}

func TestDocumentPolicyRejectsSameOriginRedirectActions(t *testing.T) {
	for _, status := range []int{301, 302, 303, 307, 308} {
		for _, target := range []string{"/banking-flatex.at/orderFormAction.do", "/banking-flatex.at/transferFormAction.do", "/next-desktop.at/overviewFormAction.do", "/banking-flatex.at/ajaxCommandServlet?command=trade"} {
			t.Run(fmt.Sprintf("%d%s", status, target), func(t *testing.T) {
				var calls atomic.Int32
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					if r.URL.Path == "/login.at/sso" {
						http.Redirect(w, r, target, status)
						return
					}
					t.Error("redirected operation reached server")
				}))
				defer srv.Close()
				c := newTestClient(t, srv)
				req, _ := http.NewRequest(http.MethodPost, srv.URL+c.ssoPath, strings.NewReader(testLoginFields(t).Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				// For 301/302/303, a plain overview GET is safe and intentional. Add
				// an action parameter so the redirect represents an actual operation.
				if target == "/next-desktop.at/overviewFormAction.do" && status < 307 {
					target += "?command=trade"
				}
				if response, err := c.hc.Do(req); err == nil {
					_ = response.Body.Close()
					t.Error("accepted redirected operation")
				}
				if calls.Load() != 1 {
					t.Errorf("outbound requests=%d, want only initial login", calls.Load())
				}
			})
		}
	}
}

// German classic's live redirect chain (2026-09-09) reaches loginCommand,
// then the query-free loginProgressFormAction.do. The previous fixture went
// straight to accountOverview and missed the guard's rejection of progress.
// Exercise both GET and preserved-POST handoffs through net/http, then use
// the resulting session to list and download a document. Paths are literal
// so a mistaken production path constant cannot make this fixture pass.
func TestGermanClassicLoginHandoff(t *testing.T) {
	for _, status := range []int{http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var seen []string
			const document = "%PDF-1.4 synthetic German statement"
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				route := r.Method + " " + r.URL.Path
				seen = append(seen, route)
				switch route {
				case "GET /login/loginIFrameFormAction.do":
					fmt.Fprint(w, `webcore.setTokenId("login-token");`)
				case "POST /login/sso":
					http.Redirect(w, r, "/banking-flatex/loginCommand?loginData=synthetic-token", status)
				case "GET /banking-flatex/loginCommand", "POST /banking-flatex/loginCommand":
					if err := r.ParseForm(); err != nil {
						t.Error(err)
						return
					}
					if r.URL.Query().Get("loginData") != "synthetic-token" {
						t.Error("handoff lost login query")
					}
					if status == http.StatusTemporaryRedirect || status == http.StatusPermanentRedirect {
						if r.Method != "POST" || len(r.PostForm) != 5 || r.PostForm.Get("userId") != "test-user" || r.PostForm.Get("password") != "test-password" {
							t.Error("handoff lost original login fields")
						}
					} else if r.Method != "GET" || len(r.PostForm) != 0 || r.ContentLength != 0 {
						t.Error("GET handoff retained a login body")
					}
					http.SetCookie(w, &http.Cookie{Name: "flatexSession", Value: "test-session", Path: "/"})
					http.Redirect(w, r, "/banking-flatex/loginProgressFormAction.do", http.StatusFound)
				case "GET /banking-flatex/loginProgressFormAction.do":
					if r.URL.RawQuery != "" || r.ContentLength != 0 || r.Header.Get("X-Requested-With") != "" {
						t.Error("login progress must be a plain, query-free GET")
					}
					fmt.Fprint(w, `<html>login progress</html>`)
				case "GET /banking-flatex/accountOverviewFormAction.do":
					fmt.Fprint(w, `webcore.setTokenId("banking-token");`)
				case "POST /banking-flatex/ajaxCommandServlet":
					if r.FormValue("command") != "engineStartUp" || r.Header.Get("X-Tokenid") != "banking-token" {
						t.Error("session startup did not use the banking token")
					}
					fmt.Fprint(w, `{"commands":[]}`)
				case "POST /banking-flatex/headerAreaFormAction.do":
					fmt.Fprint(w, `{"commands":[]}`)
				case "POST /banking-flatex/documentArchiveListFormAction.do":
					switch {
					case r.FormValue("applyFilterButton.clicked") == "true":
						fmt.Fprint(w, `{"commands":[{"command":"replacePortions","deltasToApply":["documentArchiveListTable","<tr class=\"Read\" id=\"TID1_0-0\"><td class=\"C2\">01.01.2025</td><td class=\"C4\"><div class=\"Ellipsis\">Test statement</div></td></tr>"]}]}`)
					case r.FormValue("btnDocumentDownload.clicked") == "true" && r.FormValue("documentArchiveListTable.rowSelectionSupport[0].checked") == "on":
						fmt.Fprint(w, `{"commands":[{"command":"download","location":"/banking-flatex/downloadData/1/statement.pdf"}]}`)
					default:
						t.Error("unexpected archive operation")
						http.Error(w, "unexpected archive operation", http.StatusBadRequest)
					}
				case "GET /banking-flatex/downloadData/1/statement.pdf":
					w.Header().Set("Content-Type", "application/pdf")
					fmt.Fprint(w, document)
				default:
					t.Errorf("unexpected route %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			c := newTestClient(t, srv, "flatex.de")
			if err := c.Login("test-user", "test-password"); err != nil {
				t.Fatal(err)
			}
			if c.variant != variantOld {
				t.Fatal("German classic progress changed the portal variant")
			}
			date := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
			docs, err := c.ListDocumentsDetailed(date, date)
			if err != nil || len(docs) != 1 || docs[0].Name != "Test statement" {
				t.Fatalf("ListDocumentsDetailed: documents=%+v err=%v", docs, err)
			}
			path, skipped, err := c.Download(docs[0].WindowFrom, docs[0].WindowTo, docs[0].Index, flatResolvePath(t.TempDir()), map[string]bool{}, false)
			if err != nil || skipped {
				t.Fatalf("Download: skipped=%v err=%v", skipped, err)
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != document {
				t.Fatalf("downloaded content=%q err=%v", got, err)
			}
			handoffMethod := "GET"
			if status == http.StatusTemporaryRedirect || status == http.StatusPermanentRedirect {
				handoffMethod = "POST"
			}
			want := []string{
				"GET /login/loginIFrameFormAction.do",
				"POST /login/sso",
				handoffMethod + " /banking-flatex/loginCommand",
				"GET /banking-flatex/loginProgressFormAction.do",
				"GET /banking-flatex/accountOverviewFormAction.do",
				"POST /banking-flatex/ajaxCommandServlet",
				"POST /banking-flatex/headerAreaFormAction.do",
				"POST /banking-flatex/documentArchiveListFormAction.do",
				"POST /banking-flatex/headerAreaFormAction.do",
				"POST /banking-flatex/documentArchiveListFormAction.do",
				"POST /banking-flatex/documentArchiveListFormAction.do",
				"GET /banking-flatex/downloadData/1/statement.pdf",
			}
			if !reflect.DeepEqual(seen, want) {
				t.Fatalf("requests=%v, want %v", seen, want)
			}
		})
	}
}

func TestGermanClassicLoginHandoffRejectsOtherOperations(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, "unexpected")
	}))
	defer srv.Close()
	c := newTestClient(t, srv, "flatex.de")
	for _, tc := range []struct{ method, path, query string }{
		{"GET", "/banking-flatex/loginCommand", "loginData=synthetic"},
		{"PUT", "/banking-flatex/loginCommand", "loginData=synthetic"},
		{"POST", "/banking-flatex/loginCommand", ""},
		{"POST", "/banking-flatex/loginCommand", "loginData="},
		{"POST", "/banking-flatex/loginCommand", "loginData=one&loginData=two"},
		{"POST", "/banking-flatex/loginCommand", "loginData=synthetic&command=trade"},
		{"POST", "/banking-flatex/loginCommand", "loginData=synthetic%00"},
		{"POST", "/banking-flatex/loginCommand", "loginData=%zz"},
		{"POST", "/banking-flatex/orderFormAction.do", "loginData=synthetic"},
		{"POST", "/banking-flatex/transferFormAction.do", "loginData=synthetic"},
		{"POST", "/banking-flatex/settingsFormAction.do", "loginData=synthetic"},
		{"POST", "/login/sso", "loginData=synthetic"},
		{"POST", "/banking-flatex.at/loginCommand", "loginData=synthetic"},
		{"POST", "/next-desktop.de/loginCommand", "loginData=synthetic"},
	} {
		req, err := http.NewRequest(tc.method, srv.URL+tc.path+"?"+tc.query, strings.NewReader(testLoginFields(t).Encode()))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if response, err := c.hc.Do(req); err == nil {
			_ = response.Body.Close()
			t.Errorf("accepted %s %s?%s", tc.method, tc.path, tc.query)
		}
	}
	// Bodyless GET handoffs still reject malformed or extra query fields.
	for _, query := range []string{"", "loginData=", "loginData=one&loginData=two", "loginData=synthetic&command=trade", "loginData=synthetic%00", "loginData=%zz"} {
		req, err := http.NewRequest(http.MethodGet, srv.URL+"/banking-flatex/loginCommand?"+query, nil)
		if err != nil {
			t.Fatal(err)
		}
		if response, err := c.hc.Do(req); err == nil {
			_ = response.Body.Close()
			t.Errorf("accepted unsafe GET query %q", query)
		}
	}
	for _, field := range []string{"order.clicked", "transfer.clicked", "settings.clicked", "sessionPassword", "tan", "command", "_method", "loginData", "userId"} {
		f := testLoginFields(t)
		f.Add(field, "unsafe")
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/banking-flatex/loginCommand?loginData=synthetic", strings.NewReader(f.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if response, err := c.hc.Do(req); err == nil {
			_ = response.Body.Close()
			t.Errorf("accepted extra body field %s", field)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("%d prohibited requests reached server", calls.Load())
	}
}

func TestGermanClassicLoginProgressRejectsOtherOperations(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, "unexpected")
	}))
	defer srv.Close()
	c := newTestClient(t, srv, "flatex.de")
	const progress = "/banking-flatex/loginProgressFormAction.do"
	for _, tc := range []struct{ method, path, body string }{
		{"GET", progress + "?command=trade", ""},
		{"GET", progress + "?loginData=synthetic", ""},
		{"GET", progress + "?command=one&command=two", ""},
		{"GET", progress + "?%zz", ""},
		{"GET", progress, "command=trade"},
		{"POST", progress, testLoginFields(t).Encode()},
		{"POST", progress, "command=resumeLogin"},
		{"PUT", progress, ""},
		{"DELETE", progress, ""},
		{"HEAD", progress, ""},
		{"GET", "/banking-flatex.at/loginProgressFormAction.do", ""},
		{"GET", "/banking-flatex/loginProgressFormAction.do/orderFormAction.do", ""},
	} {
		req, err := http.NewRequest(tc.method, srv.URL+tc.path, strings.NewReader(tc.body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if response, err := c.hc.Do(req); err == nil {
			_ = response.Body.Close()
			t.Errorf("accepted %s %s", tc.method, tc.path)
		}
	}
	// The German route must not become available to an Austrian client.
	at := newTestClient(t, srv)
	if _, err := at.plainGet(progress); err == nil {
		t.Error("Austrian client accepted German login progress")
	}
	if calls.Load() != 0 {
		t.Fatalf("%d prohibited requests reached server", calls.Load())
	}
}

func TestGermanClassicLoginProgressRejectsUnsafeRedirects(t *testing.T) {
	for _, status := range []int{301, 302, 303, 307, 308} {
		for _, target := range []string{
			"/banking-flatex/orderFormAction.do",
			"/banking-flatex/transferFormAction.do",
			"/banking-flatex/settingsFormAction.do",
			"/banking-flatex/loginProgressFormAction.do?command=trade",
			"/banking-flatex/ajaxCommandServlet?command=resumeLogin",
		} {
			t.Run(fmt.Sprintf("%d%s", status, target), func(t *testing.T) {
				var calls atomic.Int32
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if calls.Add(1) == 1 {
						http.Redirect(w, r, target, status)
						return
					}
					t.Error("unsafe redirect reached server")
					http.Error(w, "unexpected", http.StatusBadRequest)
				}))
				defer srv.Close()
				c := newTestClient(t, srv, "flatex.de")
				if _, err := c.plainGet("/banking-flatex/loginProgressFormAction.do"); err == nil {
					t.Error("accepted unsafe redirect from login progress")
				}
				if calls.Load() != 1 {
					t.Fatalf("requests=%d, want only login progress", calls.Load())
				}
			})
		}
	}
}
