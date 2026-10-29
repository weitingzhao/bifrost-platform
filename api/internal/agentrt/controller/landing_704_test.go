package controller

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
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Database row fixtures exercise the actual branch entry and store reader.
// The PG counterpart also runs replay and branch writes against local Git.
type branch704DB struct {
	pgx.Tx
	repo, dep string
	at        time.Time
	rule      rules.Rule
	text      string
}

func (d *branch704DB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) { return d, nil }
func (d *branch704DB) Commit(context.Context) error                           { return nil }
func (d *branch704DB) Rollback(context.Context) error                         { return nil }

type row704 []any

func (r row704) Scan(dst ...any) error {
	if len(r) != len(dst) {
		return fmt.Errorf("row length %d != %d", len(r), len(dst))
	}
	for i, v := range r {
		reflect.ValueOf(dst[i]).Elem().Set(reflect.ValueOf(v))
	}
	return nil
}

type rows704 struct {
	pgx.Rows
	rows []row704
	at   int
}

func (r *rows704) Next() bool            { r.at++; return r.at <= len(r.rows) }
func (r *rows704) Scan(dst ...any) error { return r.rows[r.at-1].Scan(dst...) }
func (r *rows704) Close()                {}
func (r *rows704) Err() error            { return nil }

const task704 = "t_00000000000000000000000001"
const attempt704 = "a_00000000000000000000000001"

func (d *branch704DB) QueryRow(_ context.Context, q string, _ ...any) pgx.Row {
	switch {
	case strings.HasPrefix(q, "SELECT rule_version,"):
		b, _ := d.rule.BodyJSON()
		signed := "owner"
		return row704{d.rule.Version, d.rule.Direction, d.rule.Effective, d.rule.Expires, d.text, b, &signed}
	case strings.Contains(q, "FROM agentrt.task t JOIN agentrt.mission"):
		return row704{"M-3", "", "fixture", "implement", "go", d.repo, strings.Repeat("b", 40), "accepted", "dev", "active", 1, d.at.Add(-30 * time.Minute), false, "", 0, "", (*int)(nil), (*string)(nil), (*time.Time)(nil), (*time.Time)(nil), (*string)(nil), []byte(nil), (*string)(nil)}
	case strings.Contains(q, "COALESCE(a.attempt_id"):
		accepted := d.at.Add(-5 * time.Minute)
		return row704{attempt704, strings.Repeat("a", 40), &accepted}
	case strings.Contains(q, "string_agg"):
		return row704{"reviewer"}
	}
	panic("unexpected query: " + q)
}
func (d *branch704DB) Query(_ context.Context, q string, args ...any) (pgx.Rows, error) {
	if strings.Contains(q, "task_dependency") {
		return &rows704{rows: []row704{{"dependency", d.dep, strings.Repeat("c", 40)}}}, nil
	}
	if strings.Contains(q, "FROM agentrt.event") {
		out := &rows704{}
		if args[0] != "landing:"+d.repo {
			return out, nil
		}
		records := []map[string]any{
			{"v": landing.Version, "kind": "mode", "op": "mode:live", "repo": d.repo},
			{"v": landing.Version, "kind": "enabled", "op": "enabled:dev", "repo": d.repo, "rule_version": 1},
			{"v": landing.Version, "kind": "claimed", "op": "land:" + task704 + ":1", "task": task704, "attempt": attempt704, "A": strings.Repeat("a", 40), "rule_version": 1},
		}
		for i, record := range records {
			b, _ := json.Marshal(record)
			at := d.at.Add(-time.Hour)
			if i == 2 {
				at = d.at.Add(-time.Minute)
			}
			if strings.Contains(q, "SELECT seq,") {
				out.rows = append(out.rows, row704{int64(i + 1), at, b, []byte(nil), []byte(nil), []byte(nil), []byte(nil), []byte(nil)})
			} else {
				out.rows = append(out.rows, row704{at, b})
			}
		}
		return out, nil
	}
	panic("unexpected query: " + q)
}
func TestReview704BranchUsesClaimEvidence(t *testing.T) {
	for _, repo := range []string{"bifrost-platform", "bifrost-trade-infra"} {
		t.Run(repo, func(t *testing.T) {
			dep := "bifrost-platform"
			if repo == dep {
				dep = "bifrost-trade-infra"
			}
			at := time.Now().UTC().Truncate(time.Second)
			text := fmt.Sprintf("rule_id: landing:dev\nrule_version: 1\ndirection: initial\neffective_at: %s\nexpires_at: %s\ntenant: dev\nrepos:\n    - repo: bifrost-platform\n      allow: [\"console/src/components/**/*.tsx\"]\n      deny: []\n      stg_release: none\n    - repo: bifrost-trade-infra\n      allow: [\"agent-config/work/multi-agent/REPORT-*.md\"]\n      deny: []\n      stg_release: none\nlimits: {max_files: 10, max_changed_lines: 3000, max_landings_per_day: 5, max_in_flight_per_repo: 1}\n", at.Add(-2*time.Hour).Format(time.RFC3339), at.Add(24*time.Hour).Format(time.RFC3339))
			r, err := rules.Parse([]byte(text))
			if err != nil {
				t.Fatal(err)
			}
			db := &branch704DB{repo: repo, dep: dep, at: at, rule: r, text: text}
			g := &repoProbe{repo: repo}
			result, history, dependency := 0, 0, 0
			src := store.LandingSource{Result: func(_ context.Context, got, task, attempt, base, a string) (bool, []rules.LandingChange, error) {
				if got != repo {
					t.Fatal("candidate crossed repositories", got)
				}
				result++
				path := "console/src/components/A.tsx"
				if repo == "bifrost-trade-infra" {
					path = "agent-config/work/multi-agent/REPORT-a.md"
				}
				return true, []rules.LandingChange{{NewPath: path, NewMode: "100644", Added: 1}}, nil
			}, History: func(_ context.Context, got, task, a, snapshot string) (landing.MainHistory, error) {
				if got == dep {
					dependency++
					return landing.MainHistory{State: landing.Effective, Commit: a}, nil
				}
				if got != repo {
					t.Fatal("unknown repository", got)
				}
				history++
				return landing.MainHistory{State: landing.Never}, nil
			}}
			c := Controller{DB: db, Env: "prod", Live: true, LiveRepos: []string{repo, dep}, Drill: &DrillSources{Facts: src}}
			err = c.LandingDrillBranch(context.Background(), g, "land:"+task704+":1", at, repo)
			// Open deliberately stops at the candidate replay. Reaching it after
			// all three reads proves the branch kept the claim's dependency source.
			if err == nil || err.Error() != repo || g.opens != 1 || result != 1 || history != 1 || dependency != 1 {
				t.Fatalf("branch lost claim evidence: err=%v opens=%d result=%d history=%d dependency=%d", err, g.opens, result, history, dependency)
			}
		})
	}
}

