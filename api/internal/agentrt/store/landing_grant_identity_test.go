package store

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
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

type grantIdentityEvent struct {
	at   time.Time
	body map[string]any
}
type grantIdentityGateDB struct {
	pgx.Tx
	t                 *testing.T
	at                time.Time
	events            []grantIdentityEvent
	rule, env         rules.Rule
	ruleText, envText string
	ruleBody, envBody []byte
	signed            string
	key               GrantCIKey
	inserts           int
}

func (d *grantIdentityGateDB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) { return d, nil }
func (*grantIdentityGateDB) Commit(context.Context) error                             { return nil }
func (*grantIdentityGateDB) Rollback(context.Context) error                           { return nil }
func (*grantIdentityGateDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag("SELECT 1"), nil
}
func (d *grantIdentityGateDB) Query(_ context.Context, q string, args ...any) (pgx.Rows, error) {
	if strings.Contains(q, "SELECT seq, at") && args[0] == landingRepoStream {
		return &grantIdentityGateRows{events: d.events}, nil
	}
	return &grantIdentityGateRows{}, nil
}
func grantIdentityAssign(dest []any, values ...any) error {
	if len(dest) != len(values) {
		return fmt.Errorf("columns %d != %d", len(dest), len(values))
	}
	for i, v := range values {
		p := reflect.ValueOf(dest[i]).Elem()
		if v == nil {
			p.SetZero()
		} else {
			p.Set(reflect.ValueOf(v))
		}
	}
	return nil
}
func (d *grantIdentityGateDB) QueryRow(_ context.Context, q string, args ...any) pgx.Row {
	return funcRow(func(dest ...any) error {
		a := strings.Repeat("a", 40)
		switch {
		case strings.Contains(q, "SELECT m.tenant, COALESCE(t.plan_node_id"):
			return grantIdentityAssign(dest, "dev", "", "M-1")
		case strings.Contains(q, "COALESCE(plan_node_id,''), mission_id"):
			return grantIdentityAssign(dest, "", "M-1")
		case strings.Contains(q, "SELECT tenant FROM agentrt.mission"):
			return grantIdentityAssign(dest, "dev")
		case strings.Contains(q, "landing_stop"):
			return grantIdentityAssign(dest, 0, 0)
		case strings.Contains(q, "SELECT t.mission_id, COALESCE(t.plan_node_id"):
			return grantIdentityAssign(dest, "M-1", "", "CI identity regression", "implement", "go", "bifrost-platform", a, "accepted", "dev", "active", 1, d.at.Add(-2*time.Hour), false, "", 0, "",
				&d.env.Version, &d.env.Direction, &d.env.Effective, &d.env.Expires, &d.envText, d.envBody, &d.signed)
		case strings.Contains(q, "SELECT COALESCE(a.attempt_id"):
			accepted := d.at.Add(-90 * time.Minute)
			return grantIdentityAssign(dest, "a_00000000000000000000000001", a, &accepted)
		case strings.Contains(q, "string_agg(verifier_attempt_id"):
			return grantIdentityAssign(dest, "a_review")
		case strings.Contains(q, "FROM agentrt.rule") && !strings.Contains(q, "encode(sha256"):
			return grantIdentityAssign(dest, d.rule.Version, d.rule.Direction, d.rule.Effective, d.rule.Expires, d.ruleText, d.ruleBody, &d.signed)
		case strings.Contains(q, "body_json->'landing_ci_key'"):
			b, e := json.Marshal(landingCIKeyRecord{V: "1", Op: "land:t_00000000000000000000000001:1", GrantCIKey: d.key})
			if e != nil {
				return e
			}
			return grantIdentityAssign(dest, b)
		case strings.Contains(q, "encode(sha256"):
			return grantIdentityAssign(dest, strings.Repeat("d", 64))
		case strings.Contains(q, "SELECT last_seq, last_sha256"):
			return grantIdentityAssign(dest, int64(len(d.events)), strings.Repeat("0", 64))
		case strings.Contains(q, "INSERT INTO agentrt.event"):
			d.inserts++
			var body map[string]any
			if err := json.Unmarshal([]byte(args[9].(string)), &body); err != nil {
				return err
			}
			d.events = append(d.events, grantIdentityEvent{args[12].(time.Time), body})
			return grantIdentityAssign(dest, int64(len(d.events)))
		default:
			return fmt.Errorf("unexpected query: %s", q)
		}
	})
}

type grantIdentityGateRows struct {
	pgx.Rows
	events []grantIdentityEvent
	i      int
}

