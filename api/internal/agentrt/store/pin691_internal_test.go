package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// Scripted SQL rows: this exercises production Go logic without PostgreSQL.
// It does not simulate database locks or claim transaction semantics.
type reviewRow struct {
	values []any
	err    error
}

func (r reviewRow) Scan(dst ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dst) != len(r.values) {
		return fmt.Errorf("fixture: %d columns for %d destinations", len(r.values), len(dst))
	}
	for i := range dst {
		reflect.ValueOf(dst[i]).Elem().Set(reflect.ValueOf(r.values[i]))
	}
	return nil
}
func reviewValues(v ...any) pgx.Row { return reviewRow{values: v} }

type reviewTx struct {
	pgx.Tx
	query func(string, []any) pgx.Row
	exec  func(string, []any) error
}

func (q *reviewTx) QueryRow(_ context.Context, s string, a ...any) pgx.Row { return q.query(s, a) }
func (q *reviewTx) Exec(_ context.Context, s string, a ...any) (pgconn.CommandTag, error) {
	if q.exec != nil {
		if err := q.exec(s, a); err != nil {
			return pgconn.CommandTag{}, err
		}
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}
func (*reviewTx) Commit(context.Context) error   { return nil }
func (*reviewTx) Rollback(context.Context) error { return nil }

type reviewDB struct{ tx *reviewTx }

func (db reviewDB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) { return db.tx, nil }

const reviewEnvelope = `rule_id: standing-envelope:dev
rule_version: 1
direction: initial
effective_at: 2026-10-01T00:00:00Z
expires_at: 2100-01-01T00:00:00Z
tenant: dev
max_action: branch
budget: {max_attempts: 40, max_wall_hours: 48}
executors: [exec:claude, exec:codex]
hosts: [h1]
external_hosts:
  - host: coordinator-mbp
    pin: external:coordinator
    proposer: seat:coordinator
    executor: exec:claude
    provider: anthropic
    repos: [bifrost-platform]
    roles: [implement]
`

func Test691SalvageCannotReplaceReviewFacts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 8, 0, 0, 0, time.UTC)
	base, oldResult, newResult := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)
	task, attempt, host := "t_00000000000000000000000001", "a_fixture", "coordinator-mbp"
	rule, err := rules.Parse([]byte(reviewEnvelope))
	if err != nil {
		t.Fatal(err)
	}
	body, err := rule.BodyJSON()
	if err != nil {
		t.Fatal(err)
	}
	signer := "fixture"
	latest := map[string]any{"changed_lines": float64(1200), "hard": true, "pushed_commit": oldResult}
	storedResult := oldResult
	storedReport := "sha256:" + strings.Repeat("d", 64)
	storedRef := AttemptRef(task, 1)
	persistedFacts := map[string]any{"changed_lines": float64(1200), "hard": true, "pushed_commit": oldResult}
	q := &reviewTx{}
	q.exec = func(sql string, args []any) error {
		if strings.Contains(sql, "UPDATE agentrt.attempt SET persisted_at") {
			storedResult = args[3].(string)
		}
		return nil
	}
	q.query = func(sql string, args []any) pgx.Row {
		switch {
		case strings.Contains(sql, "FROM agentrt.idempotency_key"):
			return reviewRow{err: pgx.ErrNoRows}
		case strings.Contains(sql, "SELECT COALESCE(t.repo, '') FROM agentrt.attempt"):
			return reviewValues("bifrost-platform")
		case strings.Contains(sql, "SELECT COALESCE(t.executor_pin, '')") && strings.Contains(sql, "FROM agentrt.attempt"):
			return reviewValues(rules.PinCoordinator, "bifrost-platform")
		case strings.Contains(sql, "SELECT attempt_id, task_id, attempt_no"):
			return reviewValues(attempt, task, 1, &host, "lost", int64(1), now.Add(-time.Hour), true, true, true)
		case strings.Contains(sql, "SELECT clock_timestamp()"):
			return reviewValues(now)
		case strings.Contains(sql, "SELECT COALESCE(pushed_ref"):
			return reviewValues(storedRef, storedResult, storedReport)
		case strings.Contains(sql, "COALESCE(body_json->'artifacts'"):
			return reviewValues([]byte(fmt.Sprintf(`[%q]`, storedReport)))
		case strings.Contains(sql, "SELECT effect_class, role"):
			return reviewValues("branch", "implement", base)
		case strings.Contains(sql, "FROM unnest($1::text[])"):
			return reviewValues([]string{})
		case strings.Contains(sql, "SELECT a.artifact_kind, b.body"):
			return reviewValues("report", []byte("fixture report"))
		case strings.Contains(sql, "SELECT COALESCE(t.executor_pin, '')"):
			return reviewValues(rules.PinCoordinator, "bifrost-platform", base, true, "dev")
		case strings.Contains(sql, "FROM agentrt.rule"):
			return reviewValues(rule.Version, rule.Direction, rule.Effective, rule.Expires, reviewEnvelope, body, &signer)
		case strings.Contains(sql, "SELECT mission_id FROM agentrt.task"):
			return reviewValues("M-1")
		case strings.Contains(sql, "SELECT last_seq, last_sha256"):
			return reviewValues(int64(1), strings.Repeat("0", 64))
		case strings.Contains(sql, "INSERT INTO agentrt.event"):
			if err := json.Unmarshal([]byte(args[9].(string)), &latest); err != nil {
				t.Fatal(err)
			}
			return reviewValues(int64(2))
		case strings.Contains(sql, "SELECT COALESCE(executor_pin, '')"):
			return reviewValues(rules.PinCoordinator)
		case strings.Contains(sql, "body_json->>'changed_lines'"):
			facts := latest
			if len(args) == 2 && args[1] == oldResult {
				facts = persistedFacts
			}
			return reviewValues(int(facts["changed_lines"].(float64)), facts["hard"].(bool))
		default:
			t.Fatalf("unexpected query: %s", sql)
			return reviewRow{err: errors.New("unexpected SQL")}
		}
	}
	res := Result{Report: "sha256:" + strings.Repeat("d", 64), Ref: AttemptRef(task, 1), Commit: newResult}
	facts := CommitFacts{Parents: []string{base}, Message: "fixture\n\nAgentrt-Task: " + task + "\nChange-Id: I" + strings.Repeat("e", 40) + "\n", Paths: []string{"docs/note.txt"}, Lines: 1}
	salvage := func(r Result) error {
		_, err := SalvageChecked(ctx, reviewDB{q}, attempt, host, r,
			func(context.Context, string, string) (string, error) { return r.Commit, nil },
			func(context.Context, string, string) (CommitFacts, error) { return facts, nil })
		return err
	}
	if err := salvage(res); !errors.Is(err, ErrState) {
		t.Fatalf("salvage B after persisted A: %v", err)
	}
	if latest["pushed_commit"] != oldResult || storedResult != oldResult {
		t.Fatal("rejected replacement changed result facts")
	}
	res.Commit = oldResult
	facts.Lines, facts.Paths = 1200, []string{"api/internal/agentrt/store/pin.go"}
	if err := salvage(res); err != nil {
		t.Fatal(err)
	}
	// Legacy/corrupt later events about B cannot supply A's review facts.
	latest = map[string]any{"changed_lines": float64(1), "hard": false, "pushed_commit": newResult}
	spec := checkSpec{check: "review", attempt: attempt, commit: oldResult}
	limit, err := pinnedReview(ctx, q, task, &spec)
	if err != nil {
		t.Fatal(err)
	}
	if limit != 3*time.Hour || spec.text != TrustReviewText {
		t.Fatalf("review A used B facts: %v %q", limit, spec.text)
	}
}

