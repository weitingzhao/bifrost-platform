package store_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func TestReview596ExternalRegistrationSerializesDevClaim(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	spec := externalSpec(t, w)
	w.legacy(t, `UPDATE agentrt.mission SET tenant='business' WHERE mission_id='M-2'`)
	tx, err := w.rt.Begin(ctx)
	ok(t, err)
	defer tx.Rollback(ctx)
	taskID := store.NewTaskID()
	_, err = store.ProposeTask(ctx, tx, "controller", store.Task{ID: taskID, MissionID: "M-2", PlanNode: s2, Role: "implement", Class: "go", Title: "business task", SpecArtifact: w.spec, Effect: "branch"})
	ok(t, err)
	conn := w.db.Connect(t, w.db.Runtime)
	observer := w.db.Connect(t, w.db.Admin)
	raceCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, e := store.RegisterExternalWork(raceCtx, conn, wr("thread:race-dev"), store.ExternalRegistration{Key: "reg", Root: root, Node: s1, RunKey: "race-dev", Artifact: spec})
		done <- e
	}()
	// Wait for the database to confirm the lock wait, not an assumed delay.
	for {
		select {
		case err = <-done:
			ok(t, tx.Commit(ctx))
			goto completed
		default:
		}
		var blocked bool
		ok(t, observer.QueryRow(raceCtx, `SELECT $1::int=ANY(pg_blocking_pids($2::int))`, w.rt.PgConn().PID(), conn.PgConn().PID()).Scan(&blocked))
		if blocked {
			ok(t, tx.Commit(ctx))
			err = <-done
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
completed:
	if err == nil {
		n := w.count(t, `SELECT count(*) FROM agentrt.task t JOIN agentrt.mission m USING(mission_id) WHERE t.plan_node_id=$1 AND m.tenant='business'`, s2)
		t.Errorf("registration succeeded with concurrently committed business tasks in dev-claimed root: %d", n)
	} else {
		is(t, err, store.ErrInvalid)
	}
}

func TestReview596SubmittedExternalDoesNotHideRuntime(t *testing.T) {
	t.Parallel()
	for _, terminal := range []string{"submitted", "cancelled"} {
		t.Run(terminal, func(t *testing.T) {
			w := newKernelWorld(t)
			task := w.ready(t, store.Task{PlanNode: s1})
			claim := w.claim(t, "h1", time.Minute, codex)
			if claim == nil {
				t.Fatal("fixture did not claim")
			}
			f := fence(claim, "h1")
			ok(t, store.Start(ctx, w.rt, f, codex.Model, codex.Provider))
			who := wr("thread:handoff")
			out, err := store.RegisterExternalWork(ctx, w.rt, who, store.ExternalRegistration{Key: "reg", Root: root, Node: s1, RunKey: "handoff", Artifact: externalSpec(t, w), Mission: "M-1", Task: task})
			ok(t, err)
			id := out["external_work_id"].(string)
			check := func(state string) {
				t.Helper()
				view, err := store.PlanView(ctx, w.rt, root)
				ok(t, err)
				raw, err := json.Marshal(view)
				ok(t, err)
				var got struct {
					Working []struct {
						Attempt  *store.ProgressAttempt `json:"attempt"`
						External []struct {
							State string `json:"state"`
						} `json:"external_work"`
					} `json:"working"`
				}
				ok(t, json.Unmarshal(raw, &got))
				if len(got.Working) != 1 {
					t.Fatalf("live attempt disappeared or duplicated: %s", raw)
				}
				a := got.Working[0].Attempt
				if a == nil || a.AttemptID != claim.AttemptID || a.State != "running" || a.Executor != codex.Executor || a.HostID != "h1" || a.HeartbeatAgeS < 0 {
					t.Fatalf("runtime execution facts lost: %+v", a)
				}
				if len(got.Working[0].External) != 1 || got.Working[0].External[0].State != state {
					t.Fatalf("external history lost: %s", raw)
				}
			}
			check("Registered")
			_, err = store.PostExternalEvent(ctx, w.rt, who, id, store.ExternalEvent{Key: "start", Seq: 1, Type: "started"})
			ok(t, err)
			check("Running")
			_, err = store.PostExternalEvent(ctx, w.rt, who, id, store.ExternalEvent{Key: "end", Seq: 2, Type: terminal})
			ok(t, err)
			state := "Submitted"
			if terminal == "cancelled" {
				state = "Cancelled"
			}
			check(state)
			// A runtime attempt ending today remains separately identifiable;
			// an external observation does not carry an attempt identity.
			_, err = store.Ended(ctx, w.rt, claim.AttemptID, "h1", "exit", true)
			ok(t, err)
			view, err := store.PlanView(ctx, w.rt, root)
			ok(t, err)
			if len(view["working"].([]any)) != 0 {
				t.Fatal("terminal work remains active")
			}
			found := 0
			for _, e := range view["finished_today"].([]any) {
				if e.(map[string]any)["attempt_id"] == claim.AttemptID {
					found++
				}
			}
			if found != 1 {
				t.Fatalf("ended attempt hidden or duplicated: %v", view["finished_today"])
			}
		})
	}
}

func TestReview596CanLinkTaskOnUnboundDevStep(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"register", "linked"} {
		t.Run(mode, func(t *testing.T) {
			w := newKernelWorld(t)
			task, err := w.task(t, "M-1", s2, "", "implement")
			ok(t, err)
			who := wr("thread:unbound")
			in := store.ExternalRegistration{Key: "reg", Root: root, Node: s2, RunKey: "unbound", Artifact: externalSpec(t, w), Task: task}
			if mode == "linked" {
				in.Node, in.Task = s1, ""
			}
			out, err := store.RegisterExternalWork(ctx, w.rt, who, in)
			ok(t, err)
			if mode == "linked" {
				_, err = store.PostExternalEvent(ctx, w.rt, who, out["external_work_id"].(string), store.ExternalEvent{Key: "link", Seq: 1, Type: "linked", Root: root, Node: s2, Task: task})
				ok(t, err)
			}
			// Mission-only requests still require an explicit step binding.
			in.Key, in.RunKey, in.Node, in.Task, in.Mission = "mission-only", "mission-only", s2, "", "M-1"
			_, err = store.RegisterExternalWork(ctx, w.rt, who, in)
			is(t, err, store.ErrInvalid)
			in.Key, in.RunKey, in.Task, in.Mission = "wrong-mission", "wrong-mission", task, "M-2"
			_, err = store.RegisterExternalWork(ctx, w.rt, who, in)
			is(t, err, store.ErrInvalid)
			in.Key, in.RunKey, in.Node, in.Mission = "wrong-step", "wrong-step", s1, ""
			_, err = store.RegisterExternalWork(ctx, w.rt, who, in)
			is(t, err, store.ErrInvalid)
		})
	}
}

