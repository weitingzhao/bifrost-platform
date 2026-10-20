package store_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// passRound carries impl, ready, through a round its checks pass, to
// accepted.
func (w *world) passRound(t *testing.T, impl, author, checker, reviewer string) {
	t.Helper()
	if c := w.run(t, author, cursor, "done"); c.TaskID != impl {
		t.Fatalf("claimed %s, not %s", c.TaskID, impl)
	}
	v, err := store.StartVerification(ctx, w.rt, impl)
	ok(t, err)
	w.run(t, checker, deterministic, "done")
	if did, err := store.Conclude(ctx, w.rt, v); err != nil || did != "review" {
		t.Fatalf("the verification did %q (%v)", did, err)
	}
	review := w.checker(t, impl, "review")
	w.run(t, reviewer, codex, "done")
	if did, err := store.Conclude(ctx, w.rt, review); err != nil || did != "accepted" {
		t.Fatalf("the review did %q (%v)", did, err)
	}
}

// head is the attempt branch of attempt n of task as Gitea would show it.
func head(task string, n int, commit string) store.AgentHead {
	return store.AgentHead{Repo: "bifrost-platform", Ref: store.AttemptRef(task, n), Commit: commit}
}

func prunedRefs(ps []store.Prune) map[string]string {
	out := map[string]string{}
	for _, p := range ps {
		out[p.Ref] = p.Why
	}
	return out
}

