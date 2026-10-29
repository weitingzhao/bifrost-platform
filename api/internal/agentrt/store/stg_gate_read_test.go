package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// Only SQL transport is doubled: CurrentRule, canonical event decoding and
// the final policy all run as they do on PostgreSQL JSONB rows.
type stgGateReadTx struct {
	pgx.Tx
	t    *testing.T
	at   time.Time
	rule rules.Rule
	text string
	body []byte
	op   string
	gate []byte
}

func (tx *stgGateReadTx) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	if !strings.Contains(query, "FROM agentrt.rule") || len(args) != 2 || args[0] != tx.rule.ID || !args[1].(time.Time).Equal(tx.at) {
		tx.t.Fatalf("unexpected current-rule read: %s %v", query, args)
	}
	return funcRow(func(dest ...any) error {
		signed := "test-owner"
		return grantIdentityAssign(dest, tx.rule.Version, tx.rule.Direction, tx.rule.Effective, tx.rule.Expires, tx.text, tx.body, &signed)
	})
}

func (tx *stgGateReadTx) Query(_ context.Context, query string, args ...any) (pgx.Rows, error) {
	if !strings.Contains(query, "body_json->'landing_gate'") || len(args) != 1 || args[0] != StgReleaseStream {
		tx.t.Fatalf("unexpected landing authority read: %s %v", query, args)
	}
	return &stgGateReadRows{op: tx.op, gate: tx.gate}, nil
}

type stgGateReadRows struct {
	pgx.Rows
	op   string
	gate []byte
	done bool
}

func (r *stgGateReadRows) Next() bool {
	if r.done {
		return false
	}
	r.done = true
	return true
}
func (*stgGateReadRows) Close()     {}
func (*stgGateReadRows) Err() error { return nil }
func (r *stgGateReadRows) Scan(dest ...any) error {
	return grantIdentityAssign(dest, r.op, r.gate, false)
}

func TestStgGateReadStoredRuleAuthority(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, release string
		version       any
		foreignOp     bool
		wantAuthority int64
		allowed       bool
	}{
		{"canonical integer", "bifrost-deliver-platform", 1, false, 1, true},
		{"current none", "none", 1, false, 1, false},
		{"missing version", "bifrost-deliver-platform", nil, false, 0, false},
		{"zero version", "bifrost-deliver-platform", 0, false, 0, false},
		{"negative version", "bifrost-deliver-platform", -1, false, 0, false},
		{"fractional version", "bifrost-deliver-platform", 1.5, false, 0, false},
		{"overflow version", "bifrost-deliver-platform", json.Number("9223372036854775808"), false, 0, false},
		{"string version", "bifrost-deliver-platform", "1", false, 0, false},
		{"other operation", "bifrost-deliver-platform", 1, true, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x := stgPolicy(t)
			var op string
			// The fixture has one ordinary landed operation, without a grant.
			for id := range x.s.Legacy.Operations {
				op = id
			}
			tx := &stgGateReadTx{t: t, at: x.at, op: op}
			tx.text = fmt.Sprintf("rule_id: landing:dev\nrule_version: 1\ndirection: initial\neffective_at: %s\nexpires_at: %s\ntenant: dev\nrepos:\n  - repo: bifrost-platform\n    allow: [\"console/src/components/**/*.tsx\"]\n    deny: []\n    stg_release: %s\nlimits: {max_files: 10, max_changed_lines: 300, max_landings_per_day: 5, max_in_flight_per_repo: 1}\n", x.at.Add(-time.Hour).Format(time.RFC3339), x.at.Add(time.Hour).Format(time.RFC3339), tc.release)
			var err error
			tx.rule, err = rules.Parse([]byte(tx.text))
			if err != nil {
				t.Fatal(err)
			}
			tx.body, err = tx.rule.BodyJSON()
			if err != nil {
				t.Fatal(err)
			}
			gateOp := op
			if tc.foreignOp {
				gateOp += "-other"
			}
			tx.gate, err = json.Marshal(map[string]any{"v": LandingGateVersion, "op": gateOp, "rule_version": tc.version,
				"decided_at": x.at.Format(time.RFC3339Nano), "quota_day": x.at.UTC().Format("2006-01-02"),
				"ci_run": "ci-landing", "ci_created_at": x.at.Add(-time.Minute).Format(time.RFC3339Nano)})
			if err != nil {
				t.Fatal(err)
			}
			f, err := stgReadGateFacts(context.Background(), tx, x.s, x.f.Bindings, x.at)
			if err != nil {
				t.Fatal(err)
			}
			r := f.Rules["dev"]
			if r == nil || r.Landing == nil {
				t.Fatal("current rule missing", r)
			}
			repo, found := r.Landing.Repo("bifrost-platform")
			if !found || r.Version != 1 || repo.STGRelease != tc.release {
				t.Fatalf("current rule version=%d stg_release=%q", r.Version, repo.STGRelease)
			}
			f.Environment, f.Live, f.Generation = x.f.Environment, x.f.Live, x.f.Generation
			d := stgEvaluateGate(x.s, stgTestBatch, x.o, f, x.at, false)
			a := f.Authority[op]
			if a.RuleVersion != tc.wantAuthority || a.UsedGrant || d.Allowed != tc.allowed || !tc.allowed && d.Why != "rule" {
				t.Fatalf("current rule version=%d stg_release=%q; stored landing authority=%+v; decision=%+v", r.Version, repo.STGRelease, a, d)
			}
		})
	}
}
