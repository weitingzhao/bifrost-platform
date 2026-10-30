package server

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

const dep708 = "t_00000000000000000000000002"

func must708(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func ruleText708(at time.Time, version int, infra bool) string {
	direction := "initial"
	if version > 1 {
		direction = "tighten"
	}
	body := " - repo: bifrost-platform\n   allow: [\"console/src/components/**/*.tsx\"]\n   deny: []\n   stg_release: none\n"
	if infra {
		body += " - repo: bifrost-trade-infra\n   allow: [\"agent-config/work/multi-agent/REPORT-*.md\"]\n   deny: []\n   stg_release: none\n"
	}
	return fmt.Sprintf("rule_id: landing:dev\nrule_version: %d\ndirection: %s\neffective_at: %s\nexpires_at: %s\ntenant: dev\nrepos:\n%slimits: {max_files: 10, max_changed_lines: 3000, max_landings_per_day: 5, max_in_flight_per_repo: 1}\n",
		version, direction, at.Add(-2*time.Hour).Format(time.RFC3339), at.Add(time.Hour).Format(time.RFC3339), body)
}

func sign708(t *testing.T, db store.DB, at time.Time) {
	t.Helper()
	key := rptest.NewKey(t)
	text := []byte(ruleText708(at, 1, true))
	_, err := store.SignRule(context.Background(), db, store.Writer{Actor: "owner", Token: "admin"}, "landing:dev", text,
		key.Sign(t, text, rules.Namespace), key.Fingerprint)
	must708(t, err)
}

// Imported accepted rows isolate this regression from dispatch and verification
// scheduling. Real migrations, guards, signatures, event appends, transactions
// and row locks stay enabled. The gate must finish, not merely reach a callback.
func fixture708(t *testing.T, db *agentrttest.DB, repo, dep, env string, at time.Time) {
	t.Helper()
	ctx := context.Background()
	_, err := store.Seed(ctx, db.Connect(t, db.Owner))
	must708(t, err)
	rt := db.Connect(t, db.Runtime)
	sign708(t, rt, at)
	appendEvent := func(ev store.Event) {
		t.Helper()
		must708(t, store.InTx(ctx, rt, func(tx pgx.Tx) error { _, err := store.Append(ctx, tx, ev); return err }))
	}
	// Establish the fence before creating the mission, using the actual
	// event clock; the final gate checks both creation and acceptance times.
	for _, rec := range []map[string]any{
		{"v": landing.Version, "kind": "mode", "op": "mode:live", "repo": repo},
		{"v": landing.Version, "kind": "enabled", "op": "enabled:dev", "repo": repo, "rule_version": 1},
	} {
		appendEvent(store.Event{Stream: "landing:" + repo, Type: "note", Actor: "controller", Body: map[string]any{"text": "Review 708", "landing": rec}})
	}
	must708(t, store.InTx(ctx, rt, func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller", Body: map[string]any{"source": "review708 fixture"}}); err != nil {
			return err
		}
		spec, err := store.PutArtifact(ctx, tx, "controller", "task_spec", "text/markdown", []byte("# Review 708\n"))
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO agentrt.mission (mission_id,title,goal,success_json,scope_json,state,deadline_at,budget_json,created_by_actor_id)
          VALUES ('M-3','Review 708','Pool regression','{}','{}','active',$1,'{}','controller')`, at.Add(time.Hour)); err != nil {
			return err
		}
		tasks := []struct{ task, repo, attempt, verification string }{{task708, repo, attempt708, "v_00000000000000000000000001"}}
		if dep != "" {
			tasks = append(tasks, struct{ task, repo, attempt, verification string }{dep708, dep, "a_00000000000000000000000002", "v_00000000000000000000000002"})
		}
		for _, task := range tasks {
			if _, err = tx.Exec(ctx, `INSERT INTO agentrt.task (task_id,mission_id,off_plan,off_plan_reason,role,task_class,title,spec_artifact_id,repo,base_commit,effect_class,state,hard_limit_s,created_by_actor_id)
              VALUES ($1,'M-3',true,'pool regression','implement','go','Review 708',$2,$3,$4,'branch','accepted',3600,'controller')`, task.task, spec, task.repo, strings.Repeat("b", 40)); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO agentrt.attempt (attempt_id,task_id,attempt_no,executor_actor_id,lease_epoch,lease_expires_at,heartbeat_at,state,persisted_at,pushed_ref,pushed_commit)
              VALUES ($1,$2,1,'exec:codex',1,$3,$3,'finished',$3,$4,$5)`, task.attempt, task.task, at.Add(-6*time.Minute), store.AttemptRef(task.task, 1), strings.Repeat("a", 40)); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO agentrt.verification (verification_id,task_id,attempt_id,method,verdict,evidence_artifact_id,accepts_attempt)
              VALUES ($1,$2,$3,'review','pass',$4,true)`, task.verification, task.task, task.attempt, spec); err != nil {
				return err
			}
		}
		if dep != "" {
			_, err = tx.Exec(ctx, `INSERT INTO agentrt.task_dependency (task_id,depends_on_task_id) VALUES ($1,$2)`, task708, dep708)
		}
		return err
	}))
	appendEvent(store.Event{Stream: "mission:M-3", Type: "task_state_changed", Actor: "controller", TaskID: task708, MissionID: "M-3",
		Body: map[string]any{"from": "verifying", "to": "accepted", "reason": "review708 fixture"}})
	// Diagnose the actual operands before the pool/locking regression runs.
	// effective_at is not the eligibility fence. Do not rewrite imported
	// created_at to hide a database/event-clock ordering failure (TD-456).
	must708(t, store.InTx(ctx, rt, func(tx pgx.Tx) error {
		state, err := store.LandingState(ctx, tx, "landing:"+repo)
		if err != nil {
			return err
		}
		var databaseNow, created, accepted time.Time
		if err := tx.QueryRow(ctx, `SELECT now(), m.created_at,
          (SELECT max(at) FROM agentrt.event WHERE task_id=$1 AND event_type='task_state_changed' AND body_json->>'to'='accepted')
          FROM agentrt.mission m WHERE mission_id='M-3'`, task708).Scan(&databaseNow, &created, &accepted); err != nil {
			return err
		}
		fence := state.Fence()
		if fence.IsZero() || fence.After(databaseNow) || created.Before(fence) || accepted.Before(fence) {
			return fmt.Errorf("Review 708 fixture clock order: enabled=%s live=%s fence=%s database_now=%s mission=%s accepted=%s event_now=%s rule_effective=%s",
				state.EnabledAt, state.LiveAt, fence, databaseNow, created, accepted, store.DecisionAt(), at.Add(-2*time.Hour).Truncate(time.Second))
		}
		return nil
	}))
	ref := "refs/heads/land/" + env + "/" + task708 + "/1"
	for _, rec := range []map[string]any{
		{"v": landing.Version, "kind": "claimed", "op": op708, "task": task708, "attempt": attempt708, "A": strings.Repeat("a", 40), "rule_version": 1},
		{"v": landing.Version, "kind": "prepared", "op": op708, "R": strings.Repeat("d", 40), "M0": strings.Repeat("b", 40), "committer_date": at.Add(-3 * time.Minute).Format(time.RFC3339Nano), "dry_run": env == "stg"},
		{"v": landing.Version, "kind": "branch_intent", "op": op708, "R": strings.Repeat("d", 40), "ref": ref},
		{"v": landing.Version, "kind": "branch_pushed", "op": op708, "R": strings.Repeat("d", 40), "ref": ref},
	} {
		appendEvent(store.Event{Stream: "landing:" + repo, Type: "note", Actor: "controller", Body: map[string]any{"text": "Review 708", "landing": rec}})
	}
}

type pid708DB struct {
	*pgxpool.Pool
	pid chan uint32
}

func (d pid708DB) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	tx, err := d.Pool.BeginTx(ctx, opts)
	if err == nil {
		d.pid <- tx.Conn().PgConn().PID()
	}
	return tx, err
}

// Use pg_blocking_pids, not a sleep or just pool occupancy, to establish that
// the real reconciler holds connection B and waits for gate A's stream lock.
// The observer connection is outside the two-slot pool used by both workers.
func waitBlocked708(ctx context.Context, observer *pgx.Conn, gate, reconcile uint32) error {
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		var blocked bool
		if err := observer.QueryRow(ctx, `SELECT $1::int = ANY(pg_blocking_pids($2::int))`, gate, reconcile).Scan(&blocked); err != nil {
			return err
		}
		if blocked {
			return nil
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Serial: keep the two-connection contention regression isolated from package load.
func TestReview708FinalGatesTwoConnectionsPG(t *testing.T) {
	for _, env := range []string{"stg", "prod"} {
		for _, tc := range []struct{ name, repo, dep string }{
			{"platform", "bifrost-platform", ""},
			{"infra", "bifrost-trade-infra", ""},
			{"platform-depends-on-infra", "bifrost-platform", "bifrost-trade-infra"},
		} {
			t.Run(env+"/"+tc.name, func(t *testing.T) {
				db := agentrttest.Migrated(t)
				at := time.Now().UTC().Truncate(time.Second)
				fixture708(t, db, tc.repo, tc.dep, env, at)
				cfg, err := pgxpool.ParseConfig(db.Runtime)
				must708(t, err)
				cfg.MaxConns = 2
				pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
				must708(t, err)
				defer pool.Close()
				observer := db.Connect(t, db.Admin)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				gate := pid708DB{pool, make(chan uint32, 1)}
				reconcile := pid708DB{pool, make(chan uint32, 1)}
				done := make(chan error, 1)
				started, histories := false, 0
				src := (&Server{}).landingRepoFacts(&controller.Controller{DB: pool}, store.LandingSource{
					Result: func(ctx context.Context, _, _, _, _, _ string) (bool, []rules.LandingChange, error) {
						started = true
						safego.Go("review708.reconcile", func() {
							defer close(done)
							_, reconcileErr := store.ReconcileDenied(ctx, reconcile, tc.repo)
							done <- reconcileErr
						})
						var pid uint32
						select {
						case pid = <-reconcile.pid:
						case <-ctx.Done():
							return false, nil, ctx.Err()
						}
						if err := waitBlocked708(ctx, observer, <-gate.pid, pid); err != nil {
							return false, nil, err
						}
						if pool.Stat().AcquiredConns() != 2 {
							return false, nil, fmt.Errorf("workers did not fill the two-slot pool")
						}
						path := "console/src/components/A.tsx"
						if tc.repo == "bifrost-trade-infra" {
							path = "agent-config/work/multi-agent/REPORT-a.md"
						}
						return true, []rules.LandingChange{{NewPath: path, NewMode: "100644", Added: 1}}, nil
					},
					History: func(_ context.Context, repo, task, a, _ string) (landing.MainHistory, error) {
						histories++
						if task == dep708 && repo == tc.dep {
							return landing.MainHistory{State: landing.Effective, Commit: a}, nil
						}
						return landing.MainHistory{State: landing.Never}, nil
					},
				})
				ci := landing.CIRun{Run: "review708", R: strings.Repeat("d", 40), Status: "Succeeded", CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
				if env == "stg" {
					err = store.FinishLandingDrill(ctx, gate, src, op708, at, func(context.Context, landing.Operation) (landing.DrillObservation, error) {
						return landing.DrillObservation{Main: strings.Repeat("b", 40), Candidate: ci}, nil
					}, tc.repo)
				} else {
					var auth store.Authorization
					var decision landing.Decision
					auth, decision, err = store.AuthorizeLandingPush(ctx, gate, src, op708, func(context.Context, landing.Operation) (store.LiveObservation, error) {
						return store.LiveObservation{Main: strings.Repeat("b", 40), CI: ci}, nil
					}, tc.repo)
					if err == nil && (!decision.Allowed || auth.R != ci.R) {
						err = fmt.Errorf("gate did not authorize: %+v %+v", auth, decision)
					}
				}
				// Let reconciliation finish after the gate commits; cancellation
				// must not be what breaks the wait on a passing test.
				if started {
					select {
					case recErr, completed := <-done:
						if !completed || recErr != nil {
							t.Errorf("reconciler: completed=%t err=%v", completed, recErr)
						}
					case <-ctx.Done():
						cancel()
						<-done
						t.Errorf("reconciler timed out")
					}
				}
				must708(t, err)
				wantReads := 1
				if tc.dep != "" {
					wantReads++
				}
				if !started || histories != wantReads {
					t.Fatalf("evidence skipped: started=%t histories=%d want=%d", started, histories, wantReads)
				}
				must708(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
					state, err := store.LandingState(ctx, tx, "landing:"+tc.repo)
					if err != nil {
						return err
					}
					want := "rehearsed"
					if env == "prod" {
						want = "main_intent"
					}
					if state.Operations[op708].Phase != want {
						return fmt.Errorf("phase=%s want=%s", state.Operations[op708].Phase, want)
					}
					return nil
				}))
			})
		}
	}
}

func TestReview708UnsignedAndRevokedEvidencePG(t *testing.T) {
	t.Parallel()
	db := agentrttest.Migrated(t)
	ctx := context.Background()
	_, err := store.Seed(ctx, db.Connect(t, db.Owner))
	must708(t, err)
	pool := db.Pool(t, db.Runtime)
	at := time.Now().UTC().Truncate(time.Second)
	reads := 0
	src := (&Server{}).landingRepoFacts(&controller.Controller{DB: pool}, store.LandingSource{
		Result: func(context.Context, string, string, string, string, string) (bool, []rules.LandingChange, error) {
			reads++
			return true, nil, nil
		},
		History: func(context.Context, string, string, string, string) (landing.MainHistory, error) {
			reads++
			return landing.MainHistory{State: landing.Never}, nil
		},
	})
	check := func(allowed bool) {
		t.Helper()
		before := reads
		_, _, resultErr := src.Result(ctx, "bifrost-trade-infra", "task", "attempt", "base", "a")
		_, historyErr := src.History(ctx, "bifrost-trade-infra", "task", "a", "")
		if (resultErr == nil) != allowed || (historyErr == nil) != allowed || !allowed && reads != before || allowed && reads != before+2 {
			t.Fatalf("allowed=%t result=%v history=%v reads=%d before=%d", allowed, resultErr, historyErr, reads, before)
		}
	}
	check(false)
	sign708(t, pool, at)
	check(true)
	_, err = store.TightenRule(ctx, pool, store.Writer{Actor: "owner", Token: "admin"}, "landing:dev", []byte(ruleText708(at, 2, false)))
	must708(t, err)
	check(false)
	var streams int
	must708(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.event_stream WHERE event_stream_id='landing:bifrost-trade-infra'`).Scan(&streams))
	if streams != 0 {
		t.Fatal("evidence checks created an infra stream")
	}
}
