package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// The exact claim and the pinned result (W-31 round 4, core block C-2;
// DESIGN-coordinator-landing-v3-2026-10-29.md §2.4–§2.8, §6 T5–T18).

const coordinatorToken = "agentd:" + externalHost

func exactReq(task, key string) store.PinnedClaimRequest {
	return store.PinnedClaimRequest{HostID: externalHost, TaskID: task, Key: key, FreeSlots: 1, Token: coordinatorToken}
}

func (w *world) exact(task, key string) (map[string]any, error) {
	return store.ClaimPinned(ctx, w.rt, exactReq(task, key))
}

func pinNum(t *testing.T, v any) int64 {
	t.Helper()
	switch n := v.(type) {
	case json.Number:
		i, err := n.Int64()
		ok(t, err)
		return i
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	}
	t.Fatalf("%v (%T) is not a number", v, v)
	return 0
}

func (w *world) attempts(t *testing.T, task string) int {
	t.Helper()
	return w.count(t, `SELECT count(*) FROM agentrt.attempt WHERE task_id = $1`, task)
}

// T5, T6, T7 at the store: the external host leases each pinned task by id
// into its own attempt and branch; another host, or a task with no pin, is
// refused with nothing leased.
func TestExactClaim(t *testing.T) {
	t.Parallel()
	w := pinWorld(t)
	w.slots(t, 2)
	a, b := w.readyPinned(t, "a"), w.readyPinned(t, "b")
	plain := w.ready(t, store.Task{})

	// Refusals first, each one change from the claims that succeed.
	if _, err := store.ClaimPinned(ctx, w.rt, store.PinnedClaimRequest{HostID: "h1", TaskID: a, Key: "k-h1", FreeSlots: 1, Token: "agentd:h1"}); !errors.Is(err, store.ErrNotExternal) {
		t.Fatalf("an ordinary host (T5): %v", err)
	}
	if _, err := w.exact(plain, "k-plain"); !errors.Is(err, store.ErrNotPinned) {
		t.Fatalf("a task with no pin (T7): %v", err)
	}
	if n := w.attempts(t, a) + w.attempts(t, plain); n != 0 {
		t.Fatalf("%d attempts after the refusals", n)
	}

	// T6: both, by id, each its own.
	for _, task := range []string{b, a} {
		out, err := w.exact(task, task+":1")
		ok(t, err)
		if out["task_id"] != task || pinNum(t, out["attempt_no"]) != 1 || out["pushed_ref"] != store.AttemptRef(task, 1) ||
			out["tool"].(map[string]any)["executor"] != "exec:claude" || out["tool"].(map[string]any)["provider"] != "anthropic" {
			t.Fatalf("claim of %s: %v", task, out)
		}
	}
	var host string
	ok(t, w.rt.QueryRow(ctx, `SELECT host_id FROM agentrt.attempt WHERE task_id = $1`, a).Scan(&host))
	if host != externalHost {
		t.Fatalf("leased on %s", host)
	}
}