// TD-383 (the Owner's 「分支 B 用 agentd」): which attempt branches go, and
// when. Seven days after the event that lets them go: a cancelled or a
// rejected task's; a superseded attempt of an accepted task, from the
// acceptance; the accepted attempt of a mission not to be merged, from the
// acceptance, and of any other, from when its result was first seen on main.
// Never a task still open, a result not seen on main, a branch this runtime
// has no task for, or one that names another commit than its attempt pushed.
func TestPrunableFollowsTheRule(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.goalMission(t, "M-3")
	w.goalMission(t, "M-4")
	w.legacy(t, `UPDATE agentrt.mission SET scope_json = scope_json || '{"no_merge": "a benchmark replay"}' WHERE mission_id = 'M-4'`)
	// M-3: accepted in round 2 (attempt 1's check failed, attempt 2 accepted).
	accepted := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
	if did := w.failRound(t, accepted, "h1", "h2"); did != "rework" {
		t.Fatalf("round 1 did %q", did)
	}
	w.passRound(t, accepted, "h3", "h4", "h5")
	// M-4, not to be merged: accepted in round 1.
	replay := w.ready(t, store.Task{MissionID: "M-4", OffPlanReason: "a goal"})
	w.passRound(t, replay, "h6", "h7", "h8")
	// M-3: one cancelled, one rejected, one still open.
	cancelled := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
	w.run(t, "h1", cursor, "done")
	rejected := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
	w.run(t, "h2", cursor, "done")
	open := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
	w.run(t, "h3", cursor, "done")
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		for _, m := range []struct{ task, to string }{{cancelled, "blocked"}, {cancelled, "cancelled"}, {rejected, "verifying"},
			{rejected, "rejected"}} {
			if err := store.MoveTaskForTest(ctx, tx, "controller", m.task, m.to); err != nil {
				return err
			}
		}
		return nil
	}))
	heads := []store.AgentHead{head(accepted, 1, commitA), head(accepted, 2, commitA), head(replay, 1, commitA),
		head(cancelled, 1, commitA), head(rejected, 1, commitA), head(open, 1, commitA)}
	// Another environment's task, and a branch that names no attempt.
	others := []store.AgentHead{head(store.NewTaskID(), 1, commitA), {Repo: "bifrost-platform", Ref: "refs/heads/agent/manual/x", Commit: commitA}}

	now := time.Now()
	if ps, err := store.Prunable(ctx, w.rt, append(heads, others...), nil, now); err != nil || len(ps) != 0 {
		t.Fatalf("today: %v %v; want none before seven days", ps, err)
	}
	later := now.Add(store.PruneAfter + time.Hour)
	ps, err := store.Prunable(ctx, w.rt, append(heads, others...), nil, later)
	ok(t, err)
	got := prunedRefs(ps)
	want := map[string]string{
		store.AttemptRef(accepted, 1):  "superseded: another attempt was accepted",
		store.AttemptRef(replay, 1):    "accepted; its mission is not merged: a benchmark replay",
		store.AttemptRef(cancelled, 1): "its task is cancelled",
		store.AttemptRef(rejected, 1):  "its task is rejected",
	}
	if len(got) != len(want) {
		t.Fatalf("pruned %v; want %v", got, want)
	}
	for ref, why := range want {
		if !strings.HasPrefix(got[ref], why+" (") {
			t.Errorf("%s: %q; want %q", ref, got[ref], why)
		}
	}
	// A branch moved since names another commit than its attempt pushed.
	if moved, err := store.Prunable(ctx, w.rt, []store.AgentHead{head(cancelled, 1, commitB)}, nil, later); err != nil || len(moved) != 0 {
		t.Fatalf("a moved branch: %v %v", moved, err)
	}

	// The accepted attempt of M-3 goes seven days after its very commit is
	// first seen on main, and only while main still holds it (review 436).
	// Main holding another commit of the task, or not read, is no landing.
	for name, read := range map[string]store.MainResults{
		"another commit of the task": func(context.Context, string) (store.OnMain, bool) {
			return store.OnMain{accepted: {commitB: commitB}}, true
		},
		"main not read": func(context.Context, string) (store.OnMain, bool) { return nil, false },
	} {
		if n, err := store.RecordLandings(ctx, w.rt, read); err != nil || n != 0 {
			t.Fatalf("%s: %d landings (%v)", name, n, err)
		}
	}
	// Its result not seen on main: no host may say it deleted the branch.
	if err := store.RecordPrunes(ctx, w.rt, "h1", []store.PruneResult{{Repo: "bifrost-platform", Ref: store.AttemptRef(accepted, 2),
		Commit: commitA, Outcome: "deleted"}}, later); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("a report of the accepted attempt not on main: %v; want invalid", err)
	}
	if need, err := store.PruneMainsNeeded(ctx, w.rt, heads, later); err != nil || len(need) != 0 {
		t.Fatalf("mains needed before a landing: %v %v", need, err)
	}
	reads := 0
	onMain := store.OnMain{accepted: {commitA: commitB}, replay: {commitA: commitB}}
	mainResults := func(_ context.Context, repo string) (store.OnMain, bool) {
		reads++
		if repo != "bifrost-platform" {
			return nil, false
		}
		return onMain, true
	}
	if n, err := store.RecordLandings(ctx, w.rt, mainResults); err != nil || n != 1 || reads != 1 {
		t.Fatalf("landings: %d (%v), %d reads; want M-3's one (M-4 is not merged), main read once", n, err, reads)
	}
	if n, err := store.RecordLandings(ctx, w.rt, mainResults); err != nil || n != 0 {
		t.Fatalf("landings again: %d (%v)", n, err)
	}
	if need, err := store.PruneMainsNeeded(ctx, w.rt, heads, later); err != nil || len(need) != 1 || need[0] != "bifrost-platform" {
		t.Fatalf("mains needed a week after the landing: %v %v", need, err)
	}
	if need, err := store.PruneMainsNeeded(ctx, w.rt, heads, now.Add(time.Hour)); err != nil || len(need) != 0 {
		t.Fatalf("mains needed an hour after the landing: %v %v", need, err)
	}
	mains := map[string]store.OnMain{"bifrost-platform": onMain}
	ps, err = store.Prunable(ctx, w.rt, heads, mains, later)
	ok(t, err)
	if why := prunedRefs(ps)[store.AttemptRef(accepted, 2)]; !strings.HasPrefix(why, "accepted and on main as "+commitB[:12]+" (") {
		t.Fatalf("the accepted attempt after its landing: %q", why)
	}
	for name, m := range map[string]map[string]store.OnMain{"reverted since": {"bifrost-platform": {}}, "main not read now": nil} {
		ps, err := store.Prunable(ctx, w.rt, heads, m, later)
		ok(t, err)
		if why, given := prunedRefs(ps)[store.AttemptRef(accepted, 2)]; given {
			t.Errorf("%s: the accepted attempt is handed out (%q)", name, why)
		}
	}
	if ps, err = store.Prunable(ctx, w.rt, heads, mains, now.Add(time.Hour)); err != nil || len(ps) != 0 {
		t.Fatalf("an hour after the landing: %v %v", ps, err)
	}
}

