package alertrelay

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

type fakeNtfy struct {
	mu   sync.Mutex
	got  []Message
	fail bool
}

func (f *fakeNtfy) server(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/topic-x" {
			t.Errorf("published to %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.fail {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		p := 0
		if v := r.Header.Get("Priority"); v != "" {
			p = int(v[0] - '0')
		}
		f.got = append(f.got, Message{Title: r.Header.Get("Title"), Body: string(body), Priority: p})
	}))
	t.Cleanup(s.Close)
	return s
}

func (f *fakeNtfy) messages() []Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Message(nil), f.got...)
}

func setup(t *testing.T) (*Relay, *fakeNtfy, http.Handler, *time.Time) {
	t.Helper()
	f := &fakeNtfy{}
	srv := f.server(t)
	now := time.Date(2026, 10, 7, 3, 0, 0, 0, time.UTC)
	r := New(Config{Enabled: true, NtfyURL: srv.URL, Topic: "topic-x", Token: "tok", HeartbeatMax: 15 * time.Minute, RepeatEvery: time.Hour})
	r.now = func() time.Time { return now }
	r.started = now
	router := chi.NewRouter()
	r.Mount(router)
	return r, f, router, &now
}

func post(h http.Handler, path, token, body string) int {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	h.ServeHTTP(rec, req)
	return rec.Code
}

const twoAlerts = `{"status":"firing","alerts":[
 {"status":"firing","fingerprint":"aa","labels":{"alertname":"BifrostLogicalBackupMissing","severity":"critical","namespace":"data"},
  "annotations":{"summary":"No logical backup in 26h"},"startsAt":"2026-10-06T22:03:00Z"},
 {"status":"resolved","fingerprint":"bb","labels":{"alertname":"BifrostMinIONasDown","severity":"warning"},"annotations":{}}]}`

func TestAlertsNeedTheRelayToken(t *testing.T) {
	_, f, h, _ := setup(t)
	for _, tok := range []string{"", "wrong"} {
		if code := post(h, "/alerts/alertmanager", tok, twoAlerts); code != http.StatusUnauthorized {
			t.Fatalf("token %q: %d, want 401", tok, code)
		}
		if code := post(h, "/alerts/heartbeat", tok, `{}`); code != http.StatusUnauthorized {
			t.Fatalf("heartbeat token %q: %d, want 401", tok, code)
		}
	}
	if n := len(f.messages()); n != 0 {
		t.Fatalf("%d messages published without a token", n)
	}
}

func TestEachAlertIsPublishedOnceWithItsPriority(t *testing.T) {
	_, f, h, _ := setup(t)
	if code := post(h, "/alerts/alertmanager", "tok", twoAlerts); code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	// the same alert reaching the relay through a second route is not sent again
	if code := post(h, "/alerts/alertmanager", "tok", twoAlerts); code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	got := f.messages()
	if len(got) != 2 {
		t.Fatalf("published %d, want 2: %+v", len(got), got)
	}
	if got[0].Title != "CRITICAL BifrostLogicalBackupMissing · data" || got[0].Priority != 5 ||
		!strings.Contains(got[0].Body, "No logical backup in 26h") || !strings.Contains(got[0].Body, "since 2026-10-06 22:03Z") {
		t.Fatalf("firing message: %+v", got[0])
	}
	if got[1].Title != "RESOLVED BifrostMinIONasDown" || got[1].Priority != 2 {
		t.Fatalf("resolved message: %+v", got[1])
	}
}

func TestNtfyFailureMakesAlertmanagerRetry(t *testing.T) {
	_, f, h, _ := setup(t)
	f.fail = true
	if code := post(h, "/alerts/alertmanager", "tok", twoAlerts); code != http.StatusBadGateway {
		t.Fatalf("status %d, want 502 so Alertmanager retries", code)
	}
	f.fail = false
	if code := post(h, "/alerts/alertmanager", "tok", twoAlerts); code != http.StatusOK || len(f.messages()) != 2 {
		t.Fatalf("retry after a failure must publish (status %d, %d messages)", code, len(f.messages()))
	}
}

func TestMissingHeartbeatPagesThenRepeatsThenResolves(t *testing.T) {
	r, f, h, now := setup(t)
	ctx := context.Background()
	step := func(d time.Duration) { *now = now.Add(d); r.Check(ctx) }

	step(10 * time.Minute) // inside HeartbeatMax since start
	if len(f.messages()) != 0 {
		t.Fatal("paged before HeartbeatMax")
	}
	if post(h, "/alerts/heartbeat", "tok", `{}`) != http.StatusOK {
		t.Fatal("heartbeat refused")
	}
	step(14 * time.Minute)
	if len(f.messages()) != 0 {
		t.Fatal("paged while heartbeats arrive")
	}
	step(2 * time.Minute) // 16 min since the beat
	if got := f.messages(); len(got) != 1 || got[0].Priority != 5 || !strings.Contains(got[0].Title, "heartbeat missing") {
		t.Fatalf("want one urgent page, got %+v", got)
	}
	step(30 * time.Minute)
	if len(f.messages()) != 1 {
		t.Fatal("re-paged before RepeatEvery")
	}
	step(31 * time.Minute)
	if len(f.messages()) != 2 {
		t.Fatal("no repeat page after RepeatEvery")
	}
	if post(h, "/alerts/heartbeat", "tok", `{}`) != http.StatusOK {
		t.Fatal("heartbeat refused")
	}
	got := f.messages()
	if len(got) != 3 || !strings.HasPrefix(got[2].Title, "RESOLVED") {
		t.Fatalf("want a resolved message when the heartbeat returns, got %+v", got)
	}
	step(time.Minute)
	if len(f.messages()) != 3 {
		t.Fatal("paged right after the heartbeat came back")
	}
}

func TestNoHeartbeatSinceStartAlsoPages(t *testing.T) {
	r, f, _, now := setup(t)
	*now = now.Add(16 * time.Minute)
	r.Check(context.Background())
	if got := f.messages(); len(got) != 1 || !strings.Contains(got[0].Body, "never since the relay started") {
		t.Fatalf("a relay that never heard Alertmanager must page: %+v", got)
	}
}

func TestConfigProblem(t *testing.T) {
	if p := (Config{Enabled: true, Token: "x"}).Problem(); p == "" {
		t.Fatal("missing topic not reported")
	}
	if p := (Config{Enabled: true, Topic: "x"}).Problem(); p == "" {
		t.Fatal("missing token not reported")
	}
}
