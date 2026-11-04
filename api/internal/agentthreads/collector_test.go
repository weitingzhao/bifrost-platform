package agentthreads

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

// The collector of host h1 (R5:5.5-12 §1.2), and the callers it is held
// apart from.
var (
	collectorH1 = actuation.Principal{Name: "collector:h1", Role: actuation.RoleReporter}
	reporterP   = actuation.Principal{Name: "reporter", Role: actuation.RoleReporter}
	viewerP     = actuation.Principal{Name: "viewer", Role: actuation.RoleViewer}
)

// as serves one request on h as p, as the platform's gates hand it over.
func as(h *Handler, p actuation.Principal, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r = r.WithContext(actuation.WithPrincipal(r.Context(), p))
	w := httptest.NewRecorder()
	switch path {
	case "/api/v1/agent/threads/heartbeat":
		h.HandleBeat(w, r)
	case "/api/v1/agent/hosts/heartbeat":
		h.HandleHost(w, r)
	default:
		h.HandleList(w, r)
	}
	return w
}

func collectorHandler(t *testing.T, titles TitlesFunc) (*Handler, *Recorder, *memAudit) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.ExpectedHosts = []string{"h1", "h2", "h3"}
	rec := NewRecorder(NewStore(""), cfg)
	rec.now = func() time.Time { return t0 }
	audit := &memAudit{}
	h := NewHandler(rec, cfg, titles, audit)
	h.now = rec.now
	return h, rec, audit
}

const (
	uuid  = "0b8e2f4a-1111-2222-3333-444455556666"
	uuid2 = "7c1d9e3b-1111-2222-3333-444455556666"
)

func beatOn(vendor, host, extra string) string {
	return `{"thread":"` + uuid + `","vendor":"` + vendor + `","host":"` + host + `","event":"turn_start","turn_id":"turn-1","seq":1` + extra + `}`
}

// ForeignWork_OtherHostBeat (§1.5): a collector's thread and host beats
// name its own host or are refused 403 and audited, storing nothing; any
// other reporter names whichever host it reports for, as before.
func TestForeignWork_OtherHostBeat(t *testing.T) {
	h, rec, audit := collectorHandler(t, nil)
	for _, host := range []string{"h2", "H1", "h1.lan", "collector-h1"} {
		if w := as(h, collectorH1, "POST", "/api/v1/agent/threads/heartbeat", beatOn("claude", host, "")); w.Code != http.StatusForbidden {
			t.Fatalf("a thread beat for %s: %d %s", host, w.Code, w.Body)
		}
		if w := as(h, collectorH1, "POST", "/api/v1/agent/hosts/heartbeat", `{"host":"`+host+`","vendors":{}}`); w.Code != http.StatusForbidden {
			t.Fatalf("a host beat for %s: %d %s", host, w.Code, w.Body)
		}
	}
	st, err := rec.State()
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Threads) != 0 || len(st.Hosts) != 0 {
		t.Fatalf("a refused beat was stored: %+v", st)
	}
	if len(audit.rows) != 8 {
		t.Fatalf("each refusal is audited: %v", audit.rows)
	}
	for _, row := range audit.rows {
		if !strings.HasPrefix(row, "agent_thread.host_rejected collector:h1 denied") {
			t.Fatalf("audit row: %s", row)
		}
	}
	// Its own host (positive control), and a plain reporter for any host.
	if w := as(h, collectorH1, "POST", "/api/v1/agent/threads/heartbeat", beatOn("claude", "h1", "")); w.Code != http.StatusAccepted {
		t.Fatalf("its own host: %d %s", w.Code, w.Body)
	}
	if w := as(h, collectorH1, "POST", "/api/v1/agent/hosts/heartbeat", `{"host":"h1","vendors":{}}`); w.Code != http.StatusAccepted {
		t.Fatalf("its own host beat: %d %s", w.Code, w.Body)
	}
	if w := as(h, reporterP, "POST", "/api/v1/agent/threads/heartbeat", beatOn("codex", "h2", "")); w.Code != http.StatusAccepted {
		t.Fatalf("a reporter for h2: %d %s", w.Code, w.Body)
	}
	if w := as(h, reporterP, "POST", "/api/v1/agent/hosts/heartbeat", `{"host":"h2","vendors":{}}`); w.Code != http.StatusAccepted {
		t.Fatalf("a reporter's host beat for h2: %d %s", w.Code, w.Body)
	}
	// A principal in the namespace that is not a valid collector (no
	// configuration admits one) is bound to no host at all.
	bad := actuation.Principal{Name: "collector:h1", Role: actuation.RoleOperator}
	if w := as(h, bad, "POST", "/api/v1/agent/threads/heartbeat", beatOn("cursor", "h1", "")); w.Code != http.StatusForbidden {
		t.Fatalf("an invalid collector: %d %s", w.Code, w.Body)
	}
}