type failed704DB struct{ pgx.Tx }

func (d failed704DB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	return nil, errors.New("unavailable")
}
func TestReview704DiscoveryBackoffAndBudget(t *testing.T) {
	now := time.Now()
	calls := 0
	fail := true
	r := &LandingReconcile{Repos: []string{"bifrost-platform"}}
	r.Discover = func(ctx context.Context) ([]string, error) {
		calls++
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 10*time.Second {
			t.Error("unbounded discovery")
		}
		if fail {
			return nil, errors.New("unavailable")
		}
		return []string{"bifrost-platform", "bifrost-trade-infra"}, nil
	}
	c := Controller{DB: failed704DB{}, Reconcile: r, Now: func() time.Time { return now }}
	for _, step := range []time.Duration{0, 0, 59 * time.Second, time.Second, 119 * time.Second, time.Second} {
		now = now.Add(step)
		_ = c.LandingReconcilePass(context.Background())
	}
	if calls != 3 {
		t.Fatalf("discovery retried outside backoff: %d", calls)
	}
	for _, step := range []time.Duration{4, 8, 16, 30, 30} {
		before := calls
		now = now.Add(step*time.Minute - time.Nanosecond)
		_ = c.LandingReconcilePass(context.Background())
		if calls != before {
			t.Fatal("discovery retried before its deadline", step, calls)
		}
		now = now.Add(time.Nanosecond)
		_ = c.LandingReconcilePass(context.Background())
		if calls != before+1 {
			t.Fatal("discovery missed its bounded retry", step, calls)
		}
	}
	now = now.Add(30 * time.Minute)
	fail = false
	_ = c.LandingReconcilePass(context.Background())
	for range 3 {
		_ = c.LandingReconcilePass(context.Background())
	}
	if calls != 9 || r.Discover != nil || len(r.Repos) != 2 {
		t.Fatal("successful discovery repeated or lost recovery", calls, r.Repos)
	}
}

func TestReview704BranchRejectsMisboundAdapter(t *testing.T) {
	g := &repoProbe{repo: "bifrost-platform"}
	c := Controller{Env: "prod", Live: true, LiveRepos: []string{"bifrost-platform", "bifrost-trade-infra"}, DB: failed704DB{}}
	for _, repo := range []string{"bifrost-trade-infra", "unknown"} {
		err := c.LandingDrillBranch(context.Background(), g, "op", time.Now(), repo)
		if err == nil || err.Error() == "unavailable" {
			t.Fatal("misbound branch reached database", repo, err)
		}
	}
	if g.opens != 0 {
		t.Fatal("misbound adapter opened")
	}
}
