package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Row fixtures supply storage only: LandingDrillCI, NextDrill, Append, Parse
// and Fold all run unchanged. The PG counterpart covers real transactions.
type drill751DB struct {
	pgx.Tx
	repo    string
	at      time.Time
	rule    rules.Rule
	text    string
	entries map[string][]landing.Entry
	pending map[string][]landing.Entry
}

type row751 []any

func (r row751) Scan(dst ...any) error {
	if len(r) != len(dst) {
		return fmt.Errorf("row length %d != %d", len(r), len(dst))
	}
	for i, v := range r {
		reflect.ValueOf(dst[i]).Elem().Set(reflect.ValueOf(v))
	}
	return nil
}

type rows751 struct {
	pgx.Rows
	rows []row751
	at   int
}

func (r *rows751) Next() bool            { r.at++; return r.at <= len(r.rows) }
func (r *rows751) Scan(dst ...any) error { return r.rows[r.at-1].Scan(dst...) }
func (r *rows751) Close()                {}
func (r *rows751) Err() error            { return nil }

func (d *drill751DB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	d.pending = map[string][]landing.Entry{}
	return d, nil
}
func (d *drill751DB) Commit(context.Context) error {
	for stream, entries := range d.pending {
		d.entries[stream] = append(d.entries[stream], entries...)
	}
	d.pending = nil
	return nil
}
func (d *drill751DB) Rollback(context.Context) error { d.pending = nil; return nil }
func (d *drill751DB) Exec(_ context.Context, q string, _ ...any) (pgconn.CommandTag, error) {
	if !strings.HasPrefix(q, "INSERT INTO agentrt.event_stream") && !strings.HasPrefix(q, "SELECT set_config(") {
		panic("unexpected exec: " + q)
	}
	return pgconn.CommandTag{}, nil
}
func (d *drill751DB) QueryRow(ctx context.Context, q string, args ...any) pgx.Row {
	if strings.HasPrefix(q, "SELECT last_seq, last_sha256") {
		stream := args[0].(string)
		return row751{int64(len(d.entries[stream]) + len(d.pending[stream])), ""}
	}
	if strings.HasPrefix(q, "INSERT INTO agentrt.event\n") {
		value, err := canon.Parse([]byte(args[9].(string)))
		if err != nil {
			panic(err)
		}
		r, err := landing.Parse(value.(map[string]any)["landing"])
		if err != nil {
			panic(err)
		}
		stream := args[0].(string)
		d.pending[stream] = append(d.pending[stream], landing.Entry{At: args[12].(time.Time), Record: r})
		return row751{args[1].(int64)}
	}
	switch {
	case strings.HasPrefix(q, "SELECT rule_version,"):
		b, err := d.rule.BodyJSON()
		if err != nil {
			panic(err)
		}
		signed := "owner"
		return row751{d.rule.Version, d.rule.Direction, d.rule.Effective, d.rule.Expires, d.text, b, &signed}
	case strings.Contains(q, "FROM agentrt.task t JOIN agentrt.mission"):
		return row751{"M-3", "", "fixture", "implement", "go", d.repo, strings.Repeat("b", 40), "accepted", "dev", "active", 1, d.at.Add(-30 * time.Minute), false, "", 0, "", (*int)(nil), (*string)(nil), (*time.Time)(nil), (*time.Time)(nil), (*string)(nil), []byte(nil), (*string)(nil)}
	case strings.Contains(q, "COALESCE(a.attempt_id"):
		accepted := d.at.Add(-5 * time.Minute)
		return row751{landingAttempt, strings.Repeat("a", 40), &accepted}
	case strings.Contains(q, "string_agg"):
		return row751{"reviewer"}
	}
	panic("unexpected query: " + q)
}
func (d *drill751DB) Query(_ context.Context, q string, args ...any) (pgx.Rows, error) {
	out := &rows751{}
	if strings.Contains(q, "task_dependency") {
		return out, nil
	}
	if !strings.Contains(q, "FROM agentrt.event") {
		panic("unexpected query: " + q)
	}
	stream := args[0].(string)
	entries := append(append([]landing.Entry{}, d.entries[stream]...), d.pending[stream]...)
	for i, e := range entries {
		b, err := json.Marshal(e.Record)
		if err != nil {
			return nil, err
		}
		if strings.Contains(q, "SELECT seq,") {
			out.rows = append(out.rows, row751{int64(i + 1), e.At, b, []byte(nil), []byte(nil), []byte(nil), []byte(nil), []byte(nil)})
		} else {
			out.rows = append(out.rows, row751{e.At, b})
		}
	}
	return out, nil
}

