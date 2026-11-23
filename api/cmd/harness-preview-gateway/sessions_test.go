package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"golang.org/x/crypto/ssh"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/harnesssession"
)

func TestOpsSessionLaunchReplayAndLogout(t *testing.T) {
	pub, signer, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	const host = "opencode-preview.ops.bifrost.lan"
	const parent = "https://ops.bifrost.lan"
	broker := newSessions("ops.bifrost.lan", pub, map[string]string{host: "opencode", "deepseek-preview.ops.bifrost.lan": "deepseek"})
	private, err := ssh.MarshalPrivateKey(signer, "test")
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "signer")
	if err = os.WriteFile(file, pem.EncodeToMemory(private), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HARNESS_GRANT_SIGNER_FILE", file)
	issue := httptest.NewRecorder()
	harnesssession.IssueForAdmin(issue, httptest.NewRequest(http.MethodPost, "/api/v1/harness-preview/grants", strings.NewReader(`{"app":"opencode"}`)))
	var grantBody struct{ Grant string }
	if issue.Code != 200 || json.Unmarshal(issue.Body.Bytes(), &grantBody) != nil || grantBody.Grant == "" {
		t.Fatal("the Ops grant API refused a valid test key")
	}
	grant := grantBody.Grant
	call := func(path, origin string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "https://"+host+path, strings.NewReader(url.Values{"grant": {grant}}.Encode()))
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		broker.serve(w, r)
		return w
	}
	if code := call("/.bifrost/session", "https://unrelated.example", nil).Code; code != 403 {
		t.Fatalf("cross-origin request accepted: %d", code)
	}
	w := call("/.bifrost/session", parent, nil)
	if w.Code != 204 || w.Header().Get("Access-Control-Allow-Origin") != parent {
		t.Fatalf("launch: %d", w.Code)
	}
	cookie := w.Result().Cookies()[0]
	if !cookie.Secure || !cookie.HttpOnly || cookie.Path != "/" || cookie.Domain != "" || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("unsafe preview cookie attributes")
	}
	if code := call("/.bifrost/session", parent, nil).Code; code != 403 {
		t.Fatalf("grant replay returned %d", code)
	}
	request := httptest.NewRequest(http.MethodGet, "https://"+host+"/", nil)
	request.AddCookie(cookie)
	request.AddCookie(&http.Cookie{Name: "native", Value: "value"})
	active, ok := broker.lookup(request)
	if !ok {
		t.Fatal("preview cookie refused")
	}
	request.Host = "deepseek-preview.ops.bifrost.lan"
	if _, ok := broker.lookup(request); ok {
		t.Fatal("accepted cookie at wrong harness")
	}
	request.Host = host
	stripSessionCookie(request)
	if h := request.Header.Get("Cookie"); h != "native=value" {
		t.Fatalf("unexpected native cookies: %s", h)
	}
	if w := call("/.bifrost/logout", parent, cookie); w.Code != 204 {
		t.Fatalf("logout: %d", w.Code)
	}
	if active.context.Err() == nil {
		t.Fatal("logout did not close session context")
	}
	request.AddCookie(cookie)
	if _, ok := broker.lookup(request); ok {
		t.Fatal("logged out cookie was accepted")
	}
}
