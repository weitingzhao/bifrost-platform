package migrate_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Check actual DML as the runtime role on the upgraded (not fresh) schema.
// Positive controls make permission/event-guard failures distinguishable
// from the three new trigger refusals.
func checkRuntime0010(t *testing.T, db *agentrttest.DB) {
	t.Helper()
	ctx := context.Background()
	rt := db.Connect(t, db.Runtime)
	var who string
	must0010(t, rt.QueryRow(ctx, `SELECT current_user`).Scan(&who))
	if who != "platform_agentrt_dev" {
		t.Fatalf("runtime probe is %s", who)
	}
	var spec, plain string
	must0010(t, rt.QueryRow(ctx, `SELECT task_id, spec_artifact_id FROM agentrt.task WHERE title='proposed'`).Scan(&plain, &spec))
	for _, function := range []string{"host_kind_immutable", "pin_immutable", "attempt_pin_guard"} {
		var allowed bool
		must0010(t, rt.QueryRow(ctx, `SELECT has_function_privilege(current_user, $1, 'EXECUTE')`, "agentrt."+function+"()").Scan(&allowed))
		if !allowed {
			t.Errorf("runtime cannot execute %s", function)
		}
	}
	insert := func(eventPin, pin, role string) (string, error) {
		id := store.NewTaskID()
		err := store.InTx(ctx, rt, func(tx pgx.Tx) error {
			body := map[string]any{"title": "pin probe", "role": role}
			if eventPin != "" {
				body["executor_pin"] = eventPin
			}
			if _, err := store.Append(ctx, tx, store.Event{Stream: store.MissionStream("M-1"), Type: "task_proposed", Actor: "controller",
				MissionID: "M-1", TaskID: id, Body: body}); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `INSERT INTO agentrt.task (task_id, mission_id, off_plan, off_plan_reason, role, task_class, title,
			  spec_artifact_id, effect_class, state, hard_limit_s, created_by_actor_id, executor_pin)
			  VALUES ($1, 'M-1', true, 'T50 trigger probe', $2, 'go', 'pin probe', $3, 'branch', 'proposed', 3600, 'controller', $4)`,
				id, role, spec, pin)
			return err
		})
		return id, err
	}
	const pin = "external:coordinator"
	_, err := insert("", pin, "implement")
	pgError0010(t, err, "23514", "its task_proposed event records none")
	_, err = insert(pin, pin, "other")
	pgError0010(t, err, "23514", "task_executor_pin_check")
	pinned, err := insert(pin, pin, "implement")
	must0010(t, err)
	var got string
	must0010(t, rt.QueryRow(ctx, `SELECT executor_pin FROM agentrt.task WHERE task_id=$1`, pinned).Scan(&got))
	if got != pin {
		t.Fatalf("runtime read pin=%q", got)
	}
	update := func(sql string, args ...any) error {
		return store.InTx(ctx, rt, func(tx pgx.Tx) error {
			if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
				Body: map[string]any{"source": "T50 runtime trigger probe"}}); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, sql, args...)
			return err
		})
	}
	must0010(t, update(`UPDATE agentrt.task SET title='renamed' WHERE task_id=$1`, pinned))
	pgError0010(t, update(`UPDATE agentrt.task SET executor_pin=NULL WHERE task_id=$1`, pinned), "23514", "keeps its executor pin")
	must0010(t, update(`UPDATE agentrt.host SET state='draining' WHERE host_id='old-dev'`))
	pgError0010(t, update(`UPDATE agentrt.host SET host_kind='external' WHERE host_id='old-dev'`), "23514", "keeps its kind")
	must0010(t, store.RegisterHost(ctx, rt, "owner", "t50-external", "external"))
	pgError0010(t, store.RegisterHost(ctx, rt, "owner", "t50-second", "external"), "23505", "host_one_external")
	lease := func(task, host string) error {
		return store.InTx(ctx, rt, func(tx pgx.Tx) error {
			_, _, err := store.LeaseAttempt(ctx, tx, "controller", "M-1", task, "exec:codex", host, 1, time.Minute)
			return err
		})
	}
	pgError0010(t, lease(pinned, "old-vm"), "23514", "runs only on the external host")
	pgError0010(t, lease(plain, "t50-external"), "23514", "runs only on the external host")
	must0010(t, lease(pinned, "t50-external"))
	must0010(t, lease(plain, "old-vm"))
	_, err = rt.Exec(ctx, `ALTER TABLE agentrt.task DISABLE TRIGGER task_pin_immutable`)
	pgError0010(t, err, "42501", "must be owner")
}