// CollectorTitleRefused, the heartbeat half (§1.5): a collector's beat that
// names a title, in any spelling encoding/json reads as one and with any
// value, is 400 and stores nothing, so no list shows it; a reporter's beat
// with a title is as before. (The title route itself is the route gate's:
// server.TestCollectorTitleRefused.)
func TestCollectorTitleRefused(t *testing.T) {
	h, rec, _ := collectorHandler(t, nil)
	for _, extra := range []string{`,"title":"collector title"`, `,"Title":"collector title"`, `,"TITLE":"x"`, `,"title":""`, `,"title":null`, `,"title":"   "`} {
		if w := as(h, collectorH1, "POST", "/api/v1/agent/threads/heartbeat", beatOn("claude", "h1", extra)); w.Code != http.StatusBadRequest {
			t.Fatalf("a collector's beat with %s: %d %s", extra, w.Code, w.Body)
		}
	}
	st, err := rec.State()
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Threads) != 0 {
		t.Fatalf("a refused beat was stored: %+v", st.Threads)
	}
	if w := as(h, viewerP, "GET", "/api/v1/agent/threads", ""); strings.Contains(w.Body.String(), "collector title") || strings.Contains(w.Body.String(), `"title"`) {
		t.Fatalf("the viewer's list has the refused title: %s", w.Body)
	}
	if w := as(h, reporterP, "POST", "/api/v1/agent/threads/heartbeat", beatOn("claude", "h1", `,"title":"reporter title"`)); w.Code != http.StatusAccepted {
		t.Fatalf("a reporter's beat with a title: %d %s", w.Code, w.Body)
	}
	if w := as(h, viewerP, "GET", "/api/v1/agent/threads", ""); !strings.Contains(w.Body.String(), `"title":"reporter title"`) {
		t.Fatalf("the reporter's title is listed as before: %s", w.Body)
	}
}

