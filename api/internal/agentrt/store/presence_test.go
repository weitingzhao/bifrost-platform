package store_test

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// presenceWorld is a world with the hosts named, none polled yet, and claude
// and codex candidates of every job (tenant dev).
func presenceWorld(t *testing.T, hosts ...string) *world {
	t.Helper()
	w := newWorld(t)
	for _, h := range hosts {
		ok(t, store.RegisterHost(ctx, w.rt, "owner", h, "dev"))
	}
	_, err := w.rt.Exec(ctx, agentrttest.DispatchRules)
	ok(t, err)
	w.jobs(t, "active", "*", codex, claude)
	return w
}

// poll is a claim request of host offering tools: what it offers is recorded
// and its poll counted, and it takes nothing (no free slot).
func (w *world) poll(t *testing.T, host string, tools ...store.Tool) time.Time {
	t.Helper()
	_, err := store.RecordHostOffered(ctx, w.rt, host, tools)
	ok(t, err)
	_, err = store.ClaimNext(ctx, w.rt, store.ClaimRequest{HostID: host, FreeSlots: 0, Tools: tools})
	ok(t, err)
	var at time.Time
	ok(t, w.rt.QueryRow(ctx, `SELECT last_poll_at FROM agentrt.host WHERE host_id = $1`, host).Scan(&at))
	return at
}

func presenceAt(t *testing.T, w *world, now time.Time) int {
	t.Helper()
	n, err := store.Presence(ctx, w.rt, now)
	ok(t, err)
	return n
}

// presenceAlerts counts the controller's alerts of rule.
func (w *world) presenceAlerts(t *testing.T, rule string) int {
	t.Helper()
	var n int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = 'policy_alert' AND actor_id = 'controller'
	  AND body_json->>'rule' = $1`, rule).Scan(&n))
	return n
}

// presenceDoc is the part of Progress presence adds.
type presenceDoc struct {
	Hosts []struct {
		HostID          string        `json:"host_id"`
		Online          bool          `json:"online"`
		ReportedOffline bool          `json:"reported_offline"`
		DiskLow         bool          `json:"disk_low"`
		Offered         []store.Offer `json:"offered"`
		OfferedAt       *string       `json:"offered_at"`
	} `json:"hosts"`
	Executors []store.ExecutorPresence `json:"executors"`
	Unserved  []string                 `json:"jobs_without_executor"`
}

func (w *world) presence(t *testing.T) (presenceDoc, []byte) {
	t.Helper()
	out, err := store.Progress(ctx, w.rt)
	ok(t, err)
	raw, err := json.MarshalIndent(out, "", "  ")
	ok(t, err)
	var doc presenceDoc
	ok(t, json.Unmarshal(raw, &doc))
	return doc, raw
}

func (d presenceDoc) executor(t *testing.T, id string) store.ExecutorPresence {
	t.Helper()
	for _, e := range d.Executors {
		if e.Executor == id {
			return e
		}
	}
	t.Fatalf("no executor %s in %+v", id, d.Executors)
	return store.ExecutorPresence{}
}

func isBool(b *bool, want bool) bool { return b != nil && *b == want }

// W-31 3.5 (the Owner: 「主机掉线或执行者不可用，Console 和参谋长简报 1 分钟内说
// 出来」): a host that stops polling is recorded and told the first pass past
// 30 seconds after its last poll, never before; with it the executors only it
// offered, and the jobs then left with no executor (the chief of staff's
// among them). Each turn is recorded and told once, whatever the passes after
// it and a pass after a restart (which reads the records); its polling again
// is told once as well.
func TestAHostThatStopsIsToldOnceAfterThirtySeconds(t *testing.T) {
	t.Parallel()
	w := presenceWorld(t, "h1")
	last := w.poll(t, "h1", claude, codex)
	for s := 0; s <= 30; s += 5 {
		if n := presenceAt(t, w, last.Add(time.Duration(s)*time.Second)); n != 0 {
			t.Fatalf("%d turns %d s after the last poll", n, s)
		}
	}
	if n := presenceAt(t, w, last.Add(store.HostLostAfter)); n != 0 || len(platformNotes(t, w)) != 0 {
		t.Fatalf("%d turns at exactly the threshold", n)
	}
	if n := presenceAt(t, w, last.Add(store.HostLostAfter+time.Millisecond)); n != 3 {
		t.Fatalf("%d turns just past the threshold; want h1 and its two executors", n)
	}
	notes := platformNotes(t, w)
	if len(notes) != 1 || w.presenceAlerts(t, "host_offline") != 1 || w.presenceAlerts(t, "executor_unavailable") != 2 {
		t.Fatalf("notes %d, host alerts %d, executor alerts %d", len(notes), w.presenceAlerts(t, "host_offline"),
			w.presenceAlerts(t, "executor_unavailable"))
	}
	for _, want := range []string{"From the platform, not the chief of staff", "Host h1 is offline: its last poll was at",
		"exec:claude is unavailable: host h1 offers it but has not polled since", "exec:codex is unavailable",
		"chief_of_staff (so the chief of staff cannot answer you until one can)", "implementer"} {
		if !strings.Contains(notes[0].Body, want) {
			t.Fatalf("the note lacks %q:\n%s", want, notes[0].Body)
		}
	}
	for s := 35; s <= 60; s += 5 {
		if n := presenceAt(t, w, last.Add(time.Duration(s)*time.Second)); n != 0 {
			t.Fatalf("told again %d s after the last poll: %d", s, n)
		}
	}
	back := w.poll(t, "h1", claude, codex)
	if n := presenceAt(t, w, back.Add(time.Second)); n != 3 {
		t.Fatalf("%d turns once h1 polls again", n)
	}
	notes = platformNotes(t, w)
	if len(notes) != 2 || !strings.Contains(notes[1].Body, "Host h1 is back online: it polled at") ||
		!strings.Contains(notes[1].Body, "exec:claude is available again: offered by h1") ||
		!strings.Contains(notes[1].Body, "Every job has an executor that can take it again.") {
		t.Fatalf("notes %+v", notes)
	}
	if n := presenceAt(t, w, back.Add(2*time.Second)); n != 0 || len(platformNotes(t, w)) != 2 {
		t.Fatalf("told again: %d", n)
	}
	var hostNotes int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id = 'host:h1' AND event_type = 'note'
	  AND starts_with(body_json->>'text', 'h1 is back online')`).Scan(&hostNotes))
	if hostNotes != 1 {
		t.Fatalf("%d back-online notes", hostNotes)
	}
}