// T8: the exact claim judges as the ordinary one does. Each case changes one
// thing of a world whose claim succeeds (the control case), and leases
// nothing, saying why; nothing is stored under its key.
func TestExactClaimJudgesAsAClaim(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		edit func(t *testing.T, w *world, task string)
		want error
		why  string
	}{
		"control": {func(*testing.T, *world, string) {}, nil, ""},
		"host stopped": {func(t *testing.T, w *world, _ string) {
			ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
				return store.SetControl(ctx, tx, "owner", "c-host", "host", externalHost, "stop", "test")
			}))
		}, store.ErrNoLease, "stop, pause or drain holds the host"},
		"mission stopped": {func(t *testing.T, w *world, _ string) {
			ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-1", "stopped", "test") }))
		}, store.ErrNoLease, ""},
		"task paused": {func(t *testing.T, w *world, task string) {
			ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
				return store.SetControl(ctx, tx, "owner", "c-task", "task", task, "pause", "test")
			}))
		}, store.ErrNoLease, "a stop or pause holds its mission or the task"},
		"envelope dropped the host": {func(t *testing.T, w *world, _ string) {
			_, err := store.TightenRule(ctx, w.rt, wr("thread:s1"), "standing-envelope:dev", []byte(pinEnvelope(2, rules.Tighten, false)))
			ok(t, err)
		}, store.ErrNotExternal, ""},
		"per_host 0": {func(t *testing.T, w *world, _ string) {
			_, err := store.TightenRule(ctx, w.rt, wr("thread:s1"), "concurrency", []byte(fmt.Sprintf(concurrencyHead, 2, rules.Tighten)+"per_host: 0\n"))
			ok(t, err)
		}, store.ErrNoLease, "no concurrency rule"},
		"host full": {func(t *testing.T, w *world, _ string) {
			other := w.readyPinned(t, "other")
			_, err := w.exact(other, "other:1")
			ok(t, err)
		}, store.ErrNoLease, "all its 1 slots"},
		"budget spent": {func(t *testing.T, w *world, _ string) {
			w.ready(t, store.Task{})
			if c := w.claim(t, "h1", time.Hour, claude); c == nil {
				t.Fatal("no ordinary claim to spend the budget with")
			}
			ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
				_, err := store.Append(ctx, tx, store.Event{Stream: store.MissionStream("M-1"), Type: "mission_amended", Actor: "owner",
					MissionID: "M-1", Body: map[string]any{"reason": "test", "max_attempts": 1, "max_wall_hours": 100}})
				if err != nil {
					return err
				}
				_, err = tx.Exec(ctx, `UPDATE agentrt.mission SET budget_json = '{"max_attempts": 1, "max_wall_hours": 100}' WHERE mission_id = 'M-1'`)
				return err
			}))
		}, store.ErrNoLease, "budget"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			w := pinWorld(t)
			task := w.readyPinned(t, "k")
			c.edit(t, w, task)
			out, err := w.exact(task, "key")
			switch {
			case c.want == nil:
				ok(t, err)
				if out["task_id"] != task {
					t.Fatalf("control: %v", out)
				}
				return
			case !errors.Is(err, c.want) || !strings.Contains(err.Error(), c.why):
				t.Fatalf("%v; want %v (%q)", err, c.want, c.why)
			}
			if n := w.attempts(t, task); n != 0 {
				t.Fatalf("%d attempts", n)
			}
			if n := w.count(t, `SELECT count(*) FROM agentrt.idempotency_key WHERE idempotency_key_id LIKE '%claim_pinned|key'`); n != 0 {
				t.Fatalf("a refusal stored its key")
			}
		})
	}
}

// T9 and the registration of the external host: an ordinary claim of the
// external host is refused before it writes anything; an agentd call never
// makes a host a standing envelope names an ordinary one, and the host's
// first exact claim registers it as the external host.
func TestExternalHostClaimsOnlyByID(t *testing.T) {
	t.Parallel()
	w := pinWorld(t)
	plain := w.ready(t, store.Task{})
	if _, err := store.ClaimNext(ctx, w.rt, store.ClaimRequest{HostID: externalHost, FreeSlots: 1, Tools: []store.Tool{claude}}); !errors.Is(err, store.ErrExternalHost) {
		t.Fatalf("an ordinary claim of the external host: %v", err)
	}
	if err := store.RefuseExternalHost(ctx, w.rt, externalHost); !errors.Is(err, store.ErrExternalHost) {
		t.Fatalf("RefuseExternalHost: %v", err)
	}
	ok(t, store.RefuseExternalHost(ctx, w.rt, "h1"))
	if c := w.claim(t, "h1", time.Hour, claude); c == nil || c.TaskID != plain {
		t.Fatalf("an ordinary host's claim: %+v", c)
	}

	// A world whose envelope names the host before any call of it.
	w2 := newKernelWorld(t)
	registerSeat(t, w2)
	s := signer{t, rptest.NewKey(t)}
	ok(t, firstErr(s.sign(w2, strings.Replace(pinEnvelope(1, rules.Initial, true), "hosts: [h1, coordinator-mbp]", "hosts: [h1]", 1))))
	if err := store.RegisterAgentdHost(ctx, w2.rt, externalHost, coordinatorToken); !errors.Is(err, store.ErrExternalHost) {
		t.Fatalf("an agentd call registering the named host: %v", err)
	}
	if n := w2.count(t, `SELECT count(*) FROM agentrt.host WHERE host_id = $1`, externalHost); n != 0 {
		t.Fatal("the refused call registered the host")
	}
	ok(t, store.RegisterAgentdHost(ctx, w2.rt, "h9", "agentd:h9"))
	task := w2.readyPinned(t, "k")
	_, err := w2.exact(task, "k:1")
	ok(t, err)
	var kind string
	ok(t, w2.rt.QueryRow(ctx, `SELECT host_kind FROM agentrt.host WHERE host_id = $1`, externalHost).Scan(&kind))
	if kind != store.HostExternal {
		t.Fatalf("registered as %s", kind)
	}
}