// CollectorThreadsNoTitle (§1.5, §1.3 row GET /agent/threads): a collector
// reads its own host only, its threads without a title (the title store is
// global, keyed by session alone), its own host's row and no refusal count;
// a row taken again after it left the list is still without one. A viewer's
// list is as before. (The ?sidebar= and ?session= filters are §3.3's.)
func TestCollectorThreadsNoTitle(t *testing.T) {
	titles := func(context.Context) (map[string]string, error) {
		return map[string]string{uuid: "store title of another host"}, nil
	}
	h, rec, _ := collectorHandler(t, titles)
	for _, step := range []struct {
		p          actuation.Principal
		path, body string
	}{
		{reporterP, "/api/v1/agent/threads/heartbeat", beatOn("claude", "h2", `,"title":"h2 beat title"`)},
		{collectorH1, "/api/v1/agent/threads/heartbeat", beatOn("codex", "h1", "")},
		// A reporter's beat on h1 stores its own title: the collector's
		// list leaves it out too.
		{reporterP, "/api/v1/agent/threads/heartbeat", strings.Replace(beatOn("cursor", "h1", `,"title":"h1 beat title"`), uuid, uuid2, 1)},
		{reporterP, "/api/v1/agent/hosts/heartbeat", `{"host":"h2","vendors":{}}`},
		{collectorH1, "/api/v1/agent/hosts/heartbeat", `{"host":"h1","vendors":{}}`},
	} {
		if w := as(h, step.p, "POST", step.path, step.body); w.Code != http.StatusAccepted {
			t.Fatalf("%s %s: %d %s", step.p.Name, step.body, w.Code, w.Body)
		}
	}
	if err := rec.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := rec.store.Update(func(st *State) error {
		st.ThreadsRefused, st.LastThreadRefusal = 3, t0
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	type row struct {
		Thread, Vendor, Host, Title string
	}
	type list struct {
		Threads []row `json:"threads"`
		Hosts   []struct {
			Host string `json:"host"`
		} `json:"hosts"`
	}
	own := func() (list, string) {
		t.Helper()
		w := as(h, collectorH1, "GET", "/api/v1/agent/threads", "")
		var l list
		if err := json.Unmarshal(w.Body.Bytes(), &l); w.Code != http.StatusOK || err != nil {
			t.Fatalf("the collector's list: %d %s", w.Code, w.Body)
		}
		return l, w.Body.String()
	}
	l, body := own()
	if len(l.Threads) != 2 || l.Threads[0].Host != "h1" || l.Threads[1].Host != "h1" || len(l.Hosts) != 1 || l.Hosts[0].Host != "h1" {
		t.Fatalf("the collector's list is not its host alone: %s", body)
	}
	for _, leak := range []string{`"title"`, "store title", "h1 beat title", "h2 beat title", `"h2"`, "h3", "threads_refused", "last_thread_refusal"} {
		if strings.Contains(body, leak) {
			t.Fatalf("the collector's list carries %s: %s", leak, body)
		}
	}
	// The row leaves the list and is taken again: still no title.
	if err := rec.store.Update(func(st *State) error { delete(st.Threads, Key("codex", uuid)); return nil }); err != nil {
		t.Fatal(err)
	}
	if w := as(h, collectorH1, "POST", "/api/v1/agent/threads/heartbeat", beatOn("codex", "h1", "")); w.Code != http.StatusAccepted {
		t.Fatalf("the row taken again: %d %s", w.Code, w.Body)
	}
	if l, body = own(); len(l.Threads) != 2 || strings.Contains(body, `"title"`) {
		t.Fatalf("after the row was taken again: %s", body)
	}
	// The viewer's list is unchanged: every host, every title, the count.
	w := as(h, viewerP, "GET", "/api/v1/agent/threads", "")
	var all list
	if err := json.Unmarshal(w.Body.Bytes(), &all); err != nil || len(all.Threads) != 3 || len(all.Hosts) != 3 {
		t.Fatalf("the viewer's list: %s", w.Body)
	}
	for _, r := range all.Threads {
		want := "store title of another host"
		if r.Thread == uuid2 {
			want = "h1 beat title"
		}
		if r.Title != want {
			t.Fatalf("the viewer's row %+v has not the title %q", r, want)
		}
	}
	if !strings.Contains(w.Body.String(), `"threads_refused":3`) {
		t.Fatalf("the viewer's list lost the refusal count: %s", w.Body)
	}
}

// A collector's beat never takes a thread another host holds (P-a review
// M1): not with that thread's own key, not a thread with no key yet (a
// legacy row, which migration would otherwise hand a key), not by replaying
// its registration nonce. The thread, its host and its work stay as they
// were, the refusal is audited and names no other host, and the collector's
// list does not show the thread. Its own host's threads, keyed or legacy,
// are its own; a plain reporter moves a thread between hosts as before; a
// collector's beat already buffered when the thread changed hands is dropped
// when it is folded in.
func TestCollectorCannotTakeAnotherHostsThread(t *testing.T) {
	const nonce = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	second := func(extra string) string {
		return strings.Replace(beatOn("claude", "h1", extra), `"seq":1`, `"seq":2`, 1)
	}
	for _, c := range []struct {
		name   string
		legacy bool
		extra  func(key string) string
	}{
		{"with its key", false, func(key string) string { return `,"thread_key":"` + key + `"` }},
		{"legacy, no key", true, func(string) string { return "" }},
		{"its registration nonce", false, func(string) string { return `,"register_nonce":"` + nonce + `"` }},
	} {
		t.Run(c.name, func(t *testing.T) {
			h, rec, audit := collectorHandler(t, nil)
			first := as(h, reporterP, "POST", "/api/v1/agent/threads/heartbeat", beatOn("claude", "h2", `,"work":"W-765","register_nonce":"`+nonce+`"`))
			var reply struct {
				Key string `json:"thread_key"`
			}
			if err := json.Unmarshal(first.Body.Bytes(), &reply); err != nil || reply.Key == "" {
				t.Fatalf("h2's thread: %d %s", first.Code, first.Body)
			}
			if c.legacy {
				if err := rec.store.Update(func(st *State) error {
					row := st.Threads[Key("claude", uuid)]
					row.KeyHash, row.RegisterNonce, row.RegisterKey = "", "", ""
					st.Threads[Key("claude", uuid)] = row
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			w := as(h, collectorH1, "POST", "/api/v1/agent/threads/heartbeat", second(c.extra(reply.Key)))
			if w.Code != http.StatusForbidden || strings.Contains(w.Body.String(), "h2") || strings.Contains(w.Body.String(), reply.Key) {
				t.Fatalf("the collector takes h2's thread: %d %s", w.Code, w.Body)
			}
			st, err := rec.State()
			if err != nil {
				t.Fatal(err)
			}
			if row := st.Threads[Key("claude", uuid)]; row.Host != "h2" || row.Work != "W-765" || row.Seq != 1 || row.HostBound {
				t.Fatalf("h2's thread changed: %+v", row)
			}
			if n := len(audit.rows); n != 1 || !strings.HasPrefix(audit.rows[0], "agent_thread.host_rejected collector:h1 denied") || strings.Contains(audit.rows[0], "h2") {
				t.Fatalf("audit: %v", audit.rows)
			}
			if l := as(h, collectorH1, "GET", "/api/v1/agent/threads", ""); strings.Contains(l.Body.String(), "W-765") || strings.Contains(l.Body.String(), uuid) {
				t.Fatalf("the collector's list shows h2's thread: %s", l.Body)
			}
			// A plain reporter holding the key still moves it, as before.
			if !c.legacy && c.name == "with its key" {
				if w := as(h, reporterP, "POST", "/api/v1/agent/threads/heartbeat", second(c.extra(reply.Key))); w.Code != http.StatusAccepted {
					t.Fatalf("a reporter with the key: %d %s", w.Code, w.Body)
				}
				if st, _ := rec.State(); st.Threads[Key("claude", uuid)].Host != "h1" {
					t.Fatalf("the reporter's move: %+v", st.Threads[Key("claude", uuid)])
				}
			}
		})
	}

	t.Run("its own host's threads", func(t *testing.T) {
		h, rec, _ := collectorHandler(t, nil)
		first := as(h, collectorH1, "POST", "/api/v1/agent/threads/heartbeat", beatOn("claude", "h1", ""))
		var reply struct {
			Key string `json:"thread_key"`
		}
		if err := json.Unmarshal(first.Body.Bytes(), &reply); err != nil || reply.Key == "" {
			t.Fatalf("its own thread: %d %s", first.Code, first.Body)
		}
		if w := as(h, collectorH1, "POST", "/api/v1/agent/threads/heartbeat", second(`,"thread_key":"`+reply.Key+`"`)); w.Code != http.StatusAccepted {
			t.Fatalf("its own keyed thread: %d %s", w.Code, w.Body)
		}
		// A legacy row of its own host is migrated to a key, as for anyone.
		as(h, reporterP, "POST", "/api/v1/agent/threads/heartbeat", strings.Replace(beatOn("cursor", "h1", ""), uuid, uuid2, 1))
		if err := rec.store.Update(func(st *State) error {
			row := st.Threads[Key("cursor", uuid2)]
			row.KeyHash = ""
			st.Threads[Key("cursor", uuid2)] = row
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		w := as(h, collectorH1, "POST", "/api/v1/agent/threads/heartbeat", strings.Replace(strings.Replace(beatOn("cursor", "h1", ""), uuid, uuid2, 1), `"seq":1`, `"seq":2`, 1))
		if w.Code != http.StatusAccepted || !strings.Contains(w.Body.String(), "thread_key") {
			t.Fatalf("its own legacy thread: %d %s", w.Code, w.Body)
		}
		st, _ := rec.State()
		if row := st.Threads[Key("cursor", uuid2)]; row.Host != "h1" || row.Seq != 2 || row.KeyHash == "" || !row.HostBound {
			t.Fatalf("its own legacy thread after: %+v", row)
		}
	})

	// The update itself checks again: the shared statefile may hold another
	// host's thread by the time admitSync writes, though Admit's snapshot
	// had none (another writer of the state). Each sync path is refused.
	t.Run("the update after another writer", func(t *testing.T) {
		for _, c := range []struct {
			name, keyHash, nonce string
		}{{"legacy, no key", "", ""}, {"its registration nonce", HashKey(strings.Repeat("cd", 32)), nonce}} {
			rec := NewRecorder(NewStore(""), DefaultConfig())
			rec.now = func() time.Time { return t0 }
			if err := rec.store.Update(func(st *State) error {
				st.Threads[Key("claude", uuid)] = Thread{Thread: uuid, Vendor: "claude", Host: "h2", Work: "W-765", Event: TurnStart,
					TurnID: "turn-1", Seq: 1, At: t0, KeyHash: c.keyHash, RegisterNonce: c.nonce, RegisterKey: strings.Repeat("cd", 32), RegisteredAt: t0}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			b := Beat{Thread: uuid, Vendor: "claude", Host: "h1", Event: TurnStart, TurnID: "turn-1", Seq: 2, RegisterNonce: c.nonce, boundHost: "h1"}
			d, err := rec.admitSync(b, t0)
			st, _ := rec.State()
			if !errors.Is(err, ErrOtherHost) || d.Key != "" || st.Threads[Key("claude", uuid)].Host != "h2" {
				t.Fatalf("%s: %+v %v, thread %+v", c.name, d, err, st.Threads[Key("claude", uuid)])
			}
		}
	})

	t.Run("a buffered beat after the thread changed hands", func(t *testing.T) {
		st := State{Threads: map[string]Thread{Key("claude", uuid): {Thread: uuid, Vendor: "claude", Host: "h2", Work: "W-765", Event: TurnStart, TurnID: "turn-1", Seq: 1, At: t0}}}
		b := Beat{Thread: uuid, Vendor: "claude", Host: "h1", Event: AfterTool, TurnID: "turn-1", Seq: 2, boundHost: "h1"}
		if st.Apply(b, t0.Add(time.Second)) || st.Threads[Key("claude", uuid)].Host != "h2" {
			t.Fatalf("a host-bound beat was folded into h2's thread: %+v", st.Threads[Key("claude", uuid)])
		}
		b.boundHost = ""
		if !st.Apply(b, t0.Add(time.Second)) || st.Threads[Key("claude", uuid)].Host != "h1" {
			t.Fatal("an unbound beat is folded in as before")
		}
	})
}

// A collector's session supersedes sessions of its own host only (P-a
// review self-check): a sidebar id is global, so without this a collector
// naming another host's sidebar made that host's quiet session superseded,
// and its silence was never paged. On its own host it supersedes as before;
// a plain reporter's session supersedes across hosts as before.
func TestCollectorSupersedesItsOwnHostOnly(t *testing.T) {
	clk := &clock{at: t0}
	cfg := DefaultConfig()
	rec := NewRecorder(NewStore(""), cfg)
	rec.now = clk.now
	h := NewHandler(rec, cfg, nil, nil)
	h.now = clk.now
	beat := func(p actuation.Principal, thread, host, sidebar string) {
		t.Helper()
		body := `{"thread":"` + thread + `","vendor":"claude","host":"` + host + `","event":"turn_start","turn_id":"turn-` + thread + `","seq":1,"sidebar":"` + sidebar + `"}`
		if w := as(h, p, "POST", "/api/v1/agent/threads/heartbeat", body); w.Code != http.StatusAccepted {
			t.Fatalf("%s %s: %d %s", p.Name, thread, w.Code, w.Body)
		}
	}
	const otherSidebar, ownSidebar = "local_aaaaaaaa-1111-2222-3333-444455556666", "local_bbbbbbbb-1111-2222-3333-444455556666"
	beat(reporterP, "a-on-h2", "h2", otherSidebar)
	beat(reporterP, "a-on-h1", "h1", ownSidebar)
	clk.at = t0.Add(15 * time.Minute)
	beat(collectorH1, "b-on-h1", "h1", otherSidebar)
	beat(collectorH1, "b2-on-h1", "h1", ownSidebar)
	st, err := rec.State()
	if err != nil {
		t.Fatal(err)
	}
	status := func(st State, thread string) Status {
		return cfg.sessionStatus(st, st.Threads[Key("claude", thread)], clk.at)
	}
	if got := status(st, "a-on-h2"); got != Silent {
		t.Fatalf("h2's quiet session under the collector's shared sidebar: %s, want silent", got)
	}
	if push, _ := cfg.Due(st, clk.at); !slices.Contains(push, Key("claude", "a-on-h2")) {
		t.Fatalf("h2's silence is not paged: %v", push)
	}
	if got := status(st, "a-on-h1"); got != Superseded {
		t.Fatalf("its own host's session: %s, want superseded", got)
	}
	clk.at = clk.at.Add(time.Minute)
	beat(reporterP, "c-on-h3", "h3", otherSidebar)
	if st, _ = rec.State(); status(st, "a-on-h2") != Superseded {
		t.Fatalf("a plain reporter's session on h3 supersedes h2's as before: %s", status(st, "a-on-h2"))
	}
}