// TD-383: what the host did is recorded: a deleted (or already gone) branch
// on its task's mission, once; a refusal on the host's stream, which pauses
// the handing out for PrunePause; Gitea out of reach likewise, for
// PruneRetry. A report that is not of an attempt as this runtime pushed it,
// of a branch the rule lets go, is refused whole (review 436 B2, 437 B2).
func TestRecordPrunes(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.goalMission(t, "M-3")
	task := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
	w.run(t, "h1", cursor, "done")
	open := w.ready(t, store.Task{MissionID: "M-3", OffPlanReason: "a goal"})
	w.run(t, "h2", cursor, "done")
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if err := store.MoveTaskForTest(ctx, tx, "controller", task, "blocked"); err != nil {
			return err
		}
		return store.MoveTaskForTest(ctx, tx, "controller", task, "cancelled")
	}))
	ref := store.AttemptRef(task, 1)
	now := time.Now()
	later := now.Add(store.PruneAfter + time.Hour)
	if until, err := store.PrunePausedUntil(ctx, w.rt, now); err != nil || !until.IsZero() {
		t.Fatalf("paused %v (%v) before any refusal", until, err)
	}
	// Cancelled a minute ago: the rule does not let it go yet (review 437 B2).
	for _, outcome := range []string{"deleted", "refused"} {
		r := store.PruneResult{Repo: "bifrost-platform", Ref: ref, Commit: commitA, Outcome: outcome}
		if err := store.RecordPrunes(ctx, w.rt, "h1", []store.PruneResult{r}, now); !errors.Is(err, store.ErrInvalid) {
			t.Errorf("a %s report the day of the cancel: %v; want invalid", outcome, err)
		}
	}
	for _, bad := range []store.PruneResult{
		{Repo: "bifrost-platform", Ref: "refs/heads/main", Commit: commitA, Outcome: "deleted"},
		{Repo: "bifrost-platform", Ref: ref, Commit: "HEAD", Outcome: "deleted"},
		{Repo: "bifrost-platform", Ref: ref, Commit: commitA, Outcome: "kept"},
		{Repo: "bifrost platform", Ref: ref, Commit: commitA, Outcome: "deleted"},
		{Repo: strings.Repeat("r", 101), Ref: ref, Commit: commitA, Outcome: "refused"},
		{Repo: "bifrost-ui", Ref: ref, Commit: commitA, Outcome: "deleted"},
		{Repo: "bifrost-platform", Ref: ref, Commit: commitB, Outcome: "deleted"},
		{Repo: "bifrost-platform", Ref: store.AttemptRef(task, 9), Commit: commitA, Outcome: "deleted"},
		{Repo: "bifrost-platform", Ref: store.AttemptRef(store.NewTaskID(), 1), Commit: commitA, Outcome: "refused"},
		{Repo: "bifrost-platform", Ref: store.AttemptRef(open, 1), Commit: commitA, Outcome: "deleted"},
		{Repo: "bifrost-platform", Ref: store.AttemptRef(open, 1), Commit: commitA, Outcome: "refused"},
	} {
		if err := store.RecordPrunes(ctx, w.rt, "h1", []store.PruneResult{bad}, later); !errors.Is(err, store.ErrInvalid) {
			t.Errorf("%+v: %v; want invalid", bad, err)
		}
	}
	if until, err := store.PrunePausedUntil(ctx, w.rt, now); err != nil || !until.IsZero() {
		t.Fatalf("paused %v (%v) after refused reports only", until, err)
	}
	deleted := store.PruneResult{Repo: "bifrost-platform", Ref: ref, Commit: commitA, Outcome: "deleted"}
	ok(t, store.RecordPrunes(ctx, w.rt, "h1", []store.PruneResult{deleted}, later))
	ok(t, store.RecordPrunes(ctx, w.rt, "h1", []store.PruneResult{deleted}, later)) // sent again: its answer was lost
	var pruned []string
	rows, err := w.rt.Query(ctx, `SELECT body_json->>'pruned' FROM agentrt.event WHERE event_type = 'note' AND task_id = $1
	  AND event_stream_id = 'mission:M-3' AND body_json ? 'pruned'`, task)
	ok(t, err)
	pruned, err = pgx.CollectRows(rows, pgx.RowTo[string])
	ok(t, err)
	if len(pruned) != 1 || pruned[0] != ref {
		t.Fatalf("pruned notes %v; want one of %s", pruned, ref)
	}

	// Gitea out of reach pauses for PruneRetry; a refusal for PrunePause.
	ok(t, store.RecordPrunes(ctx, w.rt, "h1", []store.PruneResult{{Repo: "bifrost-platform", Ref: ref, Commit: commitA, Outcome: "failed",
		Detail: "fatal: unable to access 'https://agentd:0123456789abcdef@gitea.example/x.git/': Could not resolve host"}}, later))
	until, err := store.PrunePausedUntil(ctx, w.rt, now)
	if err != nil || until.Before(now.Add(store.PruneRetry-time.Minute)) || until.After(now.Add(store.PruneRetry+time.Minute)) {
		t.Fatalf("paused until %v (%v) after a failure; want about %v", until, err, store.PruneRetry)
	}
	ok(t, store.RecordPrunes(ctx, w.rt, "h1", []store.PruneResult{{Repo: "bifrost-platform", Ref: ref, Commit: commitA, Outcome: "refused",
		Detail: "! [remote rejected] agent/x (pre-receive hook declined) | remote: Gitea: branch is protected"}}, later))
	var refused, failed string
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'text' FROM agentrt.event WHERE event_type = 'note' AND event_stream_id = 'host:h1'
	  AND body_json ? 'prune_refused'`).Scan(&refused))
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'text' FROM agentrt.event WHERE event_type = 'note' AND event_stream_id = 'host:h1'
	  AND body_json ? 'prune_failed'`).Scan(&failed))
	if !strings.Contains(refused, "Gitea refused to delete "+ref) || !strings.Contains(refused, "protected") ||
		!strings.Contains(failed, "could not reach Gitea") || strings.Contains(failed, "0123456789abcdef") {
		t.Fatalf("refused %q, failed %q", refused, failed)
	}
	until, err = store.PrunePausedUntil(ctx, w.rt, now)
	if err != nil || until.Before(now.Add(store.PrunePause-time.Minute)) {
		t.Fatalf("paused until %v (%v); want about %v", until, err, store.PrunePause)
	}
	if until, err = store.PrunePausedUntil(ctx, w.rt, now.Add(store.PrunePause+time.Minute)); err != nil || !until.IsZero() {
		t.Fatalf("after the pause: %v (%v)", until, err)
	}
}

// TD-383: an attempt branch whose task is over goes a day after the event
// that let it go (Owner 10-20 14:4x), not seven; this keeps the Owner's
// number from drifting back.
func TestPruneAfterIsADay(t *testing.T) {
	t.Parallel()
	if store.PruneAfter != 24*time.Hour {
		t.Fatalf("PruneAfter = %s, the Owner set a day", store.PruneAfter)
	}
}
