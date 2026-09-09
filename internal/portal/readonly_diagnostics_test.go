package portal

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDocumentPolicyReportsRedirectMethodWithoutSecretValues(t *testing.T) {
	const blockedPath = "/banking-flatex/settingsFormAction.do"
	const loginData = "private-login-handoff-token"
	var blockedCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("GET /login/loginIFrameFormAction.do", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `webcore.setTokenId( "private-page-token");`)
	})
	mux.HandleFunc("POST /login/sso", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, blockedPath+"?loginData="+loginData, http.StatusFound)
	})
	mux.HandleFunc(blockedPath, func(w http.ResponseWriter, _ *http.Request) {
		blockedCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := newTestClient(t, srv, "flatex.de")
	err := c.Login("private-user", "private-password")
	if !errors.Is(err, errDocumentOnly) {
		t.Fatalf("Login error = %v, want document-only policy rejection", err)
	}
	var requestError *url.Error
	if !errors.As(err, &requestError) {
		t.Fatalf("Login error = %v, want wrapped HTTP request error", err)
	}
	if requestError.Op != "Post" {
		t.Fatalf("outer method = %q, want original Post", requestError.Op)
	}
	want := fmt.Sprintf("GET %q: %s", blockedPath, errDocumentOnly)
	if got := requestError.Err.Error(); got != want {
		t.Errorf("policy diagnostic = %q, want %q", got, want)
	}
	for _, secret := range []string{loginData, "private-user", "private-password", "private-page-token"} {
		if strings.Contains(requestError.Err.Error(), secret) {
			t.Error("policy diagnostic contains a secret value")
		}
	}
	if got := blockedCalls.Load(); got != 0 {
		t.Errorf("blocked request reached server %d times", got)
	}
}
