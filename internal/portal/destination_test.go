package portal

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestNewRejectsUnapprovedDomains(t *testing.T) {
	for _, domain := range []string{"", "evil.example", "flatex.at.evil.example", "flatex.at@evil.example", "flatex.at:443", "flatex.at/", "FLATEX.AT", "https://flatex.de"} {
		t.Run(domain, func(t *testing.T) {
			if _, err := New(domain, ""); err == nil {
				t.Fatal("accepted unapproved domain")
			}
		})
	}
}

func TestProductionDestinations(t *testing.T) {
	for _, domain := range []string{"flatex.at", "flatex.de"} {
		c, err := New(domain, "")
		if err != nil {
			t.Fatal(err)
		}
		for _, location := range []string{"/archive/doc.pdf", c.baseURL + "/archive/doc.pdf"} {
			if _, err := c.resolveLocation(location); err != nil {
				t.Fatalf("valid %s: %v", location, err)
			}
		}
		for _, location := range []string{
			"http://konto." + domain + "/doc.pdf",
			c.baseURL + ".evil.example/doc.pdf",
			c.baseURL + ":8443/doc.pdf",
			"https://attacker@konto." + domain + "/doc.pdf",
			"https://evil.example/doc.pdf", "//evil.example/doc.pdf",
			"https://konto.flatex-bank.com/doc.pdf", "file:///tmp/doc.pdf",
		} {
			if _, err := c.resolveLocation(location); err == nil {
				t.Errorf("accepted %s", location)
			}
		}
		other := "https://konto.flatex.de/doc.pdf"
		if domain == "flatex.de" {
			other = "https://konto.flatex.at/doc.pdf"
		}
		if _, err := c.resolveLocation(other); err == nil {
			t.Error("accepted other country's origin")
		}
	}
}

func TestRedirectsNeverSendSecretsOffOrigin(t *testing.T) {
	var received atomic.Int32
	attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		received.Add(1)
		fmt.Fprint(w, "stolen")
	}))
	defer attacker.Close()
	for _, status := range []int{301, 302, 303, 307, 308} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, attacker.URL+"/?loginData=secret", status)
			}))
			defer srv.Close()
			c := newTestClient(t, srv)
			c.tokenID = "secret-token"
			req, err := http.NewRequest(http.MethodPost, srv.URL+"/login", strings.NewReader("password=secret"))
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := c.do(req, true); err == nil {
				t.Fatal("followed unapproved redirect")
			}
		})
	}
	if received.Load() != 0 {
		t.Fatal("request reached attacker")
	}
}

func TestRedirectRejectsHTTPSDowngrade(t *testing.T) {
	var received atomic.Int32
	var destination string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		http.Redirect(w, r, destination, http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	destination = strings.Replace(srv.URL, "https:", "http:", 1) + "/leak"
	c := newTestClient(t, srv)
	c.hc.Transport = srv.Client().Transport
	if _, err := c.plainGet("/start"); err == nil || !strings.Contains(err.Error(), "blocked request") {
		t.Fatalf("expected origin rejection, got %v", err)
	}
	if received.Load() != 1 {
		t.Fatal("redirect reached server")
	}
}

func TestValidSameOriginRedirectAndLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, "/finish", http.StatusTemporaryRedirect)
		case "/finish":
			b, _ := io.ReadAll(r.Body)
			if r.Method != http.MethodPost || string(b) != "password=secret" || r.Header.Get("X-tokenId") != "token" {
				t.Error("same-origin redirect lost request data")
			}
			fmt.Fprint(w, "ok")
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		}
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	c.tokenID = "token"
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/start", strings.NewReader("password=secret"))
	if body, _, err := c.do(req, true); err != nil || body != "ok" {
		t.Fatalf("valid redirect failed: %q, %v", body, err)
	}
	if _, err := c.plainGet("/loop"); err == nil || !strings.Contains(err.Error(), "10 redirects") {
		t.Fatalf("redirect limit: %v", err)
	}
}

func TestServerLocationsRejectedBeforeRequest(t *testing.T) {
	var received atomic.Int32
	attacker := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { received.Add(1) }))
	defer attacker.Close()
	c, err := New("flatex.de", "")
	if err != nil {
		t.Fatal(err)
	}
	c.delay = 0
	if _, err := c.plainGet(attacker.URL); err == nil {
		t.Error("accepted off-origin resync")
	}
	if _, _, err := c.fetchLocation(attacker.URL, nil, nil, false); err == nil {
		t.Error("accepted off-origin download")
	}
	req, _ := http.NewRequest(http.MethodPost, attacker.URL, strings.NewReader("secret"))
	if _, _, err := c.do(req, true); err == nil {
		t.Error("accepted off-origin direct request")
	}
	if received.Load() != 0 {
		t.Fatal("off-origin request reached server")
	}
	// Changing the base URL must not change the trusted origin.
	c.baseURL = attacker.URL
	if _, err := c.plainGet("/resync"); err == nil {
		t.Error("base URL mutation changed trusted origin")
	}
}

func TestRedirectRejectsMalformedDestination(t *testing.T) {
	c, _ := New("flatex.at", "")
	u, _ := url.Parse("https://user@konto.flatex.at/path")
	if err := c.checkRedirect(&http.Request{URL: u}, nil); err == nil {
		t.Fatal("accepted userinfo in redirect")
	}
}
