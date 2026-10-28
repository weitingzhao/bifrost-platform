package store_test

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func newLandingRecord(op, kind string, kv ...any) map[string]any {
	r := landingRecord(op, kind, kv...)
	r["v"] = landing.Version
	return r
}

// Serial: FreezeEventClock. Real migrated PG, runtime role, every new edge and
// refusals from every landing phase; no in-memory database substitute.
func TestLandingV11TransitionMatrixDatabase(t *testing.T) {
	w := newWorld(t)
	at := time.Now().Add(-time.Hour)
	t.Cleanup(store.FreezeEventClock(at))
	phases := []string{"empty", "claimed", "prepared", "branch_intent", "branch_pushed", "green", "parked", "failed", "discarded", "rehearsed", "main_intent", "main_result", "landed", "reverted"}
	number := 0
	for _, phase := range phases {
		for _, kind := range []string{"discarded", "rehearsed", "retry"} {
			t.Run(phase+"/"+kind, func(t *testing.T) {
				store.FreezeEventClock(at)
				number++
				op := fmt.Sprintf("matrix:%d", number)
				root := fmt.Sprintf("t_%026d", number)
				dry := !slices.Contains([]string{"main_intent", "main_result", "landed", "reverted"}, phase)
				put := func(k string, kv ...any) {
					t.Helper()
					ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, k, kv...)))
				}
				if phase != "empty" {
					put("claimed", "task", root, "attempt", landingAttempt, "A", commitA, "rule_version", 1)
					if phase != "claimed" {
						put("prepared", "R", commitB, "M0", commitA, "committer_date", at.Format(time.RFC3339Nano), "dry_run", dry)
						if phase != "prepared" {
							env := "stg"
							if !dry {
								env = "prod"
							}
							ref := "refs/heads/land/" + env + "/" + root + "/1"
							put("branch_intent", "ref", ref, "R", commitB)
							if phase != "branch_intent" {
								put("branch_pushed", "ref", ref, "R", commitB)
								switch phase {
								case "parked":
									put("parked", "why", "main_red", "retry_at", at.Add(time.Minute).Format(time.RFC3339Nano))
								case "failed":
									put("failed", "why", "bound")
								case "discarded":
									put("discarded", "why", "main_changed")
								case "branch_pushed":
								default:
									restore := store.FreezeEventClock(at.Add(2 * time.Second))
									put("ci_seen", "R", commitB, "run", "green", "status", "Succeeded", "created_at", at.Add(time.Second).Format(time.RFC3339Nano))
									if phase == "rehearsed" {
										put("rehearsed", "R", commitB)
									} else if phase != "green" {
										put("main_intent", "R", commitB, "M0", commitA)
										if phase != "main_intent" {
											put("main_result", "outcome", "ok")
											if phase != "main_result" {
												put("landed", "R", commitB)
												if phase == "reverted" {
													put("reverted", "R", commitB, "by", commitA)
												}
											}
										}
									}
									restore()
								}
							}
						}
					}
				}
				r := newLandingRecord(op, "discarded", "why", "branch_collision")
				want := slices.Contains([]string{"branch_intent", "branch_pushed", "green", "parked"}, phase)
				if kind == "rehearsed" {
					r = newLandingRecord(op, kind, "R", commitB)
					want = phase == "green"
				}
				if kind == "retry" {
					r = newLandingRecord(op, kind, "root", root, "after_op", op, "reason", "explicit retry", "by", "owner")
					want = phase == "failed"
				}
				restore := store.FreezeEventClock(at.Add(time.Minute))
				defer restore()
				err := appendLanding(w, landingRepoStream, r)
				if (err == nil) != want {
					t.Fatalf("success=%v err=%v", want, err)
				}
				if want && kind != "retry" {
					o := landingState(t, w, landingRepoStream).Operations[op]
					if o.Active() || !o.Terminal() || o.BlocksClaim() || o.EverLanded || o.Reservable(root) {
						t.Fatal(o)
					}
				}
			})
		}
	}
	_, err := store.VerifyStream(ctx, w.rt, landingRepoStream)
	ok(t, err)
}

