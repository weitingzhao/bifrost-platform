package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// SQL-call and resource-order simulation, NOT a PostgreSQL test. Two pool
// slots model agentrtMaxConns("stg"); a channel models the stream row lock.
// Both participants call real store entry points. No external service is used.
type pool708 struct {
	repo     string
	dep      string
	live     bool
	at       time.Time
	rule     rules.Rule
	text     string
	slots    chan struct{}
	stream   chan struct{}
	waiting  chan struct{}
	waitOnce sync.Once
	blocked  atomic.Int32
}

type tx708 struct {
	pgx.Tx
	db     *pool708
	locked bool
	once   sync.Once
}

func (d *pool708) BeginTx(ctx context.Context, _ pgx.TxOptions) (pgx.Tx, error) {
	select {
	case d.slots <- struct{}{}:
		return &tx708{db: d}, nil
	default:
		d.blocked.Add(1)
	}
	select {
	case d.slots <- struct{}{}:
		return &tx708{db: d}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (x *tx708) finish() {
	x.once.Do(func() {
		if x.locked {
			<-x.db.stream
		}
		<-x.db.slots
	})
}
func (x *tx708) Commit(context.Context) error   { x.finish(); return nil }
func (x *tx708) Rollback(context.Context) error { x.finish(); return nil }
func (x *tx708) Exec(ctx context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(q, "FROM agentrt.event_stream") && strings.Contains(q, "FOR UPDATE") && args[0] == "landing:"+x.db.repo {
		select {
		case x.db.stream <- struct{}{}:
			x.locked = true
		default:
			x.db.waitOnce.Do(func() { close(x.db.waiting) })
			select {
			case x.db.stream <- struct{}{}:
				x.locked = true
			case <-ctx.Done():
				return pgconn.CommandTag{}, ctx.Err()
			}
		}
	}
	return pgconn.CommandTag{}, nil
}

type row708 []any

func (r row708) Scan(dst ...any) error {
	if len(r) != len(dst) {
		return fmt.Errorf("row length %d != %d", len(r), len(dst))
	}
	for i, v := range r {
		reflect.ValueOf(dst[i]).Elem().Set(reflect.ValueOf(v))
	}
	return nil
}

type rows708 struct {
	pgx.Rows
	rows []row708
	at   int
}

func (r *rows708) Next() bool            { r.at++; return r.at <= len(r.rows) }
func (r *rows708) Scan(dst ...any) error { return r.rows[r.at-1].Scan(dst...) }
func (r *rows708) Close()                {}
func (r *rows708) Err() error            { return nil }

const task708 = "t_00000000000000000000000001"
const attempt708 = "a_00000000000000000000000001"
const op708 = "land:" + task708 + ":1"

func (x *tx708) QueryRow(_ context.Context, q string, _ ...any) pgx.Row {
	d := x.db
	switch {
	case strings.Contains(q, "SELECT EXISTS"):
		return row708{true}
	case strings.HasPrefix(q, "SELECT rule_version,"):
		b, _ := d.rule.BodyJSON()
		by := "owner"
		return row708{d.rule.Version, d.rule.Direction, d.rule.Effective, d.rule.Expires, d.text, b, &by}
	case strings.Contains(q, "landing_stop"):
		return row708{0, 0}
	case strings.HasPrefix(q, "SELECT m.tenant,"):
		return row708{"dev", "", "M-3"}
	case strings.HasPrefix(q, "SELECT COALESCE(plan_node_id,'')"):
		return row708{"", "M-3"}
	case strings.HasPrefix(q, "SELECT tenant FROM"):
		return row708{"dev"}
	case strings.Contains(q, "FROM agentrt.task t JOIN agentrt.mission"):
		return row708{"M-3", "", "fixture", "implement", "go", d.repo, strings.Repeat("b", 40), "accepted", "dev", "active", 1, d.at.Add(-30 * time.Minute), false, "", 0, "", (*int)(nil), (*string)(nil), (*time.Time)(nil), (*time.Time)(nil), (*string)(nil), []byte(nil), (*string)(nil)}
	case strings.Contains(q, "COALESCE(a.attempt_id"):
		accepted := d.at.Add(-5 * time.Minute)
		return row708{attempt708, strings.Repeat("a", 40), &accepted}
	case strings.Contains(q, "string_agg"):
		return row708{"reviewer"}
	}
	panic("unexpected query: " + q)
}
func (x *tx708) Query(_ context.Context, q string, args ...any) (pgx.Rows, error) {
	d := x.db
	if strings.Contains(q, "task_dependency") {
		if d.dep != "" {
			return &rows708{rows: []row708{{"dependency", d.dep, strings.Repeat("c", 40)}}}, nil
		}
		return &rows708{}, nil
	}
	if !strings.Contains(q, "FROM agentrt.event") {
		panic("unexpected query: " + q)
	}
	out := &rows708{}
	if args[0] != "landing:"+d.repo {
		return out, nil
	}
	r := strings.Repeat("d", 40)
	env := "stg"
	if d.live {
		env = "prod"
	}
	ref := "refs/heads/land/" + env + "/" + task708 + "/1"
	records := []map[string]any{
		{"v": landing.Version, "kind": "mode", "op": "mode:live", "repo": d.repo},
		{"v": landing.Version, "kind": "enabled", "op": "enabled:dev", "repo": d.repo, "rule_version": 1},
		{"v": landing.Version, "kind": "claimed", "op": op708, "task": task708, "attempt": attempt708, "A": strings.Repeat("a", 40), "rule_version": 1},
		{"v": landing.Version, "kind": "prepared", "op": op708, "R": r, "M0": strings.Repeat("b", 40), "committer_date": d.at.Add(-time.Minute).Format(time.RFC3339Nano), "dry_run": !d.live},
		{"v": landing.Version, "kind": "branch_intent", "op": op708, "R": r, "ref": ref},
		{"v": landing.Version, "kind": "branch_pushed", "op": op708, "R": r, "ref": ref},
	}
	for i, rec := range records {
		b, _ := json.Marshal(rec)
		at := d.at.Add(-time.Hour + time.Duration(i)*time.Minute)
		if i >= 2 {
			at = d.at.Add(time.Duration(i-6) * time.Minute)
		}
		if strings.Contains(q, "SELECT seq,") {
			out.rows = append(out.rows, row708{int64(i + 1), at, b, []byte(nil), []byte(nil), []byte(nil), []byte(nil), []byte(nil)})
		} else {
			out.rows = append(out.rows, row708{at, b})
		}
	}
	return out, nil
}

func TestReview708GuardedFactsDoesNotWaitForAnotherConnection(t *testing.T) {
	for _, env := range []string{"stg", "prod"} {
		for _, tc := range []struct {
			name, repo, dep string
			guarded         bool
		}{
			{"platform-control", "bifrost-platform", "", true},
			{"infra-before-wrapper-control", "bifrost-trade-infra", "", false},
			{"infra-guarded", "bifrost-trade-infra", "", true},
			{"cross-repo-before-wrapper-control", "bifrost-platform", "bifrost-trade-infra", false},
			{"cross-repo-guarded", "bifrost-platform", "bifrost-trade-infra", true},
		} {
			t.Run(env+"/"+tc.name, func(t *testing.T) {
				at := time.Now().UTC()
				text := fmt.Sprintf("rule_id: landing:dev\nrule_version: 1\ndirection: initial\neffective_at: %s\nexpires_at: %s\ntenant: dev\nrepos:\n - repo: bifrost-platform\n   allow: [\"console/src/components/**/*.tsx\"]\n   deny: []\n   stg_release: none\n - repo: bifrost-trade-infra\n   allow: [\"agent-config/work/multi-agent/REPORT-*.md\"]\n   deny: []\n   stg_release: none\nlimits: {max_files: 10, max_changed_lines: 3000, max_landings_per_day: 5, max_in_flight_per_repo: 1}\n", at.Add(-2*time.Hour).Format(time.RFC3339), at.Add(time.Hour).Format(time.RFC3339))
				rule, err := rules.Parse([]byte(text))
				if err != nil {
					t.Fatal(err)
				}
				db := &pool708{repo: tc.repo, dep: tc.dep, live: env == "prod", at: at, rule: rule, text: text, slots: make(chan struct{}, agentrtMaxConns("stg")), stream: make(chan struct{}, 1), waiting: make(chan struct{})}
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				otherDone := make(chan error, 1)
				started := false
				src := store.LandingSource{
					Result: func(ctx context.Context, _, _, _, _, _ string) (bool, []rules.LandingChange, error) {
						// The final gate has the stream lock. Start the real reconciler
						// before returning from this simulated Git read.
						started = true
						go func() { _, err := store.ReconcileDenied(ctx, db, tc.repo); otherDone <- err }()
						select {
						case <-db.waiting:
						case <-ctx.Done():
							return false, nil, ctx.Err()
						}
						path := "console/src/components/A.tsx"
						if tc.repo == "bifrost-trade-infra" {
							path = "agent-config/work/multi-agent/REPORT-a.md"
						}
						return true, []rules.LandingChange{{NewPath: path, NewMode: "100644", Added: 1}}, nil
					},
					History: func(_ context.Context, repo, task, a, snapshot string) (landing.MainHistory, error) {
						if task == "dependency" {
							return landing.MainHistory{State: landing.Effective, Commit: a}, nil
						}
						return landing.MainHistory{State: landing.Never}, nil
					},
				}
				if tc.guarded {
					src = (&Server{}).landingRepoFacts(&controller.Controller{DB: db}, src)
				}
				observed := errors.New("reached final observation")
				if env == "stg" {
					err = store.FinishLandingDrill(ctx, db, src, op708, at, func(context.Context, landing.Operation) (landing.DrillObservation, error) {
						return landing.DrillObservation{}, observed
					}, tc.repo)
				} else {
					_, _, err = store.AuthorizeLandingPush(ctx, db, src, op708, func(context.Context, landing.Operation) (store.LiveObservation, error) {
						return store.LiveObservation{}, observed
					}, tc.repo)
				}
				cancel()
				if !started {
					t.Fatalf("gate failed before the interleaving: %v", err)
				}
				select {
				case <-otherDone:
				case <-time.After(time.Second):
					t.Fatalf("reconciler did not finish; gate error=%v", err)
				}
				if !errors.Is(err, observed) {
					t.Fatalf("gate stalled before final observation: %v; blocked additional pool borrows=%d", err, db.blocked.Load())
				}
				if db.blocked.Load() != 0 {
					t.Fatal("unexpected extra pool borrow", db.blocked.Load())
				}
			})
		}
	}
}

func TestReview708UnboundEvidenceRuleChecks(t *testing.T) {
	db := &startup704DB{}
	reads := 0
	src := (&Server{}).landingRepoFacts(&controller.Controller{DB: db}, store.LandingSource{
		Result: func(context.Context, string, string, string, string, string) (bool, []rules.LandingChange, error) {
			reads++
			return true, nil, nil
		},
		History: func(context.Context, string, string, string, string) (landing.MainHistory, error) {
			reads++
			return landing.MainHistory{State: landing.Never}, nil
		},
	})
	for _, signed := range []bool{false, true, false} {
		db.signed = signed
		before, begins := reads, db.begins
		_, _, resultErr := src.Result(context.Background(), "bifrost-trade-infra", "task", "attempt", "base", "a")
		_, historyErr := src.History(context.Background(), "bifrost-trade-infra", "task", "a", "")
		wantReads := before
		if signed {
			wantReads += 2
		}
		if (resultErr == nil) != signed || (historyErr == nil) != signed || reads != wantReads || db.begins != begins+2 || !db.bounded {
			t.Fatalf("signed=%t result=%v history=%v reads=%d db=%+v", signed, resultErr, historyErr, reads, db)
		}
	}
}

// A transaction is a connection to reuse, not permission to cache the rule.
// A scheduled expiry during Result must still stop the next infra History.
func TestReview708RuleRecheckedWithinTransaction(t *testing.T) {
	for _, expire := range []bool{false, true} {
		t.Run(fmt.Sprintf("expires=%t", expire), func(t *testing.T) {
			at := time.Now().UTC()
			text := ruleText708(at, 1, true)
			rule, err := rules.Parse([]byte(text))
			must708(t, err)
			db := &pool708{repo: "bifrost-trade-infra", at: at, rule: rule, text: text, slots: make(chan struct{}, 1), stream: make(chan struct{}, 1), waiting: make(chan struct{})}
			results, histories := 0, 0
			src := (&Server{}).landingRepoFacts(&controller.Controller{DB: db}, store.LandingSource{
				Result: func(context.Context, string, string, string, string, string) (bool, []rules.LandingChange, error) {
					results++
					if expire {
						db.rule.Expires = at.Add(-time.Minute)
					}
					return true, nil, nil
				},
				History: func(context.Context, string, string, string, string) (landing.MainHistory, error) {
					histories++
					return landing.MainHistory{State: landing.Never}, nil
				},
			})
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			observed := errors.New("reached final observation")
			err = store.FinishLandingDrill(ctx, db, src, op708, at, func(context.Context, landing.Operation) (landing.DrillObservation, error) {
				return landing.DrillObservation{}, observed
			}, db.repo)
			if results != 1 || db.blocked.Load() != 0 {
				t.Fatalf("transaction borrowed another connection: results=%d blocked=%d err=%v", results, db.blocked.Load(), err)
			}
			if expire {
				if histories != 0 || err == nil || err.Error() != "landing repo has no current rule" {
					t.Fatalf("expired rule reached history: calls=%d err=%v", histories, err)
				}
			} else if histories != 1 || !errors.Is(err, observed) {
				t.Fatalf("signed control: histories=%d err=%v", histories, err)
			}
		})
	}
}