// What a host offers is recorded once a change: its first claim, and a claim
// that offers something else; the order of its preference is no change.
func TestAHostsOffersAreRecordedOnceAChange(t *testing.T) {
	t.Parallel()
	w := presenceWorld(t, "h1")
	for i, step := range []struct {
		tools []store.Tool
		want  bool
	}{{[]store.Tool{claude, codex}, true}, {[]store.Tool{codex, claude}, false}, {[]store.Tool{claude}, true},
		{[]store.Tool{claude}, false}, {nil, true}, {nil, false}} {
		got, err := store.RecordHostOffered(ctx, w.rt, "h1", step.tools)
		ok(t, err)
		if got != step.want {
			t.Fatalf("step %d: recorded %v", i, got)
		}
	}
	doc, _ := w.presence(t)
	if len(doc.Hosts) != 1 || doc.Hosts[0].Offered == nil || len(doc.Hosts[0].Offered) != 0 || doc.Hosts[0].OfferedAt == nil {
		t.Fatalf("hosts %+v", doc.Hosts)
	}
}

// Acceptance 2a of the cut: each way an executor becomes unavailable (no host
// offers it, the hosts that offer it are offline, its account cools down, its
// pool is at the routing rule's quota stop) is recorded once, told once, and
// said by GET /progress; and its coming back is too.
func TestEachWayAnExecutorIsUnavailableIsToldOnce(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, why string
		down      func(t *testing.T, w *world)
		up        func(t *testing.T, w *world)
	}{
		{name: "not offered", why: "no host offers it",
			down: func(t *testing.T, w *world) { w.poll(t, "h1", claude) },
			up:   func(t *testing.T, w *world) { w.poll(t, "h1", claude, codex) }},
		{name: "its hosts offline", why: "host h2 offers it but has not polled since",
			down: func(t *testing.T, w *world) {
				w.legacy(t, `UPDATE agentrt.host SET last_poll_at = clock_timestamp() - interval '31 seconds' WHERE host_id = 'h2'`)
			},
			up: func(t *testing.T, w *world) { w.poll(t, "h2", codex) }},
		{name: "cooling", why: "the codex account's pools cool down until",
			down: func(t *testing.T, w *world) { w.reading(t, "codex:owner:weekly", 1, time.Hour) },
			up:   func(t *testing.T, w *world) { w.reading(t, "codex:owner:weekly", 0.5, time.Hour) }},
		{name: "quota stop", why: "its pool reads 95% used, at or past the dev tenant's quota stop of 90%",
			down: func(t *testing.T, w *world) {
				w.legacy(t, routingRule(1, -1, 0))
				w.reading(t, "codex:owner:weekly", 0.95, time.Hour)
			},
			up: func(t *testing.T, w *world) { w.reading(t, "codex:owner:weekly", 0.5, time.Hour) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			w := presenceWorld(t, "h1", "h2")
			w.poll(t, "h1", claude)
			if c.name == "not offered" {
				w.legacy(t, `UPDATE agentrt.host SET state = 'draining' WHERE host_id = 'h2'`)
			} else {
				w.poll(t, "h2", codex)
			}
			if n := presenceAt(t, w, time.Time{}); n != 0 && c.name != "not offered" {
				t.Fatalf("%d turns before anything changed", n)
			}
			c.down(t, w)
			presenceAt(t, w, time.Time{})
			presenceAt(t, w, time.Time{})
			if n := w.presenceAlerts(t, "executor_unavailable"); n != 1 {
				t.Fatalf("%d executor alerts", n)
			}
			notes := platformNotes(t, w)
			if len(notes) != 1 || !strings.Contains(notes[0].Body, "exec:codex is unavailable: "+c.why) ||
				strings.Contains(notes[0].Body, "exec:claude is unavailable") || strings.Contains(notes[0].Body, "No executor can take") {
				t.Fatalf("notes %+v", notes)
			}
			doc, _ := w.presence(t)
			if e := doc.executor(t, "exec:codex"); !isBool(e.Available, false) || !strings.Contains(e.Why, c.why) || len(doc.Unserved) != 0 {
				t.Fatalf("codex %+v; jobs without an executor %v", e, doc.Unserved)
			}
			if e := doc.executor(t, "exec:claude"); !isBool(e.Available, true) || e.Why != "offered by h1" || len(e.Jobs) != 5 {
				t.Fatalf("claude %+v", e)
			}
			c.up(t, w)
			presenceAt(t, w, time.Time{})
			presenceAt(t, w, time.Time{})
			if notes = platformNotes(t, w); len(notes) != 2 || !strings.Contains(notes[1].Body, "exec:codex is available again: offered by") {
				t.Fatalf("notes %+v", notes)
			}
			if doc, _ = w.presence(t); !isBool(doc.executor(t, "exec:codex").Available, true) {
				t.Fatalf("codex %+v", doc.executor(t, "exec:codex"))
			}
		})
	}
}

