package store

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// An SQL-call probe, not a PostgreSQL/concurrency test. No production data.
type review704DB struct{ tx *review704Tx }

func (d review704DB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) { return d.tx, nil }

type review704Tx struct {
	pgx.Tx
	inserts   []string
	committed bool
}

func (x *review704Tx) Exec(_ context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(q, "INSERT INTO agentrt.event_stream") {
		x.inserts = append(x.inserts, fmt.Sprint(args[0]))
	}
	return pgconn.CommandTag{}, nil
}
func (x *review704Tx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return &landingEmptyRows{}, nil
}
func (x *review704Tx) QueryRow(_ context.Context, q string, _ ...any) pgx.Row {
	if strings.Contains(q, "SELECT EXISTS") {
		return funcRow(func(dest ...any) error { *dest[0].(*bool) = false; return nil })
	}
	return review704NoRule{}
}
func (x *review704Tx) Commit(context.Context) error   { x.committed = true; return nil }
func (x *review704Tx) Rollback(context.Context) error { return nil }

type review704NoRule struct{}

func (review704NoRule) Scan(...any) error { return pgx.ErrNoRows }

func TestReview704UnsignedModeMustNotCreateStream(t *testing.T) {
	t.Parallel()
	tx := &review704Tx{}
	at, err := EnsureLandingMode(context.Background(), review704DB{tx}, "bifrost-trade-infra")
	if err != nil || !at.IsZero() {
		t.Fatal(at, err)
	}
	if tx.committed && len(tx.inserts) > 0 {
		t.Fatalf("no current rule, but committed stream INSERT: %v", tx.inserts)
	}
}

func TestReview704EmptyRecoveryMustNotCreateStream(t *testing.T) {
	t.Parallel()
	tx := &review704Tx{}
	ended, err := ReconcileDenied(context.Background(), review704DB{tx}, "bifrost-trade-infra")
	if err != nil || len(ended) != 0 || len(tx.inserts) != 0 {
		t.Fatal("empty recovery created a stream", ended, tx.inserts, err)
	}
}

func TestReview704PlatformDependencyMustRespectInfraRule(t *testing.T) {
	t.Parallel()
	p := "bifrost-platform"
	i := "bifrost-trade-infra"
	reads := []string{}
	src := LandingSource{
		Result: func(context.Context, string, string, string, string, string) (bool, []rules.LandingChange, error) {
			return true, nil, nil
		},
		History: func(_ context.Context, repo, task, a, snapshot string) (landing.MainHistory, error) {
			reads = append(reads, repo)
			if repo == i {
				return landing.MainHistory{State: landing.Effective, Commit: a}, nil
			}
			return landing.MainHistory{State: landing.Never}, nil
		},
	}
	f := landing.Facts{Repo: p, Rule: &rules.Rule{Landing: &rules.Landing{Repos: []rules.LandingRepo{{Repo: p}}}}}
	err := landingFactsRead(context.Background(), src, &f, []landingDep{{task: "dependency", repo: i, commit: strings.Repeat("a", 40)}}, "", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, repo := range reads {
		if repo == i {
			t.Fatalf("platform-only rule still invoked infra history: %v", reads)
		}
	}
	if f.DependenciesEffective {
		t.Fatal("unsigned dependency counted as effective")
	}
	// Same entry, adding the signed infra stanza permits its evidence.
	f.Rule.Landing.Repos = append(f.Rule.Landing.Repos, rules.LandingRepo{Repo: i})
	reads = nil
	if err := landingFactsRead(context.Background(), src, &f, []landingDep{{task: "dependency", repo: i, commit: strings.Repeat("a", 40)}}, "", true); err != nil || !f.DependenciesEffective || len(reads) != 2 || reads[1] != i {
		t.Fatal("signed dependency control", reads, f.DependenciesEffective, err)
	}
}
