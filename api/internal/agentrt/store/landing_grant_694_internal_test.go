package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// Regressions of code review 694 on the grant chain (W-31 core C-5). The
// row doubles exercise the production functions, not SQL; the database
// tests are in landing_grant*_test.go.

// B-01: a grant rests only on the strict read of its pinned key: a run
// with no sibling results, another sibling, or no verified pipeline is not
// green (on dff96c3c the grant read v1's LandingCI, which took all three).
func TestC5B01GrantCIProblem(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	sha, ui, infra := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)
	op := landing.Operation{R: sha, PreparedAt: now.Add(-2 * time.Minute)}
	key := &GrantCIKey{Repo: "bifrost-platform", Revision: sha, Base: sha, Siblings: map[string]string{"bifrost-ui": ui, "bifrost-trade-infra": infra}}
	good := func() GrantCIEvidence {
		return GrantCIEvidence{Pipeline: "bifrost-ci-platform", Run: "green", UID: "uid", CreatedAt: now.Add(-time.Minute), Status: "Succeeded",
			Candidate: sha, Siblings: map[string]string{"bifrost-ui": ui, "bifrost-trade-infra": infra}}
	}
	if grantCIProblem(op, key, good(), now) {
		t.Fatal("the control is not green")
	}
	for name, edit := range map[string]func(*GrantCIEvidence){
		"missing siblings":   func(e *GrantCIEvidence) { e.Siblings = map[string]string{"bifrost-ui": "", "bifrost-trade-infra": ""} },
		"wrong sibling":      func(e *GrantCIEvidence) { e.Siblings["bifrost-ui"] = infra },
		"remote pipeline":    func(e *GrantCIEvidence) { e.Pipeline, e.Status, e.Reason = "", "Unknown", "ci_pipeline_mismatch" },
		"result mismatch":    func(e *GrantCIEvidence) { e.Status, e.Reason = "Failed", "ci_result_mismatch" },
		"another candidate":  func(e *GrantCIEvidence) { e.Candidate = ui },
		"before preparation": func(e *GrantCIEvidence) { e.CreatedAt = op.PreparedAt },
	} {
		e := good()
		edit(&e)
		if !grantCIProblem(op, key, e, now) {
			t.Errorf("%s accepted", name)
		}
	}
	if !grantCIProblem(op, nil, good(), now) {
		t.Error("CI with no pinned key accepted")
	}
}

// R-04: a landing stream is one ordered read, grants and all (on dff96c3c
// LandingState read the stream twice, the second time for grants only).
func TestC5R04OneReadOfTheStream(t *testing.T) {
	t.Parallel()
	tx := &countingTx{}
	if _, err := LandingState(context.Background(), tx, landingRepoStream); err != nil {
		t.Fatal(err)
	}
	if len(tx.queries) != 1 {
		t.Fatalf("%d reads of the stream: %v", len(tx.queries), tx.queries)
	}
}

type countingTx struct {
	pgx.Tx
	queries []string
}

func (c *countingTx) Query(_ context.Context, q string, _ ...any) (pgx.Rows, error) {
	c.queries = append(c.queries, q)
	return &grantRows{}, nil
}

// grantRows answers scanLandingStream's wide row: seq, at and the five
// record columns, from stored event bodies.
type grantRows struct {
	pgx.Rows
	bodies []map[string]any
	at     time.Time
	i      int
}

func (r *grantRows) Next() bool { r.i++; return r.i <= len(r.bodies) }
func (*grantRows) Close()       {}
func (*grantRows) Err() error   { return nil }
func (r *grantRows) Scan(dest ...any) error {
	body := r.bodies[r.i-1]
	*dest[0].(*int64) = int64(r.i)
	*dest[1].(*time.Time) = r.at
	for i, key := range []string{"landing", "landing_grant", "landing_grant_revoked", "landing_grant_use", "landing_grant_refused"} {
		*dest[2+i].(*[]byte) = nil
		if v, ok := body[key]; ok {
			b, err := json.Marshal(v)
			if err != nil {
				return err
			}
			*dest[2+i].(*[]byte) = b
		}
	}
	return nil
}

type funcRow func(...any) error

func (f funcRow) Scan(dest ...any) error { return f(dest...) }

// revokeDB stores event bodies; its first transaction's COMMIT is lost
// after it committed, and may cancel the request's context.
type revokeDB struct {
	t                *testing.T
	cancel           context.CancelFunc
	at               time.Time
	bodies           []map[string]any
	pending          map[string]any
	begun            int
	readbackDeadline bool
}

