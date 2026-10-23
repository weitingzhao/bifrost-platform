package store_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentd"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// PruneSilence is agentd's prune budget and 15 seconds: a change of agentd's
// budget fails here until the platform's follows.
func TestPruneSilenceCoversAgentdsBudget(t *testing.T) {
	t.Parallel()
	if store.PruneSilence != agentd.PruneWork+agentd.PruneReport+15*time.Second {
		t.Fatalf("PruneSilence %v; agentd's prune budget is %v + %v", store.PruneSilence, agentd.PruneWork, agentd.PruneReport)
	}
}

// pollAt sets host's last contact to at, as a claim or a call of its agentd
// at that moment would.
func (w *world) pollAt(t *testing.T, host string, at time.Time) {
	t.Helper()
	_, err := w.rt.Exec(ctx, `UPDATE agentrt.host SET last_poll_at = $2 WHERE host_id = $1`, host, at)
	ok(t, err)
}

// presenceDocAt is Progress at at, a host's silence counted from no earlier
// than since, and the status of the live attempt of task.
func (w *world) presenceDocAt(t *testing.T, at, since time.Time, task string) (presenceDoc, string) {
	t.Helper()
	out, err := store.ProgressAtForTest(ctx, w.rt, at, since)
	ok(t, err)
	raw, err := json.Marshal(out)
	ok(t, err)
	var doc presenceDoc
	ok(t, json.Unmarshal(raw, &doc))
	var live struct {
		Live []struct {
			TaskID string `json:"task_id"`
			Status string `json:"status"`
		} `json:"live"`
	}
	ok(t, json.Unmarshal(raw, &live))
	for _, a := range live.Live {
		if a.TaskID == task {
			return doc, a.Status
		}
	}
	return doc, ""
}

func (d presenceDoc) host(t *testing.T, id string) (online, reported, pruning bool) {
	t.Helper()
	for _, h := range d.Hosts {
		if h.HostID == id {
			return h.Online, h.ReportedOffline, h.Pruning
		}
	}
	t.Fatalf("no host %s", id)
	return
}

// Review 526 A1: agentd deletes the attempt branches it was handed between
// two claims, for up to its prune budget, while the host is alive. Handed
// branches, a host silent for two minutes is online (pruning), its executors
// available, its running attempt not host_lost, and nothing is told; past
// PruneSilence with no report it is lost: offline, its executors
// unavailable, its attempt host_lost. A report (or a claim) closes the
// round: the host is then lost 30 seconds after its last contact.
func TestAPruningHostIsNotLost(t *testing.T) {
	t.Parallel()
	w := presenceWorld(t, "h1")
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-1", "active", "test") }))
	task := w.ready(t, store.Task{Effect: "none"})
	w.poll(t, "h1", claude, codex)
	c := w.claim(t, "h1", time.Hour, codex)
	if c == nil || c.TaskID != task {
		t.Fatalf("claim %+v", c)
	}
	ok(t, store.Start(ctx, w.rt, fence(c, "h1"), "", ""))
	ok(t, store.OpenPruneRound(ctx, w.rt, "h1", 3))
	var opened time.Time
	ok(t, w.rt.QueryRow(ctx, `SELECT at FROM agentrt.event WHERE event_stream_id = $1`, store.HostPruneStream("h1")).Scan(&opened))
	w.pollAt(t, "h1", opened)
	at := opened.Add(2 * time.Minute)
	doc, status := w.presenceDocAt(t, at, time.Time{}, task)
	if online, reported, pruning := doc.host(t, "h1"); !online || reported || !pruning || status == store.StatusHostLost ||
		!isBool(doc.executor(t, "exec:claude").Available, true) {
		t.Fatalf("2 min into a prune round: online %v reported %v pruning %v, attempt %s, claude %+v", online, reported, pruning, status,
			doc.executor(t, "exec:claude"))
	}
	if n := presenceAt(t, w, at); n != 0 || len(platformNotes(t, w)) != 0 {
		t.Fatalf("%d turns 2 min into a prune round", n)
	}
	past := opened.Add(store.PruneSilence + time.Second)
	doc, status = w.presenceDocAt(t, past, time.Time{}, task)
	if online, reported, _ := doc.host(t, "h1"); online || !reported || status != store.StatusHostLost ||
		!isBool(doc.executor(t, "exec:claude").Available, false) {
		t.Fatalf("past the prune budget: online %v reported %v, attempt %s", online, reported, status)
	}
	if n := presenceAt(t, w, past); n != 3 {
		t.Fatalf("%d turns past the prune budget; want h1 and its two executors", n)
	}
	// A new round, closed by the report: from then on the usual 30 seconds.
	ok(t, store.OpenPruneRound(ctx, w.rt, "h1", 1))
	ok(t, store.ClosePruneRound(ctx, w.rt, "h1", "it reported what it deleted"))
	ok(t, store.ClosePruneRound(ctx, w.rt, "h1", "it claims again"))
	if n := w.lastSeq(t, store.HostPruneStream("h1")); n != 3 {
		t.Fatalf("%d records of h1's prune rounds; want 3 (a round closes once)", n)
	}
	var closed time.Time
	ok(t, w.rt.QueryRow(ctx, `SELECT at FROM agentrt.event WHERE event_stream_id = $1 AND seq = 3`, store.HostPruneStream("h1")).Scan(&closed))
	w.pollAt(t, "h1", closed)
	if doc, _ = w.presenceDocAt(t, closed.Add(31*time.Second), time.Time{}, task); func() bool { _, r, p := doc.host(t, "h1"); return !r || p }() {
		t.Fatalf("31 s after a closed round: %+v", doc.Hosts)
	}
}

