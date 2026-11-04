package actuation

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// CollectorNameReserved, the configuration half (R5:5.5-12 §1.2, §1.5):
// a name in the collector namespace loads only as a reporter named
// collector:<host>, once, so the one token named collector:h1 is the only
// principal that derives thread:collector@h1.
func TestCollectorNameReservedConfig(t *testing.T) {
	entry := func(name, role, token string) string {
		s := "  - name: \"" + name + "\"\n"
		if role != "" {
			s += "    role: " + role + "\n"
		}
		if token != "" {
			s += "    token: " + token + "\n"
		}
		return s
	}
	for name, body := range map[string]string{
		"upper-case host":    entry("collector:H1", "reporter", "fixture-collector-1"),
		"no host":            entry("collector:", "reporter", "fixture-collector-1"),
		"a dot in the host":  entry("collector:h1.lan", "reporter", "fixture-collector-1"),
		"a host too long":    entry("collector:"+strings.Repeat("h", 33), "reporter", "fixture-collector-1"),
		"padded":             entry(" collector:h1", "reporter", "fixture-collector-1"),
		"an operator":        entry("collector:h1", "operator", "fixture-collector-1"),
		"an admin":           entry("collector:h1", "admin", "fixture-collector-1"),
		"a viewer (no role)": entry("collector:h1", "", "fixture-collector-1"),
		"an agentd":          entry("collector:h1", "agentd", "fixture-collector-1"),
		"named twice":        entry("collector:h1", "reporter", "fixture-collector-1") + entry("collector:h1", "reporter", "fixture-collector-2"),
		"twice, one unset":   entry("collector:h1", "reporter", "fixture-collector-1") + entry("collector:h1", "reporter", ""),
		"misnamed, unset":    entry("collector:H1", "reporter", ""),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadAuth(writeAuthFile(t, "tokens:\n"+body)); err == nil {
				t.Fatalf("loaded:\n%s", body)
			}
		})
	}

	auth, err := LoadAuth(writeAuthFile(t, "tokens:\n"+entry("collector:h1", "reporter", "fixture-collector-1")+
		entry("collector:h2", "reporter", "fixture-collector-2")+entry("reporter", "reporter", "fixture-reporter-1")))
	if err != nil {
		t.Fatal(err)
	}
	for token, want := range map[string]string{"fixture-collector-1": "thread:collector@h1", "fixture-collector-2": "thread:collector@h2", "fixture-reporter-1": ""} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		p, ok := auth.Authenticate(r)
		if !ok {
			t.Fatalf("%s did not authenticate", token)
		}
		actor, isCollector := CollectorActor(p)
		host, _ := CollectorHost(p)
		if actor != want || isCollector != (want != "") || IsCollector(p) != (want != "") ||
			want != "" && "thread:collector@"+host != want {
			t.Fatalf("%+v: actor %q host %q collector %v", p, actor, host, isCollector)
		}
	}
	// Only a reporter named collector:<host> derives a collector actor, so a
	// principal built any other way (no configuration admits one) does not.
	for _, p := range []Principal{{Name: "collector:h1", Role: RoleOperator}, {Name: "collector:H1", Role: RoleReporter},
		{Name: "collector:", Role: RoleReporter}, {Name: "collector@h1", Role: RoleReporter}} {
		if actor, ok := CollectorActor(p); ok {
			t.Fatalf("%+v derives %s", p, actor)
		}
	}
}

// ConfineCollectors refuses a collector every request its table does not
// admit, before the handler; any other token, and none, pass untouched,
// whatever the table says.
func TestConfineCollectors(t *testing.T) {
	auth, err := LoadAuth(writeAuthFile(t, `tokens:
  - name: collector:h1
    role: reporter
    token: fixture-collector-1
  - name: reporter
    role: reporter
    token: fixture-reporter-1
  - name: admin
    role: admin
    token: fixture-admin-1
`))
	if err != nil {
		t.Fatal(err)
	}
	gate := auth.ConfineCollectors(func(r *http.Request) bool { return r.URL.Path == "/allowed" })
	for _, c := range []struct {
		token, path string
		reached     bool
	}{
		{"fixture-collector-1", "/allowed", true},
		{"fixture-collector-1", "/other", false},
		{"fixture-reporter-1", "/other", true},
		{"fixture-admin-1", "/other", true},
		{"", "/other", true},
		{"not-a-token", "/other", true},
	} {
		reached := false
		h := gate(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
		r := httptest.NewRequest(http.MethodGet, c.path, nil)
		if c.token != "" {
			r.Header.Set("Authorization", "Bearer "+c.token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if reached != c.reached || !reached && w.Code != http.StatusForbidden {
			t.Fatalf("%q %s: reached %v, %d %s", c.token, c.path, reached, w.Code, w.Body)
		}
	}
}
