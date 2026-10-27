package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Real PostgreSQL, two runtime connections and the production collector.
// Cover creation of both streams and a commit that advances only observation
// (the unchanged binding set's pass is still within its refresh interval).
func TestPV2CollectorPostgresOutOfOrder(t *testing.T) {
	t.Parallel()
	for _, prime := range []bool{false, true} {
		name := "new streams"
		if prime {
			name = "observation only"
		}
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			d := pvDocument(t, 1)
			b := pvBinding(d, s1, "main_effective", "git")
			b.Repo, b.Result = "bifrost-platform", commitA
			adoptView(t, w, pvArtifact(t, w, d, b), 0)
			slow := w.db.Connect(t, w.db.Runtime)
			if slow.PgConn().PID() == w.rt.PgConn().PID() {
				t.Fatal("test requires two PostgreSQL backends")
			}
			source := func(state landing.History) store.PlanSources {
				return store.PlanSources{History: func(context.Context, string, string, string, string) (landing.MainHistory, error) {
					return landing.MainHistory{State: state, Commit: commitA}, nil
				}}
			}
			if prime {
				pvPoll(t, w, source(landing.Effective))
			}
			positions := func() [2]int64 {
				var p [2]int64
				ok(t, w.rt.QueryRow(ctx, `SELECT
 COALESCE((SELECT last_seq FROM agentrt.event_stream WHERE event_stream_id=$1),0),
 COALESCE((SELECT last_seq FROM agentrt.event_stream WHERE event_stream_id=$2),0)`, "plan-observation-pass:"+root, "plan-observation:"+root).Scan(&p[0], &p[1]))
				return p
			}
			before := positions()
			started, release := make(chan struct{}), make(chan struct{})
			readctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			type result struct {
				n   int
				err error
			}
			done := make(chan result, 1)
			finished := false
			go func() {
				n, err := store.CollectPlanObservations(readctx, slow, store.PlanSources{History: func(ctx context.Context, _, _, _, _ string) (landing.MainHistory, error) {
					close(started)
					select {
					case <-release:
						return landing.MainHistory{State: landing.Effective, Commit: commitA}, nil
					case <-ctx.Done():
						return landing.MainHistory{}, ctx.Err()
					}
				}})
				done <- result{n, err}
			}()
			defer func() {
				cancel()
				if finished {
					return
				}
				select {
				case <-done:
				case <-time.After(10 * time.Second):
					t.Error("collector did not stop")
				}
			}()
			select {
			case <-started:
			case <-readctx.Done():
				t.Fatal("older collector never reached remote read")
			}
			pvPoll(t, w, source(landing.Reverted))
			committed := positions()
			if prime && (committed[0] != before[0] || committed[1] != before[1]+1) {
				t.Fatalf("expected only observation to advance: %v -> %v", before, committed)
			}
			close(release)
			got := <-done
			finished = true
			if got.err != nil || got.n != 0 {
				t.Fatalf("older round must skip without error: %+v", got)
			}
			if after := positions(); after != committed {
				t.Fatalf("skipped round wrote events: %v -> %v", committed, after)
			}
			var value string
			ok(t, w.rt.QueryRow(ctx, `SELECT body_json->'plan_observation'->>'value' FROM agentrt.event WHERE event_stream_id=$1 ORDER BY seq DESC LIMIT 1`, "plan-observation:"+root).Scan(&value))
			if value != "false" {
				t.Fatalf("older Effective overwrote committed Reverted: %s", value)
			}
			pvEvaluate(t, w, s1, "planned")
		})
	}
}