// T12: the key is the host's and the request its task. The same key gets
// the first answer again, even once the attempt has ended; another task
// with it is a conflict; two calls with one key at once lease once; a new
// key after the attempt ended gets the next attempt and its own branch.
func TestExactClaimKey(t *testing.T) {
	t.Parallel()
	w := pinWorld(t)
	task, other := w.readyPinned(t, "a"), w.readyPinned(t, "b")
	pool := w.db.Pool(t, w.db.Runtime)
	var wg sync.WaitGroup
	outs := make([]map[string]any, 4)
	errs := make([]error, 4)
	for i := range outs {
		wg.Add(1)
		go func() { defer wg.Done(); outs[i], errs[i] = store.ClaimPinned(ctx, pool, exactReq(task, "k1")) }()
	}
	wg.Wait()
	for i := range outs {
		ok(t, errs[i])
		if outs[i]["attempt_id"] != outs[0]["attempt_id"] {
			t.Fatalf("one key leased %v and %v", outs[0]["attempt_id"], outs[i]["attempt_id"])
		}
	}
	if n := w.attempts(t, task); n != 1 {
		t.Fatalf("%d attempts for one key", n)
	}
	if _, err := w.exact(other, "k1"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("the key with another task: %v", err)
	}
	if _, err := w.exact(task, "k2"); !errors.Is(err, store.ErrState) {
		t.Fatalf("a new key while the attempt is live: %v", err)
	}
	first := outs[0]["attempt_id"].(string)
	_, err := store.Ended(ctx, w.rt, first, externalHost, "lease_lost", true)
	ok(t, err)
	again, err := w.exact(task, "k1")
	ok(t, err)
	if again["attempt_id"] != first {
		t.Fatalf("the old key after the attempt ended: %v", again)
	}
	next, err := w.exact(task, "k2")
	ok(t, err)
	if pinNum(t, next["attempt_no"]) != 2 || next["pushed_ref"] != store.AttemptRef(task, 2) {
		t.Fatalf("a new key: %v", next)
	}
	// Direct key lookup coverage; T13's COMMIT fault is exercised through
	// ClaimPinned by Test695ClaimPinnedCommitOutcome.
	back, found := store.ReadBackClaimPinned(ctx, w.rt, externalHost, "k2", task)
	if !found || back["attempt_id"] != next["attempt_id"] {
		t.Fatalf("read back: %v %v", back, found)
	}
	if _, found := store.ReadBackClaimPinned(ctx, w.rt, externalHost, "k3", task); found {
		t.Fatal("read back a key never stored")
	}
}

// pinnedRun is a pinned task leased to the external host, started and
// computed, with its report stored: ready to persist (ttl past the grace:
// ready to salvage).
type pinnedRun struct {
	task, attempt, report string
	fence                 store.Fence
	no                    int
}

func (w *world) pinnedRun(t *testing.T, key string, ttl time.Duration) pinnedRun {
	t.Helper()
	task := w.readyPinned(t, key)
	out, err := store.ClaimPinnedFor(ctx, w.rt, exactReq(task, key+":1"), ttl)
	ok(t, err)
	r := pinnedRun{task: task, attempt: out["attempt_id"].(string), no: int(pinNum(t, out["attempt_no"]))}
	r.fence = store.Fence{AttemptID: r.attempt, HostID: externalHost, Epoch: pinNum(t, out["lease_epoch"])}
	if ttl > 0 {
		ok(t, store.Start(ctx, w.rt, r.fence, "", "anthropic"))
		zero := 0
		_, err = store.Computed(ctx, w.rt, r.fence, &zero, "exit", "done")
		ok(t, err)
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		r.report, err = store.PutArtifact(ctx, tx, "controller", "report", "text/markdown", []byte("handed in "+task))
		return err
	}))
	return r
}

