package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

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