// What a host that could take work offers is not known until its claim says
// so (a platform just upgraded): its executors are then not known, and no
// turn is recorded for them; a host held by a drain or a stop, or retired,
// is not reported offline.
func TestPresenceSaysNothingItDoesNotKnow(t *testing.T) {
	t.Parallel()
	w := presenceWorld(t, "h1", "h2", "h3")
	_, err := store.ClaimNext(ctx, w.rt, store.ClaimRequest{HostID: "h1", FreeSlots: 0})
	ok(t, err)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		return store.SetControl(ctx, tx, "owner", store.NewControlID(), "host", "h2", "drain", "test: maintenance")
	}))
	w.legacy(t, `UPDATE agentrt.host SET state = 'retired' WHERE host_id = 'h3'`)
	doc, _ := w.presence(t)
	for _, e := range doc.Executors {
		if e.Available != nil || !strings.Contains(e.Why, "what h1 offers is not recorded yet") {
			t.Fatalf("executor %+v", e)
		}
	}
	for _, h := range doc.Hosts {
		if h.ReportedOffline {
			t.Fatalf("host %+v", h)
		}
	}
	if n := presenceAt(t, w, time.Time{}); n != 0 || len(platformNotes(t, w)) != 0 {
		t.Fatalf("%d turns", n)
	}
}

