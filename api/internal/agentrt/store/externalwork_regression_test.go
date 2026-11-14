package store_test

import (
	"context"
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