// No-database companion to the double-connection test: expire the entry at
// the capability-lock boundary. The control reaches the common concurrency
// refusal; the revoked case must stop at authorization before that judgment.
func Test691ClaimRereadsAuthorityAfterLock(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"control", "revoked", "repo narrowed"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			locked := false
			rule, err := rules.Parse([]byte(reviewEnvelope))
			if err != nil {
				t.Fatal(err)
			}
			body, err := rule.BodyJSON()
			if err != nil {
				t.Fatal(err)
			}
			signer := "fixture"
			q := &reviewTx{}
			q.exec = func(sql string, args []any) error {
				if strings.Contains(sql, "pg_advisory_xact_lock_shared") {
					locked = true
				}
				return nil
			}
			q.query = func(sql string, args []any) pgx.Row {
				switch {
				case strings.Contains(sql, "FROM agentrt.idempotency_key"):
					return reviewRow{err: pgx.ErrNoRows}
				case strings.Contains(sql, "SELECT host_kind"):
					return reviewValues(HostExternal)
				case strings.Contains(sql, "UPDATE agentrt.host"):
					return reviewValues("active", HostExternal, 1)
				case strings.Contains(sql, "SELECT COALESCE(t.executor_pin"):
					if strings.Contains(sql, "COALESCE(t.repo") {
						repo := "bifrost-platform"
						if mode == "repo narrowed" {
							repo = "bifrost-trade-infra"
						}
						return reviewValues(rules.PinCoordinator, "dev", repo, "implement", "ready")
					}
					if strings.Contains(sql, "t.state") {
						return reviewValues(rules.PinCoordinator, "dev", "ready")
					}
					return reviewValues(rules.PinCoordinator, "dev")
				case strings.Contains(sql, "SELECT clock_timestamp"):
					return reviewValues(time.Date(2026, 10, 29, 8, 0, 0, 0, time.UTC))
				case strings.Contains(sql, "FROM agentrt.rule"):
					if args[0] == rules.KindConcurrency || locked && mode == "revoked" {
						return reviewRow{err: pgx.ErrNoRows}
					}
					return reviewValues(rule.Version, rule.Direction, rule.Effective, rule.Expires, reviewEnvelope, body, &signer)
				default:
					t.Fatalf("unexpected query: %s", sql)
					return reviewRow{err: errors.New("unexpected query")}
				}
			}
			_, err = ClaimPinned(ctx, reviewDB{q}, PinnedClaimRequest{HostID: "coordinator-mbp", TaskID: "t_00000000000000000000000001", Key: "key", FreeSlots: 1})
			want := ErrNoLease
			if mode != "control" {
				want = ErrNotExternal
			}
			if mode == "repo narrowed" && (err == nil || !strings.Contains(err.Error(), "no longer permits")) {
				t.Fatalf("scope check masked: %v", err)
			}
			if !locked || !errors.Is(err, want) {
				t.Fatalf("locked=%v error=%v want %v", locked, err, want)
			}
		})
	}
}

