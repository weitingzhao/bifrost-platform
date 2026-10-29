package store

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

type claimReplay695DB struct {
	tx      *claimReplay695Tx
	commits int
}

func (db *claimReplay695DB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	return db.tx, nil
}

type claimReplay695Tx struct {
	*reviewTx
	db *claimReplay695DB
}

func (tx *claimReplay695Tx) Commit(context.Context) error {
	tx.db.commits++
	if tx.db.commits == 1 {
		return errors.New("fixture: lost replay COMMIT response")
	}
	return nil
}

// Offline companion: fault the replay transaction through public ClaimPinned,
// then require its production readback to return the stored answer. This
// proves the Go error path, not a PostgreSQL write or transaction durability;
// Test695ClaimPinnedCommitOutcome covers the first real writing transaction.
func Test695ClaimPinnedReadbackControlFlow(t *testing.T) {
	t.Parallel()
	for _, found := range []bool{true, false} {
		t.Run(map[bool]string{true: "stored", false: "missing"}[found], func(t *testing.T) {
			t.Parallel()
			r := PinnedClaimRequest{HostID: "coordinator-mbp", TaskID: "t_" + strings.Repeat("0", 25) + "1", Key: "same"}
			sum, err := canon.Sum(map[string]any{"task_id": r.TaskID})
			if err != nil {
				t.Fatal(err)
			}
			stored := `{"attempt_id":"a_fixture","attempt_no":1,"lease_epoch":7,"task_id":"` + r.TaskID + `","pushed_ref":"` + AttemptRef(r.TaskID, 1) + `"}`
			db := &claimReplay695DB{}
			reads := 0
			tx := &reviewTx{query: func(sql string, args []any) pgx.Row {
				if !strings.Contains(sql, "FROM agentrt.idempotency_key") || len(args) != 1 || args[0] != "host:"+r.HostID+"|claim_pinned|same" {
					t.Fatalf("unexpected query: %s %v", sql, args)
				}
				reads++
				if reads > 1 && !found {
					return reviewRow{err: pgx.ErrNoRows}
				}
				return reviewValues(sum, stored)
			}}
			db.tx = &claimReplay695Tx{reviewTx: tx, db: db}
			out, err := ClaimPinned(context.Background(), db, r)
			if reads != 2 {
				t.Fatalf("production readback omitted: %d key reads, err=%v", reads, err)
			}
			if !found {
				if err == nil || out != nil {
					t.Fatalf("unconfirmed outcome returned a claim: %v %v", out, err)
				}
				return
			}
			want, _ := canon.Parse([]byte(stored))
			if err != nil || !reflect.DeepEqual(out, want) || db.commits != 2 {
				t.Fatalf("readback: %v, %v (commits=%d)", out, err, db.commits)
			}
		})
	}
}

// Exercise the same route calculation the claim rechecks, with different
// executors and equal providers so executor equality cannot mask the defect.
func Test695PinnedReviewProviderIndependence(t *testing.T) {
	t.Parallel()
	cp := capabilities{candidates: map[string][]jobCandidate{"reviewer": {
		{id: "reviewer/exec:codex/openai/*", executor: "exec:codex", provider: "openai", pattern: "*"},
		{id: "reviewer/exec:claude/anthropic/*", executor: "exec:claude", provider: "anthropic", pattern: "*"},
	}}}
	c := candidate{role: "review", subject: "subject", subjectPin: rules.PinCoordinator}
	for _, provider := range []string{"anthropic", "openai"} {
		t.Run(provider, func(t *testing.T) {
			t.Parallel()
			in := cp.input(c, []priorAttempt{{id: "subject-attempt", executor: "exec:claude", provider: &provider}})
			route, _ := in.decide()
			if provider == "openai" {
				if len(route) != 0 {
					t.Fatalf("forged openai subject routes to %v", route)
				}
			} else if !slices.Equal(route, []string{"reviewer/exec:codex/openai/*"}) {
				t.Fatalf("independent control routes to %v", route)
			}
		})
	}
}

// Capture what pinnedSpec really writes, preserving every command and argv
// position. A same-length list of ["true"] must fail this check.
func Test695PinnedSpecFullVerify(t *testing.T) {
	t.Parallel()
	for _, repo := range []string{"bifrost-platform", "bifrost-trade-infra"} {
		t.Run(repo, func(t *testing.T) {
			t.Parallel()
			var body []byte
			tx := &reviewTx{exec: func(sql string, args []any) error {
				if strings.Contains(sql, "INSERT INTO agentrt.artifact_blob") {
					body = slices.Clone(args[1].([]byte))
				}
				return nil
			}}
			if _, err := pinnedSpec(context.Background(), tx, TaskProposal{Title: "fixture", Repo: repo}); err != nil {
				t.Fatal(err)
			}
			var spec struct{ Verify [][]string }
			if err := json.Unmarshal(body, &spec); err != nil {
				t.Fatal(err)
			}
			want := Repos[repo].Verify
			if len(want) == 0 || !slices.EqualFunc(spec.Verify, want, slices.Equal) {
				t.Fatalf("Verify = %q; want %q", spec.Verify, want)
			}
		})
	}
}