// Serial: FreezeEventClock.
func TestLandingV11RetryAnswersDatabase(t *testing.T) {
	// Answers lock the root's task and mission (review 645 R1): the roots
	// are real tasks of an active mission.
	w := newKernelWorld(t)
	at := time.Now().Add(-7 * time.Hour)
	t.Cleanup(store.FreezeEventClock(at))
	ctl := controller.Controller{DB: w.rt, Env: "stg"}
	reasons := []string{"bound", "stale", "ci_red", "ci_failed", "declined", "conflict", "patch_changed", "overlap", "floor", "limit", "denied", "rule", "main_red", "ci_stuck", "window"}
	for i, why := range reasons {
		store.FreezeEventClock(at)
		op := fmt.Sprintf("retry:%d", i)
		root := w.ready(t, store.Task{OffPlanReason: fmt.Sprintf("answer %d", i)})
		ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "claimed", "task", root, "attempt", landingAttempt, "A", commitA, "rule_version", 1)))
		ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "failed", "why", why)))
		q := store.Question{ID: fmt.Sprintf("q_retry_%d", i), Status: store.QuestionAnswered, Channel: "console", Option: "try_again", Text: "owner retry", AnsweredBy: "owner", AnsweredAt: at.Add(7 * time.Hour).Format(time.RFC3339Nano)}
		read := func(_ context.Context, r, a string) (store.Question, bool) {
			if r != root || a != op {
				t.Fatal("wrong binding")
			}
			return q, true
		}
		restore := store.FreezeEventClock(at.Add(7 * time.Hour))
		res, err := ctl.LandingAnswer(ctx, root, op, read)
		ok(t, err)
		retryable := slices.Contains([]string{"bound", "stale", "ci_red"}, why)
		// A failure the protocol does not retry is a recorded refusal, not an
		// error (CUT §8).
		if (res.Outcome == store.LandingAnswerApplied) != retryable || (!retryable && res.Why != "not_retryable") {
			t.Fatal(why, res)
		}
		if retryable {
			again, err := ctl.LandingAnswer(ctx, root, op, read) // same answer is idempotent
			ok(t, err)
			if again.Recorded {
				t.Fatal("the same card was handled twice", again)
			}
			state := landingState(t, w, landingRepoStream)
			first, n := landing.RootBudget(state, root)
			if !first.Equal(at.Add(7*time.Hour).Truncate(time.Microsecond)) || n != 1 || state.Operations[op].BlocksClaim() {
				t.Fatal(first, n, state.Operations[op])
			}
			next := op + ":2"
			ok(t, appendLanding(w, landingRepoStream, newLandingRecord(next, "claimed", "task", root, "attempt", landingAttempt, "A", commitA, "rule_version", 1)))
			q.ID += "_late" // a new card still bound to the old operation
			if late, err := ctl.LandingAnswer(ctx, root, op, read); err != nil || late.Outcome != store.LandingAnswerMoot || late.Why != "superseded" {
				t.Fatal("old question applied to new operation", late, err)
			}
			if landingState(t, w, landingRepoStream).Retries[root].ConsumedBy != next {
				t.Fatal("not consumed")
			}
		}
		restore()
	}
	store.FreezeEventClock(at)
	// No answer, rule answer or an unrelated answer must not write retry.
	op := "answer:decline"
	root := w.ready(t, store.Task{OffPlanReason: "answer decline"})
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "claimed", "task", root, "attempt", landingAttempt, "A", commitA, "rule_version", 1)))
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "failed", "why", "bound")))
	q := store.Question{ID: "q_decline", Status: store.QuestionAnswered, Channel: "rule", Option: "try_again", AnsweredBy: "owner", AnsweredAt: time.Now().Format(time.RFC3339Nano)}
	read := func(context.Context, string, string) (store.Question, bool) { return q, true }
	if res, err := ctl.LandingAnswer(ctx, root, op, read); err != nil || res.Outcome != "" {
		t.Fatal("rule answer handled", res, err)
	}
	if !landingState(t, w, landingRepoStream).Operations[op].BlocksClaim() {
		t.Fatal("rule retry")
	}
	q.Channel = "console"
	q.Option = "do_not_merge"
	if res, err := ctl.LandingAnswer(ctx, root, op, read); err != nil || res.Outcome != store.LandingAnswerApplied {
		t.Fatal("decline", res, err)
	}
	q.ID, q.Option = "q_decline_retry", "try_again"
	if res, err := ctl.LandingAnswer(ctx, root, op, read); err != nil || res.Outcome != store.LandingAnswerRefused || res.Why != "not_retryable" {
		t.Fatal("declined retry", res, err)
	}
	// Non-controller append is rejected even with valid fields and transition.
	op = "actor:1"
	root = "t_00000000000000000000000999"
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "claimed", "task", root, "attempt", landingAttempt, "A", commitA, "rule_version", 1)))
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "failed", "why", "bound")))
	err := store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, e := store.Append(ctx, tx, store.Event{Stream: landingRepoStream, Type: "note", Actor: "owner", Body: map[string]any{"text": "invalid actor", "landing": newLandingRecord(op, "retry", "root", root, "after_op", op, "reason", "again", "by", "owner")}})
		return e
	})
	if err == nil {
		t.Fatal("non-controller retry")
	}
}