// goodCommit is the commit a pinned task's result is: one, on the base, its
// task and one Change-Id named, a path outside the core.
func goodCommit(task string) store.CommitFacts {
	return store.CommitFacts{Parents: []string{commitA}, Message: "a change\n\nAgentrt-Task: " + task + "\nChange-Id: I" + strings.Repeat("ab", 20) + "\n",
		Paths: []string{"api/internal/research/x.go"}, Lines: 40}
}

func forge(at string, facts store.CommitFacts) (store.BranchHead, store.CommitReader) {
	return func(context.Context, string, string) (string, error) { return at, nil },
		func(context.Context, string, string) (store.CommitFacts, error) { return facts, nil }
}

// T14, T15, T16: persist and salvage check a pinned result the same way, on
// what the forge says; each refusal changes one thing of the result that
// persists and leaves the attempt as it was. Without a commit reader a
// pinned result waits.
func TestPinnedResultCheck(t *testing.T) {
	t.Parallel()
	w := pinWorld(t)
	w.slots(t, 3)
	type entry struct {
		name string
		run  func(r pinnedRun, res store.Result, head store.BranchHead, commits store.CommitReader) (map[string]any, error)
		ttl  time.Duration
	}
	entries := []entry{
		{"persist", func(r pinnedRun, res store.Result, head store.BranchHead, commits store.CommitReader) (map[string]any, error) {
			return store.PersistChecked(ctx, w.rt, r.fence, res, head, commits)
		}, time.Hour},
		{"salvage", func(r pinnedRun, res store.Result, head store.BranchHead, commits store.CommitReader) (map[string]any, error) {
			return store.SalvageChecked(ctx, w.rt, r.attempt, externalHost, res, head, commits)
		}, -store.Grace - time.Minute},
	}
	for _, e := range entries {
		r := w.pinnedRun(t, e.name, e.ttl)
		good := store.Result{Report: r.report, Ref: store.AttemptRef(r.task, r.no), Commit: commitB}
		cases := map[string]func(res *store.Result, at *string, f *store.CommitFacts){
			"another attempt's branch": func(res *store.Result, _ *string, _ *store.CommitFacts) { res.Ref = store.AttemptRef(r.task, r.no+1) },
			"the branch elsewhere":     func(_ *store.Result, at *string, _ *store.CommitFacts) { *at = commitA },
			"another parent": func(_ *store.Result, _ *string, f *store.CommitFacts) {
				f.Parents = []string{"1111111111111111111111111111111111111111"}
			},
			"a merge": func(_ *store.Result, _ *string, f *store.CommitFacts) {
				f.Parents = []string{commitA, "1111111111111111111111111111111111111111"}
			},
			"no Agentrt-Task": func(_ *store.Result, _ *string, f *store.CommitFacts) {
				f.Message = strings.Replace(f.Message, "Agentrt-Task: "+r.task+"\n", "", 1)
			},
			"two Agentrt-Task": func(_ *store.Result, _ *string, f *store.CommitFacts) { f.Message += "Agentrt-Task: " + r.task + "\n" },
			"another task": func(_ *store.Result, _ *string, f *store.CommitFacts) {
				f.Message = strings.Replace(f.Message, r.task, "t_X", 1)
			},
			"no Change-Id": func(_ *store.Result, _ *string, f *store.CommitFacts) {
				f.Message = strings.Replace(f.Message, "Change-Id: I"+strings.Repeat("ab", 20)+"\n", "", 1)
			},
			"core work, not core (T16)": func(_ *store.Result, _ *string, f *store.CommitFacts) {
				f.Paths = []string{"api/internal/agentrt/store/x.go"}
			},
		}
		if e.name == "persist" {
			cases["no report (T15)"] = func(res *store.Result, _ *string, _ *store.CommitFacts) { res.Report = "" }
		}
		for name, edit := range cases {
			res, at, facts := good, commitB, goodCommit(r.task)
			edit(&res, &at, &facts)
			before := w.count(t, `SELECT count(*) FROM agentrt.event`)
			head, commits := forge(at, facts)
			_, err := e.run(r, res, head, commits)
			if !errors.Is(err, store.ErrState) {
				t.Errorf("%s, %s: %v", e.name, name, err)
			}
			if name == "core work, not core (T16)" && !errors.Is(err, store.ErrCoreMismatch) {
				t.Errorf("%s: %v; want core_mismatch", e.name, err)
			}
			if after := w.count(t, `SELECT count(*) FROM agentrt.event`); after != before {
				t.Errorf("%s, %s: %d events", e.name, name, after-before)
			}
		}
		// Without a reader of commits a pinned result waits.
		head, _ := forge(commitB, goodCommit(r.task))
		if _, err := e.run(r, good, head, nil); !errors.Is(err, store.ErrNoHead) {
			t.Errorf("%s without a commit reader: %v", e.name, err)
		}
		head, commits := forge(commitB, goodCommit(r.task))
		_, err := e.run(r, good, head, commits)
		ok(t, err)
		var body map[string]any
		ok(t, w.rt.QueryRow(ctx, `SELECT body_json FROM agentrt.event WHERE attempt_id = $1 AND event_type IN ('attempt_persisted','attempt_salvaged')`,
			r.attempt).Scan(&body))
		if pinNum(t, body["changed_lines"]) != 40 || body["hard"] != false {
			t.Errorf("%s recorded %v", e.name, body)
		}
	}

	// T15: a result before computed.
	task := w.readyPinned(t, "early")
	out, err := w.exact(task, "early:1")
	ok(t, err)
	f := store.Fence{AttemptID: out["attempt_id"].(string), HostID: externalHost, Epoch: pinNum(t, out["lease_epoch"])}
	ok(t, store.Start(ctx, w.rt, f, "", "anthropic"))
	var report string
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		report, err = store.PutArtifact(ctx, tx, "controller", "report", "text/markdown", []byte("early "+task))
		return err
	}))
	head, commits := forge(commitB, goodCommit(task))
	if _, err := store.PersistChecked(ctx, w.rt, f, store.Result{Report: report, Ref: store.AttemptRef(task, 1), Commit: commitB}, head, commits); !errors.Is(err, store.ErrState) {
		t.Fatalf("a result before computed: %v", err)
	}
}