// Review 526 A1b: a release of the platform stops it for tens of seconds,
// and every host's last poll is then older than 30 seconds. A host's silence
// is counted from no earlier than the reader's start: h1 and h2 last polled
// 90 seconds before now, the platform came back 25 seconds before now; neither
// is reported, nor its executors (not known yet). h1 polls 3 seconds after
// now and is never reported; h2 does not, and is reported once 30 seconds
// have passed since the platform came back, not before.
func TestARestartedPlatformWaitsForTheHosts(t *testing.T) {
	t.Parallel()
	w := presenceWorld(t, "h1", "h2")
	w.poll(t, "h1", claude)
	w.poll(t, "h2", codex)
	var now time.Time
	ok(t, w.rt.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now))
	w.pollAt(t, "h1", now.Add(-90*time.Second))
	w.pollAt(t, "h2", now.Add(-90*time.Second))
	back := now.Add(-25 * time.Second)
	at := func(d time.Duration) int {
		t.Helper()
		n, err := store.Presence(ctx, w.rt, now.Add(d), back)
		ok(t, err)
		return n
	}
	doc, _ := w.presenceDocAt(t, now, back, "")
	for _, id := range []string{"h1", "h2"} {
		if online, reported, _ := doc.host(t, id); online || reported {
			t.Fatalf("%s just after the platform came back: online %v reported %v", id, online, reported)
		}
	}
	for _, e := range doc.Executors {
		if e.Available != nil {
			t.Fatalf("executor %+v while its hosts are not heard from since the platform came back", e)
		}
	}
	if n := at(0); n != 0 {
		t.Fatalf("%d turns 25 s after the platform came back", n)
	}
	w.pollAt(t, "h1", now.Add(3*time.Second))
	if n := at(5 * time.Second); n != 0 {
		t.Fatalf("%d turns 30 s after the platform came back", n)
	}
	if n := at(5*time.Second + time.Millisecond); n != 2 {
		t.Fatalf("%d turns just past 30 s after the platform came back; want h2 and codex", n)
	}
	notes := platformNotes(t, w)
	if len(notes) != 1 || !containsAll(notes[0].Body, "Host h2 is offline", "exec:codex is unavailable") || containsAll(notes[0].Body, "Host h1") {
		t.Fatalf("notes %+v", notes)
	}
	// Without the platform's start, the same moment would have told both.
	if doc, _ = w.presenceDocAt(t, now, time.Time{}, ""); func() bool { _, r, _ := doc.host(t, "h2"); return !r }() {
		t.Fatalf("h2 90 s silent with no start given: %+v", doc.Hosts)
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}