func (d *revokeDB) BeginTx(ctx context.Context, _ pgx.TxOptions) (pgx.Tx, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	d.begun++
	return &revokeTx{d: d, readback: d.begun > 1}, nil
}

type revokeTx struct {
	pgx.Tx
	d        *revokeDB
	readback bool
}

func (tx *revokeTx) Rollback(context.Context) error { return nil }
func (tx *revokeTx) Commit(context.Context) error {
	if tx.readback {
		return nil
	}
	tx.d.bodies = append(tx.d.bodies, tx.d.pending)
	if tx.d.cancel != nil {
		tx.d.cancel()
	}
	return errors.New("COMMIT response lost")
}
func (tx *revokeTx) Exec(ctx context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	if tx.readback {
		_, tx.d.readbackDeadline = ctx.Deadline()
	}
	return pgconn.NewCommandTag("SELECT 1"), ctx.Err()
}
func (tx *revokeTx) Query(ctx context.Context, _ string, _ ...any) (pgx.Rows, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return &grantRows{bodies: tx.d.bodies, at: tx.d.at}, nil
}
func (tx *revokeTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return funcRow(func(dest ...any) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		switch {
		case strings.Contains(sql, "SELECT last_seq"):
			*dest[0].(*int64) = 1
			*dest[1].(*string) = strings.Repeat("0", 64)
		case strings.Contains(sql, "INSERT INTO agentrt.event"):
			var body map[string]any
			if err := json.Unmarshal([]byte(args[9].(string)), &body); err != nil {
				return err
			}
			tx.d.pending = body
			*dest[0].(*int64) = 2
		default:
			tx.d.t.Fatalf("unexpected QueryRow: %s", sql)
		}
		return nil
	})
}

// R-01: a revocation committed whose COMMIT answer was lost is read back
// on a fresh context with its own 10-second deadline, even when the
// request's context was cancelled meanwhile (on dff96c3c the read back used
// the request's context: "context canceled", reported uncertain).
func TestC5R01RevokeReadbackUsesItsOwnContext(t *testing.T) {
	t.Parallel()
	for _, cancelRequest := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel=%v", cancelRequest), func(t *testing.T) {
			t.Parallel()
			now := DecisionAt().Add(-time.Minute)
			sha := strings.Repeat("a", 40)
			digest := "sha256:" + strings.Repeat("b", 64)
			task := "t_00000000000000000000000001"
			g := LandingGrant{V: "1", GrantID: "lg_appr_r01", ApprovalID: "appr_r01", ApprovalNumber: 1,
				Action: ActionCoreLanding, ParamsHash: strings.Repeat("c", 64), Home: "prod", Env: "prod", Tenant: "dev", Repo: "bifrost-platform",
				Task: task, Attempt: "a_00000000000000000000000001", Op: "land:" + task + ":1", A: sha, Base: sha, R: sha, M0: sha,
				CommitterDate: now.Format(time.RFC3339Nano), CandidateDigest: digest, MaterialDigest: digest,
				Waives: []string{"core"}, SoftAtRequest: []string{"core"}, Files: 1, Lines: 1,
				Rule: GrantRule{ID: "landing:dev", Version: 1, SHA256: strings.Repeat("d", 64)}, ChecksVersion: 1,
				CI:      GrantCI{Pipeline: "bifrost-ci-platform", Run: "green", UID: "uid", CreatedAt: now.Format(time.RFC3339Nano), Status: "Succeeded", SHA: sha},
				Purpose: LandingGrantPurpose, ApprovedAt: now.Format(time.RFC3339Nano), GrantedAt: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(LandingGrantTTL).Format(time.RFC3339Nano)}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			db := &revokeDB{t: t, at: now, bodies: []map[string]any{{"landing_grant": g.Body()}}}
			if cancelRequest {
				db.cancel = cancel
			}
			result, err := RevokeLandingGrant(ctx, db, g.GrantID)
			if len(db.bodies) != 2 {
				t.Fatal("the fixture did not commit the revocation")
			}
			if err != nil || !result.Revoked {
				t.Errorf("the committed revocation was not read back: %+v %v", result, err)
			}
			if !db.readbackDeadline {
				t.Error("the read back has no deadline of its own")
			}
		})
	}
}