func TestExternalRegistrationRejectsLaterBusinessBinding(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.legacy(t, `UPDATE agentrt.mission SET tenant='business' WHERE mission_id='M-2'`)
	_, err := store.RegisterExternalWork(ctx, w.rt, wr("thread:dev-first"), store.ExternalRegistration{Key: "reg", Root: root, Node: s1, RunKey: "dev-first", Artifact: externalSpec(t, w)})
	ok(t, err)
	_, err = w.task(t, "M-2", s2, "", "implement")
	is(t, err, store.ErrInvalid)
}

func TestReview596LinkedExternalDoesNotHideEndedRuntime(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	task := w.ready(t, store.Task{PlanNode: s1})
	claim := w.claim(t, "h1", time.Minute, codex)
	if claim == nil {
		t.Fatal("fixture did not claim")
	}
	ok(t, store.Start(ctx, w.rt, fence(claim, "h1"), codex.Model, codex.Provider))
	_, err := store.Ended(ctx, w.rt, claim.AttemptID, "h1", "exit", true)
	ok(t, err)
	_, err = store.RegisterExternalWork(ctx, w.rt, wr("thread:ended"), store.ExternalRegistration{Key: "reg", Root: root, Node: s1, RunKey: "ended", Artifact: externalSpec(t, w), Task: task})
	ok(t, err)
	view, err := store.PlanView(ctx, w.rt, root)
	ok(t, err)
	ended := view["finished_today"].([]any)
	if len(ended) != 1 || ended[0].(map[string]any)["attempt_id"] != claim.AttemptID {
		t.Fatalf("linked external work hid ended runtime attempt: %v", ended)
	}
}