// splitWorld is a world where the planner's one candidate is claude and the
// implementer's codex, with hosts h1 and h2.
func splitWorld(t *testing.T) *world {
	t.Helper()
	w := newWorld(t)
	for _, h := range []string{"h1", "h2"} {
		ok(t, store.RegisterHost(ctx, w.rt, "owner", h, "dev"))
	}
	_, err := w.rt.Exec(ctx, agentrttest.DispatchRules)
	ok(t, err)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if err := store.LockCapabilities(ctx, tx); err != nil {
			return err
		}
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test: one candidate a job"}}); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, agentrttest.TestJobRule); err != nil {
			return err
		}
		for job, tl := range map[string]store.Tool{"planner": claude, "implementer": codex} {
			if _, err := tx.Exec(ctx, `INSERT INTO agentrt.job (job_id, tenant, rule_id, rule_version) VALUES ($1, 'dev', 'job:test', 1)`, job); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO agentrt.job_candidate (job_candidate_id, job_id, executor_actor_id, model_provider,
			  model_pattern, status, rule_id, rule_version) VALUES ($1, $1, $2, $3, '*', 'active', 'job:test', 1)`, job, tl.Executor, tl.Provider); err != nil {
				return err
			}
		}
		return nil
	}))
	return w
}

// Review 526 A2: one executor available again while another job's is not
// known says that one back, and that job not known, never that every job
// has an executor again. Once both are known available it does.
func TestAnExecutorBackIsNotEveryJobCovered(t *testing.T) {
	t.Parallel()
	w := splitWorld(t)
	w.poll(t, "h1", claude)
	w.poll(t, "h2", codex)
	var p time.Time
	ok(t, w.rt.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&p))
	w.pollAt(t, "h1", p)
	w.pollAt(t, "h2", p.Add(35*time.Second))
	if n, err := store.Presence(ctx, w.rt, p.Add(40*time.Second), time.Time{}); err != nil || n != 2 {
		t.Fatalf("h1 lost: %d turns (%v)", n, err)
	}
	// The platform comes back at p+90 s: h1 polls at p+100 s, h2 has not been
	// heard from since p+35 s, which is not yet lost (counted from p+90 s).
	w.pollAt(t, "h1", p.Add(100*time.Second))
	if n, err := store.Presence(ctx, w.rt, p.Add(101*time.Second), p.Add(90*time.Second)); err != nil || n != 2 {
		t.Fatalf("h1 back: %d turns (%v)", n, err)
	}
	notes := platformNotes(t, w)
	if len(notes) != 2 || !containsAll(notes[1].Body, "exec:claude is available again", "Not known yet whether an executor can take these jobs: implementer") ||
		containsAll(notes[1].Body, "Every job has an executor") {
		t.Fatalf("notes %+v", notes)
	}
	// Both known: a later turn may say every job is covered.
	w.pollAt(t, "h2", p.Add(110*time.Second))
	w.pollAt(t, "h1", p.Add(80*time.Second))
	if n, err := store.Presence(ctx, w.rt, p.Add(115*time.Second), time.Time{}); err != nil || n != 2 {
		t.Fatalf("h1 lost again: %d turns (%v)", n, err)
	}
	w.pollAt(t, "h1", p.Add(116*time.Second))
	if n, err := store.Presence(ctx, w.rt, p.Add(117*time.Second), time.Time{}); err != nil || n != 2 {
		t.Fatalf("h1 back again: %d turns (%v)", n, err)
	}
	if notes = platformNotes(t, w); len(notes) != 4 || !containsAll(notes[3].Body, "Every job has an executor that can take it again.") ||
		containsAll(notes[3].Body, "Not known yet") {
		t.Fatalf("notes %+v", notes)
	}
}

// Review 526 B1: one host scope both ways. h1, told offline, is then taken
// out of work (drained, stopped or retired): its offline is closed quietly
// (a note, nothing told), its polling during that is not told, and, the
// drain or stop lifted while it polls, nothing stale is told either. h2
// offers what h1 does, so only h1's turns are in play.
func TestAHostTakenOutOfWorkIsNotToldBack(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"drain", "stop", "retired"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			w := presenceWorld(t, "h1", "h2")
			w.poll(t, "h1", claude, codex)
			w.poll(t, "h2", claude, codex)
			var p time.Time
			ok(t, w.rt.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&p))
			w.pollAt(t, "h1", p)
			w.pollAt(t, "h2", p.Add(40*time.Second))
			if n := presenceAt(t, w, p.Add(41*time.Second)); n != 1 || len(platformNotes(t, w)) != 1 {
				t.Fatalf("h1 lost: %d turns", n)
			}
			control := store.NewControlID()
			if kind == "retired" {
				w.legacy(t, `UPDATE agentrt.host SET state = 'retired' WHERE host_id = 'h1'`)
			} else {
				ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
					return store.SetControl(ctx, tx, "owner", control, "host", "h1", kind, "test: maintenance")
				}))
			}
			if n := presenceAt(t, w, p.Add(42*time.Second)); n != 1 || len(platformNotes(t, w)) != 1 {
				t.Fatalf("taken out of work: %d turns, %d notes", n, len(platformNotes(t, w)))
			}
			var head string
			ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'text' FROM agentrt.event WHERE event_stream_id = $1 ORDER BY seq DESC LIMIT 1`,
				store.HostPresenceStream("h1")).Scan(&head))
			if !strings.HasPrefix(head, "h1 was taken out of work") {
				t.Fatalf("h1's presence head %q", head)
			}
			w.pollAt(t, "h1", p.Add(43*time.Second))
			w.pollAt(t, "h2", p.Add(43*time.Second))
			if n := presenceAt(t, w, p.Add(44*time.Second)); n != 0 {
				t.Fatalf("polling while out of work: %d turns", n)
			}
			if kind != "retired" {
				ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.ClearControl(ctx, tx, "owner", control) }))
				if n := presenceAt(t, w, p.Add(45*time.Second)); n != 0 {
					t.Fatalf("back in work and online: %d turns", n)
				}
			}
			if notes := platformNotes(t, w); len(notes) != 1 || containsAll(notes[0].Body, "back online") {
				t.Fatalf("notes %+v", notes)
			}
		})
	}
}