func (r *grantIdentityGateRows) Next() bool { r.i++; return r.i <= len(r.events) }
func (*grantIdentityGateRows) Close()       {}
func (*grantIdentityGateRows) Err() error   { return nil }
func (r *grantIdentityGateRows) Scan(dest ...any) error {
	e := r.events[r.i-1]
	*dest[0].(*int64) = int64(r.i)
	*dest[1].(*time.Time) = e.at
	for i, k := range []string{"landing", "landing_grant", "landing_grant_revoked", "landing_grant_use", "landing_grant_refused", "landing_ci_key"} {
		*dest[i+2].(*[]byte) = nil
		if v, ok := e.body[k]; ok {
			raw, err := json.Marshal(v)
			if err != nil {
				return err
			}
			*dest[i+2].(*[]byte) = raw
		}
	}
	return nil
}
func grantIdentityGateFixture(t *testing.T) *grantIdentityGateDB {
	at := DecisionAt()
	d := &grantIdentityGateDB{t: t, at: at, signed: "test-owner"}
	header := func(id string) string {
		return fmt.Sprintf("rule_id: %s\nrule_version: 1\ndirection: initial\neffective_at: %s\nexpires_at: %s\n", id, at.Add(-4*time.Hour).Format(time.RFC3339), at.Add(24*time.Hour).Format(time.RFC3339))
	}
	d.ruleText = header("landing:dev") + "tenant: dev\nrepos:\n  - repo: bifrost-platform\n    allow: [\"console/src/components/**/*.tsx\"]\n    deny: []\n    stg_release: none\nlimits: {max_files: 10, max_changed_lines: 300, max_landings_per_day: 5, max_in_flight_per_repo: 1}\n"
	d.envText = header("standing-envelope:dev") + "tenant: dev\nmax_action: branch\nbudget: {max_attempts: 40, max_wall_hours: 48}\nexecutors: [exec:claude, exec:codex]\nhosts: [h1, coordinator-mbp]\nexternal_hosts:\n  - host: coordinator-mbp\n    pin: external:coordinator\n    proposer: seat:coordinator\n    executor: exec:claude\n    provider: anthropic\n    repos: [bifrost-platform]\n    roles: [implement]\n"
	var err error
	d.rule, err = rules.Parse([]byte(d.ruleText))
	if err != nil {
		t.Fatal(err)
	}
	d.ruleBody, err = d.rule.BodyJSON()
	if err != nil {
		t.Fatal(err)
	}
	d.env, err = rules.Parse([]byte(d.envText))
	if err != nil {
		t.Fatal(err)
	}
	d.envBody, err = d.env.BodyJSON()
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"mode", "enabled"} {
		r := landing.Record{"v": landing.Version, "kind": kind, "op": "enabled:dev", "repo": "bifrost-platform"}
		if kind == "enabled" {
			r["rule_version"] = 1
		}
		d.events = append(d.events, grantIdentityEvent{at.Add(-3 * time.Hour), map[string]any{"landing": r}})
	}
	entries := reviewWaitEntries(t, at.Add(-time.Hour), at.Add(-30*time.Minute))
	for _, e := range entries {
		d.events = append(d.events, grantIdentityEvent{e.At, map[string]any{"landing": e.Record}})
	}
	op := "land:t_00000000000000000000000001:1"
	a, b := strings.Repeat("a", 40), strings.Repeat("b", 40)
	d.key = GrantCIKey{Repo: "bifrost-platform", Revision: b, Base: a, Siblings: map[string]string{"bifrost-ui": a, "bifrost-trade-infra": a}}
	approved := at.Add(-20 * time.Minute)
	digest := "sha256:" + strings.Repeat("e", 64)
	g := LandingGrant{V: "1", GrantID: "lg_appr_698", ApprovalID: "appr_698", ApprovalNumber: 698, Action: ActionCoreLanding, ParamsHash: strings.Repeat("f", 64), Home: "prod", Env: "prod", Tenant: "dev", Repo: "bifrost-platform", Task: "t_00000000000000000000000001", Attempt: "a_00000000000000000000000001", Op: op, A: a, Base: a, R: b, M0: a, CommitterDate: at.Add(-time.Hour).Format(time.RFC3339Nano), CandidateDigest: digest, MaterialDigest: digest, Waives: []string{"core", "floor_soft", "rule_paths"}, SoftAtRequest: []string{"core", "floor_soft", "rule_paths"}, Files: 1, Lines: 2, Rule: GrantRule{ID: "landing:dev", Version: 1, SHA256: strings.Repeat("d", 64)}, ChecksVersion: 1, CI: GrantCI{Pipeline: "bifrost-ci-platform", Run: "ci-review", UID: "old-uid", CreatedAt: at.Add(-30 * time.Minute).Format(time.RFC3339Nano), Status: "Succeeded", SHA: b}, Purpose: LandingGrantPurpose, ApprovedAt: approved.Format(time.RFC3339Nano), GrantedAt: approved.Format(time.RFC3339Nano), ExpiresAt: approved.Add(24 * time.Hour).Format(time.RFC3339Nano)}
	if _, err := parseLandingGrant(g.Body()); err != nil {
		t.Fatal(err)
	}
	d.events = append(d.events, grantIdentityEvent{approved, map[string]any{"landing_grant": g.Body()}})
	return d
}

