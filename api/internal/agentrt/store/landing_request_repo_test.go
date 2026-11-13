package store_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Both repository fixtures freeze the event clock.
func TestLandingRequestsUseOperationRepository(t *testing.T) {
	t.Parallel()
	ctx := ownClock(t)
	for _, repo := range []string{"bifrost-platform", infraRepo} {
		t.Run(repo, func(t *testing.T) {
			var w *world
			var root, op string
			var at time.Time
			if repo == infraRepo {
				c := floorWait(t)
				w, root, op, at = c.w, c.root, c.op, c.at
			} else {
				w, root, op, _, at = c4Wait(t)
			}
			stream := "landing:" + repo
			r := c4Request(op, "pending", at)
			body := map[string]any{"v": "1", "op": op, "approval_id": r.ApprovalID, "approval_number": r.ApprovalNumber,
				"status": r.Status, "params_hash": r.ParamsHash, "seen_at": at.UTC().Format(time.RFC3339Nano)}
			if repo == infraRepo {
				// Simulate a misplaced historical observation without passing Append's guard.
				admin := w.db.Connect(t, w.db.Admin)
				tx, err := admin.Begin(ctx)
				ok(t, err)
				defer tx.Rollback(ctx)
				_, err = tx.Exec(ctx, "SET LOCAL session_replication_role = replica")
				ok(t, err)
				tag, err := tx.Exec(ctx, `UPDATE agentrt.event SET body_json = body_json || jsonb_build_object('landing_grant_request', $1::jsonb)
      WHERE (event_stream_id,seq) = (SELECT event_stream_id,seq FROM agentrt.event WHERE event_stream_id <> $2 LIMIT 1)`, body, stream)
				ok(t, err)
				if tag.RowsAffected() != 1 {
					t.Fatal("negative control observation was not seeded")
				}
				ok(t, tx.Commit(ctx))
				wait, err := store.ReadLandingWait(ctx, w.rt, op)
				ok(t, err)
				wait.ObserveMain(wait.M0)
				if wait.ApprovalStatus != "" || wait.OwnerTodo {
					t.Fatalf("read another stream's request: %+v", wait)
				}
				err = store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
					_, err := store.Append(ctx, tx, store.Event{Stream: landingRepoStream, Type: "note", Actor: "controller", TaskID: root,
						Body: map[string]any{"text": "wrong stream", "landing_grant_request": body}})
					return err
				})
				if err == nil {
					t.Fatal("infra observation accepted on platform")
				}
			}
			before := w.count(t, `SELECT count(*) FROM agentrt.event WHERE event_stream_id=$1 AND body_json ? 'landing_grant_request'`, landingRepoStream)
			wrote, err := store.RecordLandingGrantRequest(ctx, w.rt, r)
			ok(t, err)
			if !wrote {
				t.Fatal("first observation not recorded")
			}
			if wrote, err = store.RecordLandingGrantRequest(ctx, w.rt, r); err != nil || wrote {
				t.Fatalf("repeat: %v %v", wrote, err)
			}
			if n := w.count(t, `SELECT count(*) FROM agentrt.event WHERE event_stream_id=$1 AND body_json->'landing_grant_request'->>'op'=$2`, stream, op); n != 1 {
				t.Fatalf("observations on own stream: %d", n)
			}
			if repo == infraRepo && before != w.count(t, `SELECT count(*) FROM agentrt.event WHERE event_stream_id=$1 AND body_json ? 'landing_grant_request'`, landingRepoStream) {
				t.Fatal("platform stream changed")
			}
			wait, err := store.ReadLandingWait(ctx, w.rt, op)
			ok(t, err)
			if wait.ApprovalStatus != "pending" || wait.ApprovalID != r.ApprovalID {
				t.Fatalf("request missing: %+v", wait)
			}
			wait.ObserveMain(wait.M0)
			if !wait.OwnerTodo {
				t.Fatalf("pending request is not Owner work: %+v", wait)
			}
			if repo == infraRepo && len(store.SettledLandingGrantRequests(landingState(t, w, landingRepoStream), []store.LandingGrantRequest{r})) != 0 {
				t.Fatal("platform settled unknown infra operation")
			}
		})
	}
}
