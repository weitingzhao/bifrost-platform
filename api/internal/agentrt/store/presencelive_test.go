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