// T16, its other half: a core task's commit under the core persists; and the
// repository leaves the entry: refused.
func TestPinnedResultCoreAndRepo(t *testing.T) {
	t.Parallel()
	w := pinWorld(t)
	r := w.pinnedRun(t, "a", time.Hour)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := store.Append(ctx, tx, store.Event{Stream: store.MissionStream("M-1"), Type: "task_state_changed", Actor: "controller",
			MissionID: "M-1", PlanNode: s1, TaskID: r.task, Body: map[string]any{"from": "running", "to": "running", "reason": "test: core"}})
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE agentrt.task SET core = true WHERE task_id = $1`, r.task)
		return err
	}))
	facts := goodCommit(r.task)
	facts.Paths = []string{"api/internal/agentrt/store/x.go"}
	head, commits := forge(commitB, facts)
	_, err := store.PersistChecked(ctx, w.rt, r.fence, store.Result{Report: r.report, Ref: store.AttemptRef(r.task, 1), Commit: commitB}, head, commits)
	ok(t, err)
	var hard bool
	ok(t, w.rt.QueryRow(ctx, `SELECT (body_json->>'hard')::boolean FROM agentrt.event WHERE attempt_id = $1 AND event_type = 'attempt_persisted'`,
		r.attempt).Scan(&hard))
	if !hard {
		t.Fatal("a commit in agentrt/store is not recorded as touching the trusted core")
	}

	w2 := pinWorld(t)
	r2 := w2.pinnedRun(t, "b", time.Hour)
	_, err = store.TightenRule(ctx, w2.rt, wr("thread:s1"), "standing-envelope:dev",
		[]byte(strings.Replace(pinEnvelope(2, rules.Tighten, true), "repos: [bifrost-platform]", "repos: []", 1)))
	if err == nil {
		t.Fatal("an entry with no repositories read")
	}
	// An entry narrowed to nothing it had is dropped instead: the result is refused.
	_, err = store.TightenRule(ctx, w2.rt, wr("thread:s1"), "standing-envelope:dev", []byte(pinEnvelope(2, rules.Tighten, false)))
	ok(t, err)
	head, commits = forge(commitB, goodCommit(r2.task))
	if _, err = store.PersistChecked(ctx, w2.rt, r2.fence, store.Result{Report: r2.report, Ref: store.AttemptRef(r2.task, 1), Commit: commitB}, head, commits); !errors.Is(err, store.ErrNotExternal) {
		t.Fatalf("with the entry gone: %v", err)
	}
	if w2.count(t, `SELECT count(*) FROM agentrt.attempt WHERE attempt_id = $1 AND persisted_at IS NULL`, r2.attempt) != 1 {
		t.Fatal("the refused result was recorded")
	}
}

// T17: a pinned task's review goes to Codex of openai alone, whatever else
// the reviewer job lists, and to nobody when the subject's attempt cannot
// be shown independent of it; a review of an ordinary task is routed as
// before. Its time limit follows the commit, and a commit touching the
// trusted core brings the trust chain's questions.
func TestPinnedReviewRoute(t *testing.T) {
	t.Parallel()
	w := pinWorld(t)
	w.slots(t, 3)
	route := func(task string) []string {
		var r []string
		ok(t, w.rt.QueryRow(ctx, `SELECT ARRAY(SELECT jsonb_array_elements_text(route_json)) FROM agentrt.task WHERE task_id = $1`, task).Scan(&r))
		return r
	}
	review := func(subject string) string {
		id := store.NewTaskID()
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			if _, err := store.ProposeTask(ctx, tx, "controller", store.Task{ID: id, MissionID: "M-1", PlanNode: s1, Subject: subject, Role: "review",
				Class: "go", Title: "review", SpecArtifact: w.spec, Effect: "none"}); err != nil {
				return err
			}
			return store.MarkReady(ctx, tx, "controller", id)
		}))
		return id
	}
	pinned := w.readyPinned(t, "a")
	_, err := w.exact(pinned, "a:1")
	ok(t, err)
	got := route(review(pinned))
	if len(got) == 0 || slices.ContainsFunc(got, func(c string) bool { return !strings.HasPrefix(c, "reviewer/exec:codex/openai/") }) {
		t.Fatalf("a pinned task's review routes to %v", got)
	}
	plain := w.ready(t, store.Task{})
	c := w.claim(t, "h1", time.Hour, cursor)
	if c == nil || c.TaskID != plain {
		t.Fatalf("claim of the plain task: %+v", c)
	}
	if got := route(review(plain)); !slices.ContainsFunc(got, func(c string) bool { return strings.HasPrefix(c, "reviewer/exec:claude/") }) {
		t.Fatalf("an ordinary review routes to %v", got)
	}
	// The subject's attempt with no provider recorded: nobody.
	forged := w.readyPinned(t, "b")
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, _, err := store.LeaseAttempt(ctx, tx, "controller", "M-1", forged, "exec:claude", externalHost, 1, time.Hour); err != nil {
			return err
		}
		return store.MoveTaskForTest(ctx, tx, "controller", forged, "leased")
	}))
	if got := route(review(forged)); len(got) != 0 {
		t.Fatalf("a review of a subject with no provider routes to %v", got)
	}

	// The limit and the questions, from what the result's check recorded.
	r := w.pinnedRun(t, "c", time.Hour)
	facts := goodCommit(r.task)
	facts.Lines, facts.Paths = 650, []string{"api/internal/server/x.go"}
	head, commits := forge(commitB, facts)
	_, err = store.PersistChecked(ctx, w.rt, r.fence, store.Result{Report: r.report, Ref: store.AttemptRef(r.task, 1), Commit: commitB}, head, commits)
	ok(t, err)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		limit, text, err := store.PinnedReviewForTest(ctx, tx, r.task, r.attempt)
		if err != nil {
			return err
		}
		if limit != 2*time.Hour+30*time.Minute || text != store.TrustReviewText {
			t.Errorf("a 650-line commit in server/: %v, %q", limit, text)
		}
		limit, text, err = store.PinnedReviewForTest(ctx, tx, plain, c.AttemptID)
		if err != nil {
			return err
		}
		if limit != 0 || text != "" {
			t.Errorf("an ordinary review: %v, %q", limit, text)
		}
		return nil
	}))
}

func TestPinnedReviewLimit(t *testing.T) {
	t.Parallel()
	for lines, want := range map[int]time.Duration{0: time.Hour, 1: 90 * time.Minute, 300: 90 * time.Minute, 301: 2 * time.Hour,
		1200: 3 * time.Hour, 100000: 3 * time.Hour} {
		if got := store.PinnedReviewLimit(lines); got != want {
			t.Errorf("%d lines: %v, want %v", lines, got, want)
		}
	}
}

// T18 and F4: a pinned task's spec is the platform's, with the repository's
// own checks, whatever the proposer uploaded; it declares its providers, the
// entry's alone.
func TestPinnedSpecAndProviders(t *testing.T) {
	t.Parallel()
	w := pinWorld(t)
	var thin string
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		var err error
		thin, err = store.PutArtifact(ctx, tx, "controller", "task_spec", "application/json", []byte(`{"text":"no checks","verify":[["true"]]}`))
		return err
	}))
	task := func(providers ...string) []store.PinnedTask {
		p := w.pinnedTask("a", rules.PinCoordinator)
		p.SpecArtifact, p.Providers = thin, providers
		return []store.PinnedTask{p}
	}
	for name, providers := range map[string][]string{"none": nil, "another": {"openai"}, "mixed": {"anthropic", "openai"}} {
		if _, err := store.ProposePinnedTasks(ctx, w.rt, coordinator, "k-"+name, "M-1", "", task(providers...)); !errors.Is(err, store.ErrMixedSource) {
			t.Errorf("providers %s: %v", name, err)
		}
	}
	out, err := store.ProposePinnedTasks(ctx, w.rt, coordinator, "k", "M-1", "", task("anthropic"))
	ok(t, err)
	id := out["tasks"].([]any)[0].(map[string]any)["task_id"].(string)
	var spec string
	var body []byte
	ok(t, w.rt.QueryRow(ctx, `SELECT t.spec_artifact_id, b.body FROM agentrt.task t JOIN agentrt.artifact_blob b ON b.artifact_id = t.spec_artifact_id
	  WHERE t.task_id = $1`, id).Scan(&spec, &body))
	if spec == thin {
		t.Fatal("the uploaded spec was taken")
	}
	var specDoc struct{ Verify [][]string }
	ok(t, json.Unmarshal(body, &specDoc))
	if want := store.Repos["bifrost-platform"].Verify; len(want) == 0 || !slices.EqualFunc(specDoc.Verify, want, slices.Equal) {
		t.Fatalf("the spec runs %q; the repository requires %q", specDoc.Verify, want)
	}
	var providers []string
	ok(t, w.rt.QueryRow(ctx, `SELECT ARRAY(SELECT jsonb_array_elements_text(body_json->'providers')) FROM agentrt.event
	  WHERE task_id = $1 AND event_type = 'task_proposed'`, id).Scan(&providers))
	if !slices.Equal(providers, []string{"anthropic"}) {
		t.Fatalf("recorded providers %v", providers)
	}
}

// The seat the platform registers at its start (the gap C-1 left): only
// RegisterableSeats, only when the token table names it, once.
func TestEnsureSeats(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	done, err := store.EnsureSeats(ctx, w.rt, []string{"seat:ghost"})
	ok(t, err)
	if len(done) != 0 || w.count(t, `SELECT count(*) FROM agentrt.actor WHERE actor_id IN ('seat:ghost', $1)`, coordinatorSeat) != 0 {
		t.Fatalf("registered %v without the coordinator's token", done)
	}
	done, err = store.EnsureSeats(ctx, w.rt, []string{"seat:ghost", coordinatorSeat})
	ok(t, err)
	if !slices.Equal(done, []string{coordinatorSeat}) {
		t.Fatalf("registered %v", done)
	}
	again, err := store.EnsureSeats(ctx, w.rt, []string{coordinatorSeat})
	ok(t, err)
	if len(again) != 0 || w.count(t, `SELECT count(*) FROM agentrt.event WHERE event_type = 'actor_registered' AND body_json->'actors' ? $1`, coordinatorSeat) != 1 {
		t.Fatalf("a second start registered %v", again)
	}
	if w.count(t, `SELECT count(*) FROM agentrt.actor WHERE actor_id = 'seat:ghost'`) != 0 {
		t.Fatal("a seat outside RegisterableSeats was registered")
	}
}

// registerSeat registers the coordinator's seat as the platform's start does.
func registerSeat(t *testing.T, w *world) {
	t.Helper()
	_, err := store.EnsureSeats(ctx, w.rt, []string{coordinatorSeat})
	ok(t, err)
}

// slots signs a concurrency rule giving each host n live attempts.
func (w *world) slots(t *testing.T, n int) {
	t.Helper()
	s := signer{t, rptest.NewKey(t)}
	ok(t, firstErr(s.sign(w, fmt.Sprintf(concurrencyHead, 2, rules.Loosen)+fmt.Sprintf("per_host: %d\n", n))))
}