// Serial: FreezeEventClock.
// Review 701 A-01: use the real gate, Append validation and wait projection;
// only SQL rows and external evidence are doubled. No PostgreSQL is required.
func TestC5A01CIObjectIdentity(t *testing.T) {
	for _, status := range []string{"Running", "Failed"} {
		for _, identity := range []struct {
			name, run, uid string
			age            time.Duration
			stale          bool
			unreadable     bool
		}{
			{"recreated", "ci-review", "recreated-uid", time.Minute, true, false},
			{"renamed", "new-ci", "new-uid", time.Minute, true, false},
			{"same object", "ci-review", "old-uid", 30 * time.Minute, false, false},
			{"missing name", "", "new-uid", time.Minute, false, false},
			{"missing UID", "new-ci", "", time.Minute, false, false},
			{"missing creation time", "new-ci", "new-uid", 0, false, false},
			{"older object", "old-ci", "older-uid", 40 * time.Minute, false, false},
			{"equal creation time", "new-ci", "new-uid", 30 * time.Minute, false, false},
			{"future creation time", "new-ci", "new-uid", -time.Hour, false, false},
			{"read failed", "new-ci", "new-uid", time.Minute, false, true},
		} {
			t.Run(status+"/"+identity.name, func(t *testing.T) {
				at := time.Now().UTC()
				defer FreezeEventClock(at)()
				db := grantIdentityGateFixture(t)
				created := at.Add(-identity.age)
				if identity.age == 0 {
					created = time.Time{}
				}
				src := LandingSource{
					Result: func(context.Context, string, string, string, string, string) (bool, []rules.LandingChange, error) {
						return true, []rules.LandingChange{{OldPath: "api/internal/agentrt/store/x.go", NewPath: "api/internal/agentrt/store/x.go", OldMode: "100644", NewMode: "100644", Added: 1, Deleted: 1}}, nil
					},
					History: func(context.Context, string, string, string, string) (landing.MainHistory, error) {
						return landing.MainHistory{State: landing.Never}, nil
					},
					GrantPatch: func(context.Context, string, string, string) (GrantPatch, error) { return GrantPatch{}, nil },
					GrantCI: func(context.Context, GrantCIKey, time.Time) (GrantCIEvidence, error) {
						if identity.unreadable {
							return GrantCIEvidence{}, fmt.Errorf("CI unavailable")
						}
						return GrantCIEvidence{Run: identity.run, UID: identity.uid, CreatedAt: created, Status: status}, nil
					},
				}
				op := "land:t_00000000000000000000000001:1"
				auth, decision, err := AuthorizeLandingPush(context.Background(), db, src, op, func(context.Context, landing.Operation) (LiveObservation, error) {
					return LiveObservation{Main: db.key.Base, CI: landing.CIRun{Run: identity.run, UID: identity.uid, R: db.key.Revision, CreatedAt: created, Status: status}}, nil
				})
				if (err != nil) != identity.unreadable || decision.Allowed || auth.Op != "" {
					t.Fatalf("auth=%+v decision=%+v err=%v", auth, decision, err)
				}
				state, err := LandingState(context.Background(), db, landingRepoStream)
				if err != nil {
					t.Fatal(err)
				}
				wait, err := readLandingWaitStatus(context.Background(), db, state, op, at)
				if err != nil {
					t.Fatal(err)
				}
				later, err := readLandingWaitStatus(context.Background(), db, state, op, at.Add(time.Hour))
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("gate=%s writes=%d shown=%s valid=%v W=%d -> %d E=%d -> %d", decision.Reason, db.inserts, wait.Shown, wait.Grants[0].Valid, wait.WaitingSeconds, later.WaitingSeconds, wait.ExecutionSeconds, later.ExecutionSeconds)
				if identity.stale {
					if decision.Reason != "grant_stale" || db.inserts != 1 || wait.Shown != "grant_refused" || wait.Grants[0].Valid || !wait.Grants[0].Refused || wait.GrantRefused != "grant_stale" {
						t.Fatal("new CI object did not end the grant")
					}
					if later.WaitingSeconds-wait.WaitingSeconds != 3600 || later.ExecutionSeconds != wait.ExecutionSeconds {
						t.Fatal("refused grant did not resume W and pause E")
					}
				} else if db.inserts != 0 || !wait.Grants[0].Valid || wait.Shown != "authorized" {
					t.Fatal("incomplete or non-new evidence manufactured a refusal")
				}
			})
		}
	}
}