// heartbeatAt sets attempt's last heartbeat to at, as its own goroutine's
// heartbeat at that moment would.
func (w *world) heartbeatAt(t *testing.T, attempt string, at time.Time) {
	t.Helper()
	_, err := w.rt.Exec(ctx, `UPDATE agentrt.attempt SET heartbeat_at = $2 WHERE attempt_id = $1`, attempt, at)
	ok(t, err)
}

func (d presenceDoc) claiming(t *testing.T, id string) *bool {
	t.Helper()
	for _, h := range d.Hosts {
		if h.HostID == id {
			return h.Claiming
		}
	}
	t.Fatalf("no host %s", id)
	return nil
}

// Review 527 A3: h1's claim loop stops while an attempt of it keeps
// reporting. It is alive (online, not reported offline, its attempt not
// host_lost) and takes no work (claiming false): the executors only it
// offers are told unavailable once, 30 s after its last claim, and back when
// its claims resume. In a prune round nothing is told within its budget, and
// they are told unavailable once the budget has passed though the attempt
// still reports.
func TestAHostThatOnlyHeartbeatsTakesNoWork(t *testing.T) {
	t.Parallel()
	w := presenceWorld(t, "h1")
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-1", "active", "test") }))
	task := w.ready(t, store.Task{Effect: "none"})
	w.poll(t, "h1", claude, codex)
	c := w.claim(t, "h1", time.Hour, codex)
	if c == nil || c.TaskID != task {
		t.Fatalf("claim %+v", c)
	}
	ok(t, store.Start(ctx, w.rt, fence(c, "h1"), "", ""))
	var p time.Time
	ok(t, w.rt.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&p))
	w.pollAt(t, "h1", p)
	w.heartbeatAt(t, c.AttemptID, p.Add(28*time.Second))
	if n := presenceAt(t, w, p.Add(30*time.Second)); n != 0 {
		t.Fatalf("%d turns 30 s after the last claim", n)
	}
	doc, status := w.presenceDocAt(t, p.Add(31*time.Second), time.Time{}, task)
	online, reported, _ := doc.host(t, "h1")
	if claiming := doc.claiming(t, "h1"); !online || reported || claiming == nil || *claiming || status == store.StatusHostLost {
		t.Fatalf("its claims stopped, its attempt reporting: online %v reported %v claiming %v, attempt %s", online, reported, claiming, status)
	}
	for _, e := range []string{"exec:claude", "exec:codex"} {
		if x := doc.executor(t, e); !isBool(x.Available, false) || !strings.Contains(x.Why, "host h1 offers it but has not claimed work since") {
			t.Fatalf("%s %+v", e, x)
		}
	}
	if n := presenceAt(t, w, p.Add(31*time.Second)); n != 2 {
		t.Fatalf("%d turns 31 s after the last claim; want claude and codex", n)
	}
	if n := presenceAt(t, w, p.Add(36*time.Second)); n != 0 {
		t.Fatalf("told again: %d", n)
	}
	notes := platformNotes(t, w)
	if len(notes) != 1 || !containsAll(notes[0].Body, "exec:claude is unavailable: host h1 offers it but has not claimed work since",
		"No executor can take these jobs now") || strings.Contains(notes[0].Body, "Host h1 is offline") {
		t.Fatalf("notes %+v", notes)
	}
	w.pollAt(t, "h1", p.Add(40*time.Second))
	if n := presenceAt(t, w, p.Add(41*time.Second)); n != 2 {
		t.Fatalf("%d turns once its claims resume", n)
	}
	// In a prune round: nothing within its budget, unavailable past it.
	ok(t, store.OpenPruneRound(ctx, w.rt, "h1", 2))
	var opened time.Time
	ok(t, w.rt.QueryRow(ctx, `SELECT at FROM agentrt.event WHERE event_stream_id = $1`, store.HostPruneStream("h1")).Scan(&opened))
	w.pollAt(t, "h1", opened)
	w.heartbeatAt(t, c.AttemptID, opened.Add(2*time.Minute))
	if n := presenceAt(t, w, opened.Add(2*time.Minute)); n != 0 {
		t.Fatalf("%d turns within a prune round's budget", n)
	}
	w.heartbeatAt(t, c.AttemptID, opened.Add(store.PruneSilence-2*time.Second))
	past := opened.Add(store.PruneSilence + time.Second)
	if n := presenceAt(t, w, past); n != 2 {
		t.Fatalf("%d turns past the prune budget, its attempt reporting; want claude and codex", n)
	}
	if doc, _ = w.presenceDocAt(t, past, time.Time{}, task); func() bool { o, r, _ := doc.host(t, "h1"); return !o || r }() {
		t.Fatalf("h1 past the prune budget, its attempt reporting: %+v", doc.Hosts)
	}
	if notes = platformNotes(t, w); len(notes) != 3 || strings.Contains(notes[2].Body, "Host h1 is offline") {
		t.Fatalf("notes %+v", notes)
	}
}