// Two passes at once (two platform replicas) record a turn once: the seat's
// lock orders them, and the second reads the first's records.
func TestTwoPassesAtOnceRecordATurnOnce(t *testing.T) {
	t.Parallel()
	w := presenceWorld(t, "h1")
	last := w.poll(t, "h1", claude, codex)
	pool := w.db.Pool(t, w.db.Runtime)
	var wg sync.WaitGroup
	counts := make([]int, 4)
	for i := range counts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, err := store.Presence(ctx, pool, last.Add(time.Minute))
			if err != nil {
				t.Error(err)
			}
			counts[i] = n
		}()
	}
	wg.Wait()
	if total := counts[0] + counts[1] + counts[2] + counts[3]; total != 3 || len(platformNotes(t, w)) != 1 {
		t.Fatalf("turns %v, %d notes", counts, len(platformNotes(t, w)))
	}
}

// The Console's fixture of presence (console/src/api/__fixtures__/agentrt-progress-presence.testdb.json):
// GET /progress after a presence pass with h1 online offering claude, h2 offering codex and stopped 45
// seconds ago, and claude's account cooling down for an hour: h2 is reported offline, both executors are
// unavailable (each for its reason) and every job is left with no executor. Written when
// CONSOLE_PRESENCE_FIXTURE_WRITE=1 (only when every assertion held); the Console's tests read it.
const consolePresenceFixture = "../../../../console/src/api/__fixtures__/agentrt-progress-presence.testdb.json"

func TestConsolePresenceFixture(t *testing.T) {
	t.Parallel()
	w := presenceWorld(t, "h1", "h2")
	w.poll(t, "h1", claude)
	w.poll(t, "h2", codex)
	w.legacy(t, `UPDATE agentrt.host SET last_poll_at = clock_timestamp() - interval '45 seconds' WHERE host_id = 'h2'`)
	w.reading(t, "claude:owner:weekly", 1, time.Hour)
	if n := presenceAt(t, w, time.Time{}); n != 3 {
		t.Fatalf("%d turns", n)
	}
	doc, raw := w.presence(t)
	byID := map[string]int{}
	for i, h := range doc.Hosts {
		byID[h.HostID] = i
	}
	h1, h2 := doc.Hosts[byID["h1"]], doc.Hosts[byID["h2"]]
	if !h1.Online || h1.ReportedOffline || len(h1.Offered) != 1 || h1.Offered[0].Executor != "exec:claude" ||
		h2.Online || !h2.ReportedOffline || len(h2.Offered) != 1 || h2.Offered[0].Executor != "exec:codex" {
		t.Fatalf("hosts %+v", doc.Hosts)
	}
	if e := doc.executor(t, "exec:claude"); !isBool(e.Available, false) || !strings.HasPrefix(e.Why, "the claude account's pools cool down until") {
		t.Fatalf("claude %+v", e)
	}
	if e := doc.executor(t, "exec:codex"); !isBool(e.Available, false) || !strings.HasPrefix(e.Why, "host h2 offers it but has not polled since") {
		t.Fatalf("codex %+v", e)
	}
	if strings.Join(doc.Unserved, ",") != "chief_of_staff,implementer,planner,reviewer,verifier" {
		t.Fatalf("jobs without an executor %v", doc.Unserved)
	}
	if os.Getenv("CONSOLE_PRESENCE_FIXTURE_WRITE") == "1" && !t.Failed() {
		ok(t, os.WriteFile(consolePresenceFixture, append(raw, '\n'), 0o644))
	}
}