func Test691PendingExternalKnownNames(t *testing.T) {
	t.Parallel()
	for _, ordinary := range []bool{false, true} {
		t.Run(fmt.Sprint(ordinary), func(t *testing.T) {
			t.Parallel()
			rule, err := rules.Parse([]byte(strings.Replace(reviewEnvelope, "hosts: [h1]", "hosts: [h1, coordinator-mbp]", 1)))
			if err != nil {
				t.Fatal(err)
			}
			if ordinary {
				rule.Envelope.Hosts = append(rule.Envelope.Hosts, "unknown")
			}
			q := &reviewTx{query: func(sql string, args []any) pgx.Row {
				if strings.Contains(sql, "SELECT ARRAY") {
					var unknown []string
					for _, h := range args[1].([]string) {
						if h != "h1" {
							unknown = append(unknown, h)
						}
					}
					return reviewValues(unknown)
				}
				if strings.Contains(sql, "EXISTS (SELECT 1 FROM agentrt.host") {
					return reviewValues(false, true, true)
				}
				t.Fatalf("unexpected query: %s", sql)
				return reviewRow{}
			}}
			err = knownNames(context.Background(), q, rule)
			if ordinary && !errors.Is(err, ErrInvalid) || !ordinary && err != nil {
				t.Fatalf("ordinary=%v: %v", ordinary, err)
			}
		})
	}
}

func Test691ReviewFactsMatchCommit(t *testing.T) {
	t.Parallel()
	q := &reviewTx{query: func(sql string, args []any) pgx.Row {
		if strings.Contains(sql, "SELECT COALESCE(executor_pin") {
			return reviewValues(rules.PinCoordinator)
		}
		if strings.Contains(sql, "changed_lines") {
			if len(args) == 2 && args[1] == "A" {
				return reviewValues(1200, true)
			}
			return reviewValues(1, false)
		}
		t.Fatalf("unexpected query %s", sql)
		return reviewRow{}
	}}
	spec := checkSpec{check: "review", attempt: "attempt", commit: "A"}
	limit, err := pinnedReview(context.Background(), q, "task", &spec)
	if err != nil || limit != 3*time.Hour || spec.text != TrustReviewText {
		t.Fatalf("review A: %v %v %q", limit, err, spec.text)
	}
	spec = checkSpec{check: "review", attempt: "attempt", commit: "B"}
	limit, err = pinnedReview(context.Background(), q, "task", &spec)
	if err != nil || limit != 90*time.Minute || spec.text != "" {
		t.Fatalf("review B control: %v %v %q", limit, err, spec.text)
	}
}