// headAt is the moment of the last record of stream.
func (w *world) headAt(t *testing.T, stream string) time.Time {
	t.Helper()
	var at time.Time
	ok(t, w.rt.QueryRow(ctx, `SELECT at FROM agentrt.event WHERE event_stream_id = $1 ORDER BY seq DESC LIMIT 1`, stream).Scan(&at))
	return at
}

// Review 527 A1b: one recovery point for every reader, the moment the
// platform could take agentd's calls again (the head of PlatformStream), and
// for the controller its own start too. On the clock of the records:
//
//   - the API connects 60 s after it was wired (and after the controller
//     started): a host silent through it is not shown or told lost until it
//     stays silent 30 s after the API connected, one that polls within them
//     never is;
//   - the API goes down (the controller up all along, as a controller up
//     before the API): nothing is told while it is down, nor until 30 s
//     after the next API is up;
//   - an API replaced in a rolling update records no down after its
//     successor's up;
//   - a down no up follows holds the hosts' silence for PlatformDownLimit at
//     most;
//   - the controller restarted alone, the API up long since: a host silent
//     since before its start is told 30 s after it, not before.
func TestTheHostsWaitForThePlatformsRecovery(t *testing.T) {
	t.Parallel()
	w := presenceWorld(t, "h1", "h2", "h3")
	for _, h := range []string{"h1", "h2", "h3"} {
		w.poll(t, h, claude, codex)
	}
	var t0 time.Time
	ok(t, w.rt.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&t0))
	for _, h := range []string{"h1", "h2", "h3"} {
		w.pollAt(t, h, t0.Add(-61*time.Second))
	}
	ok(t, store.MarkPlatformUp(ctx, w.rt, "api-1"))
	up := w.headAt(t, store.PlatformStream)
	started := up.Add(-60 * time.Second)
	pass := func(now, since time.Time) int {
		t.Helper()
		n, err := store.Presence(ctx, w.rt, now, since)
		ok(t, err)
		return n
	}
	reported := func(at time.Time, id string) bool {
		t.Helper()
		doc, _ := w.presenceDocAt(t, at, time.Time{}, "")
		_, r, _ := doc.host(t, id)
		return r
	}
	// The API connected 60 s after its wiring.
	if reported(up.Add(10*time.Second), "h1") || pass(up.Add(10*time.Second), started) != 0 {
		t.Fatal("a host silent through the API's start is lost 10 s after it connected")
	}
	w.pollAt(t, "h2", up.Add(20*time.Second))
	w.pollAt(t, "h3", up.Add(25*time.Second))
	if n := pass(up.Add(30*time.Second), started); n != 0 || reported(up.Add(30*time.Second), "h1") {
		t.Fatalf("%d turns 30 s after the API connected", n)
	}
	if n := pass(up.Add(30*time.Second+time.Millisecond), started); n != 1 || !reported(up.Add(31*time.Second), "h1") || reported(up.Add(31*time.Second), "h2") {
		t.Fatalf("%d turns just past 30 s after the API connected; want h1 alone", n)
	}
	// The API goes down; the controller, up all along, tells nothing until
	// 30 s after the next API is up.
	if did, err := store.MarkPlatformDown(ctx, w.rt, "api-1"); err != nil || !did {
		t.Fatalf("down: %v %v", did, err)
	}
	down := w.headAt(t, store.PlatformStream)
	w.pollAt(t, "h3", down.Add(-5*time.Second))
	for _, d := range []time.Duration{40 * time.Second, 5 * time.Minute, 9 * time.Minute} {
		if n := pass(down.Add(d), started); n != 0 || reported(down.Add(d), "h3") {
			t.Fatalf("%d turns %v after the API went down", n, d)
		}
	}
	ok(t, store.MarkPlatformUp(ctx, w.rt, "api-2"))
	up2 := w.headAt(t, store.PlatformStream)
	w.pollAt(t, "h2", up2.Add(10*time.Second))
	if n := pass(up2.Add(30*time.Second), started); n != 0 {
		t.Fatalf("%d turns 30 s after the next API came up", n)
	}
	if n := pass(up2.Add(30*time.Second+time.Millisecond), started); n != 1 {
		t.Fatalf("%d turns just past 30 s after the next API came up; want h3 alone", n)
	}
	// A rolling update: the new API is up before the old one goes.
	ok(t, store.MarkPlatformUp(ctx, w.rt, "api-3"))
	if did, err := store.MarkPlatformDown(ctx, w.rt, "api-2"); err != nil || did {
		t.Fatalf("the replaced API's down: %v %v", did, err)
	}
	// A down no up follows holds for PlatformDownLimit at most.
	if did, err := store.MarkPlatformDown(ctx, w.rt, "api-3"); err != nil || !did {
		t.Fatalf("down: %v %v", did, err)
	}
	down3 := w.headAt(t, store.PlatformStream)
	w.pollAt(t, "h2", down3.Add(-time.Second))
	limit := down3.Add(store.PlatformDownLimit)
	if n := pass(limit.Add(30*time.Second), started); n != 0 {
		t.Fatalf("%d turns 30 s past the down limit", n)
	}
	if n := pass(limit.Add(31*time.Second), started); n != 3 {
		t.Fatalf("%d turns 31 s past the down limit; want h2 and its executors", n)
	}
	// The controller restarted alone, the API up long since.
	ok(t, store.MarkPlatformUp(ctx, w.rt, "api-4"))
	up4 := w.headAt(t, store.PlatformStream)
	restarted := up4.Add(10 * time.Minute)
	for _, h := range []string{"h1", "h2", "h3"} {
		w.pollAt(t, h, restarted.Add(-time.Second))
	}
	if n := pass(restarted.Add(-500*time.Millisecond), started); n != 5 {
		t.Fatalf("%d turns with every host polling; want h1, h2, h3 and the two executors back", n)
	}
	w.pollAt(t, "h1", restarted.Add(-10*time.Second))
	w.pollAt(t, "h2", restarted.Add(20*time.Second))
	w.pollAt(t, "h3", restarted.Add(20*time.Second))
	if n := pass(restarted.Add(30*time.Second), restarted); n != 0 {
		t.Fatalf("%d turns 30 s after the controller's start", n)
	}
	if n := pass(restarted.Add(30*time.Second+time.Millisecond), restarted); n != 1 {
		t.Fatalf("%d turns just past 30 s after the controller's start; want h1", n)
	}
}