// Serial: FreezeEventClock. Malformed fields, wrong bindings, and stale CI are
// refused through Append, not only through the pure validator.
func TestLandingV11RefusesBadEvidenceDatabase(t *testing.T) {
	w := newWorld(t)
	at := time.Now().Add(-time.Hour)
	t.Cleanup(store.FreezeEventClock(at))
	op := "evidence:1"
	ref := "refs/heads/land/stg/" + landingTask + "/1"
	for _, r := range []map[string]any{newLandingRecord(op, "claimed", "task", landingTask, "attempt", landingAttempt, "A", commitA, "rule_version", 1), newLandingRecord(op, "prepared", "R", commitB, "M0", commitA, "committer_date", at.Format(time.RFC3339Nano), "dry_run", true), newLandingRecord(op, "branch_intent", "ref", ref, "R", commitB), newLandingRecord(op, "branch_pushed", "ref", ref, "R", commitB)} {
		ok(t, appendLanding(w, landingRepoStream, r))
	}
	store.FreezeEventClock(at.Add(time.Minute))
	for _, status := range []string{"Failed", "Unknown", "Running"} {
		ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "ci_seen", "R", commitB, "run", "latest", "status", status, "created_at", at.Add(time.Second).Format(time.RFC3339Nano))))
		if appendLanding(w, landingRepoStream, newLandingRecord(op, "rehearsed", "R", commitB)) == nil {
			t.Fatal(status)
		}
	}
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "ci_seen", "R", commitB, "run", "latest", "status", "Succeeded", "created_at", at.Add(time.Second).Format(time.RFC3339Nano))))
	if appendLanding(w, landingRepoStream, newLandingRecord(op, "rehearsed", "R", commitA)) == nil {
		t.Fatal("wrong R")
	}
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "ci_rerun", "try", 2)))
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "ci_seen", "R", commitB, "run", "latest", "status", "Succeeded", "created_at", at.Add(time.Second).Format(time.RFC3339Nano))))
	if appendLanding(w, landingRepoStream, newLandingRecord(op, "rehearsed", "R", commitB)) == nil {
		t.Fatal("success before retry")
	}
	for _, r := range []map[string]any{landingRecord(op, "discarded", "why", "main_changed"), newLandingRecord(op, "discarded", "why", "stale"), newLandingRecord(op, "rehearsed", "R", "short")} {
		if appendLanding(w, landingRepoStream, r) == nil {
			t.Fatal("bad fields", r)
		}
	}
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "failed", "why", "bound")))
	for _, r := range []map[string]any{newLandingRecord(op, "retry", "root", "t_00000000000000000000000001", "after_op", op, "reason", "again", "by", "owner"), newLandingRecord(op, "retry", "root", landingTask, "after_op", "missing", "reason", "again", "by", "owner")} {
		if appendLanding(w, landingRepoStream, r) == nil {
			t.Fatal("bad binding")
		}
	}
	retry := newLandingRecord(op, "retry", "root", landingTask, "after_op", op, "reason", "again", "by", "owner")
	ok(t, appendLanding(w, landingRepoStream, retry))
	if appendLanding(w, landingRepoStream, retry) == nil {
		t.Fatal("duplicate retry")
	}
}

// Serial: FreezeEventClock.
func TestLandingV11LiveCannotRehearseDatabase(t *testing.T) {
	w := newKernelWorld(t)
	root := w.ready(t, store.Task{OffPlanReason: "live"}) // answers lock the root's task (review 645 R1)
	at := time.Now().Add(-time.Hour)
	t.Cleanup(store.FreezeEventClock(at))
	op := "live:1"
	ref := "refs/heads/land/prod/" + root + "/1"
	for _, r := range []map[string]any{newLandingRecord(op, "claimed", "task", root, "attempt", landingAttempt, "A", commitA, "rule_version", 1), newLandingRecord(op, "prepared", "R", commitB, "M0", commitA, "committer_date", at.Format(time.RFC3339Nano), "dry_run", false), newLandingRecord(op, "branch_intent", "ref", ref, "R", commitB), newLandingRecord(op, "branch_pushed", "ref", ref, "R", commitB)} {
		ok(t, appendLanding(w, landingRepoStream, r))
	}
	store.FreezeEventClock(at.Add(time.Minute))
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "ci_seen", "R", commitB, "run", "green", "status", "Succeeded", "created_at", at.Add(time.Second).Format(time.RFC3339Nano))))
	if appendLanding(w, landingRepoStream, newLandingRecord(op, "rehearsed", "R", commitB)) == nil {
		t.Fatal("live rehearsal")
	}
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "parked", "why", "window", "retry_at", at.Add(2*time.Minute).Format(time.RFC3339Nano))))
	ctl := controller.Controller{DB: w.rt, Env: "stg"}
	_, err := ctl.LandingAnswer(ctx, root, op, func(context.Context, string, string) (store.Question, bool) {
		return store.Question{ID: "q_no", Status: store.QuestionAnswered, Channel: "console", Option: "do_not_merge", AnsweredBy: "owner", AnsweredAt: at.Add(3 * time.Minute).Format(time.RFC3339Nano)}, true
	})
	ok(t, err)
	if o := landingState(t, w, landingRepoStream).Operations[op]; o.Phase != "failed" || o.Outcome != "declined" {
		t.Fatal("parked not declined", o)
	}
}