// Serial: FreezeEventClock. Review 751's quota rollover must first unpark;
// recreating the controller each pass cannot lose the preparation or kick twice.
func TestReview751QuotaRecoveryBeforeCIKick(t *testing.T) {
	for _, repo := range landing.LandingRepoNames() {
		t.Run(repo, func(t *testing.T) {
			at := time.Date(2026, 10, 30, 23, 40, 0, 0, time.UTC)
			t.Cleanup(store.FreezeEventClock(at))
			text := fmt.Sprintf("rule_id: landing:dev\nrule_version: 1\ndirection: initial\neffective_at: %s\nexpires_at: %s\ntenant: dev\nrepos:\n    - repo: bifrost-platform\n      allow: [\"console/src/components/**/*.tsx\"]\n      deny: []\n      stg_release: none\n    - repo: bifrost-trade-infra\n      allow: [\"agent-config/work/multi-agent/REPORT-*.md\"]\n      deny: []\n      stg_release: none\nlimits: {max_files: 10, max_changed_lines: 3000, max_landings_per_day: 1, max_in_flight_per_repo: 1}\n", at.Add(-2*time.Hour).Format(time.RFC3339), at.Add(24*time.Hour).Format(time.RFC3339))
			rule, err := rules.Parse([]byte(text))
			if err != nil {
				t.Fatal(err)
			}
			db := &drill751DB{repo: repo, at: at, rule: rule, text: text, entries: map[string][]landing.Entry{}}
			op, stream := "land:"+landingTask+":1", "landing:"+repo
			r, base, sibling := strings.Repeat("c", 40), strings.Repeat("b", 40), strings.Repeat("d", 40)
			ref := "refs/heads/land/prod/" + landingTask + "/1"
			seed := func(stream, kind string, when time.Time, fields map[string]any) {
				t.Helper()
				fields["v"], fields["kind"], fields["op"] = landing.Version, kind, op
				record, err := landing.Parse(fields)
				if err != nil {
					t.Fatal(err)
				}
				db.entries[stream] = append(db.entries[stream], landing.Entry{At: when, Record: record})
				if _, err := landing.Fold(db.entries[stream]); err != nil {
					t.Fatal(err)
				}
			}
			seed(stream, "mode", at.Add(-time.Hour), map[string]any{"repo": repo})
			seed(stream, "enabled", at.Add(-time.Hour), map[string]any{"repo": repo, "rule_version": 1})
			seed(stream, "claimed", at, map[string]any{"task": landingTask, "attempt": landingAttempt, "A": strings.Repeat("a", 40), "rule_version": 1})
			prep := map[string]any{"R": r, "M0": base, "dry_run": false, "committer_date": at.Format(time.RFC3339)}
			if repo == "bifrost-trade-infra" {
				prep["platform_revision"], prep["generation"] = sibling, 1
			}
			seed(stream, "prepared", at, prep)
			seed(stream, "branch_intent", at, map[string]any{"R": r, "ref": ref})
			seed(stream, "branch_pushed", at, map[string]any{"R": r, "ref": ref})
			read := func() landing.Operation {
				t.Helper()
				st, err := store.LandingState(context.Background(), db, stream)
				if err != nil {
					t.Fatal(err)
				}
				return st.Operations[op]
			}
			prepared := read()
			ci := landing.CIRun{Status: "Missing"}
			created := map[string]bool{}
			ensures := 0
			ensure := func(name string) error { ensures++; created[name] = true; return nil }
			src := controller.DrillSources{
				Facts: store.LandingSource{
					Result: func(context.Context, string, string, string, string, string) (bool, []rules.LandingChange, error) {
						path := "console/src/components/A.tsx"
						if repo == "bifrost-trade-infra" {
							path = "agent-config/work/multi-agent/REPORT-a.md"
						}
						return true, []rules.LandingChange{{NewPath: path, NewMode: "100644", Added: 1}}, nil
					},
					History: func(context.Context, string, string, string, string) (landing.MainHistory, error) {
						return landing.MainHistory{State: landing.Never}, nil
					},
				},
				Main:          func(context.Context, string) (string, error) { return base, nil },
				WindowBlocked: func(context.Context, string) (bool, error) { return false, nil },
				CI: func(context.Context, string, string, time.Time) (landing.CIRun, error) {
					if repo != "bifrost-platform" {
						t.Fatal("infra used platform CI")
					}
					return ci, nil
				},
				EnsureCI: func(_ context.Context, got, name, revision string) error {
					if repo != "bifrost-platform" || got != repo || revision != r {
						t.Fatal("wrong legacy CI key")
					}
					return ensure(name)
				},
				InfraCI: func(_ context.Context, o landing.Operation) (landing.CIRun, error) { return ci, nil },
				EnsureInfraCI: func(_ context.Context, o landing.Operation) error {
					if o.R != r || o.M0 != base || o.PlatformRevision != sibling || o.Generation != 1 {
						t.Fatal("preparation key changed", o)
					}
					return ensure(landing.InfraCIName(o.R, o.M0, o.PlatformRevision, o.Generation))
				},
			}
			step := func(when time.Time, want string) {
				t.Helper()
				store.FreezeEventClock(when)
				c := controller.Controller{DB: db, Env: "prod", Live: true, LiveRepos: []string{repo}, Drill: &src}
				a, err := c.LandingDrillCI(context.Background(), src, op, at, repo)
				if err != nil || a.Kind != want {
					t.Fatalf("want %s, got %+v: %v", want, a, err)
				}
			}
			step(at.Add(time.Minute), "wait") // positive control before the shared quota fills
			seed("landing-quota:dev:"+at.Format("2006-01-02"), "reserved", at.Add(2*time.Minute), map[string]any{"root": "t_00000000000000000000000002"})
			step(at.Add(11*time.Minute), "parked")
			parked := read()
			if parked.Why != "limit" || parked.Resume != "branch_pushed" || parked.Kicked || ensures != 0 {
				t.Fatal("did not reach the reported quota park", parked, ensures)
			}
			step(at.Add(12*time.Minute), "parked")
			if len(db.entries[stream]) != 7 {
				t.Fatal("repeated park wrote another event")
			}
			step(at.Add(21*time.Minute), "unparked")
			if got := read(); got.Phase != "branch_pushed" || got.Kicked || ensures != 0 {
				t.Fatal("resume must only unpark", got, ensures)
			}
			step(at.Add(22*time.Minute), "ci_kick")
			ci = landing.CIRun{Run: "observed-run", R: r, Status: "Running", CreatedAt: at.Add(22*time.Minute + time.Second)}
			for range 3 {
				step(at.Add(23*time.Minute), "wait")
			}
			got := read()
			if !got.Kicked || got.Rerun || len(created) != 1 || got.CI.String("run") != ci.Run {
				t.Fatal("recovery duplicated CI or lost evidence", got, created)
			}
			prepared.CI, prepared.Kicked, prepared.KickedAt, prepared.LastAt = got.CI, got.Kicked, got.KickedAt, got.LastAt
			if !reflect.DeepEqual(prepared, got) {
				t.Fatal("recovery changed the preparation", prepared, got)
			}
			var kinds []string
			for _, e := range db.entries[stream][6:] {
				kinds = append(kinds, e.Record.String("kind"))
			}
			if !reflect.DeepEqual(kinds, []string{"parked", "unparked", "ci_kick", "ci_seen"}) {
				t.Fatal("unexpected recovery history", kinds)
			}
		})
	}
}
