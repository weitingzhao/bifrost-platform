package mustrefuse_test

import (
	"strings"
	"testing"
)

// Owner 2026-11-15: no second reader; the author proves a judge change with
// machine facts (must-refuse set, decision snapshots). Every case is refused,
// under a policy with every relaxing field switched on, in the words the
// case names.
func TestEveryCaseIsRefused(t *testing.T) {
	seen := map[string]bool{}
	for _, tc := range cases {
		if seen[tc.name] || tc.origin == "" || tc.want == "" || tc.wrong == nil {
			t.Fatalf("case %q: a case has its own name, an origin, something wrong and the words of its refusal", tc.name)
		}
		seen[tc.name] = true
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			tc.wrong(w)
			d := w.decide(tc.start)
			if d.Auto {
				t.Fatalf("COVERED, and it must be refused (%s). Clauses: %s", tc.origin, d.ClauseText())
			}
			if reasons := strings.Join(d.Reasons, "\n"); !strings.Contains(reasons, tc.want) {
				t.Fatalf("refused, but not for %q:\n%s", tc.want, reasons)
			}
		})
	}
}

// Owner 2026-11-15: no second reader; the author proves a judge change with
// machine facts (must-refuse set, decision snapshots). A case that is
// dropped refuses nothing: the set has as many cases as caseCount says.
func TestNoCaseIsDropped(t *testing.T) {
	if len(cases) != caseCount {
		t.Fatalf("the set has %d cases and caseCount says %d: cases are added together with the count, never dropped", len(cases), caseCount)
	}
}

// Owner 2026-11-15: no second reader; the author proves a judge change with
// machine facts (must-refuse set, decision snapshots). The positive control
// of the whole set: with nothing wrong, each starting request is covered,
// and a trust-anchor change of the self-checked repo with CI green is too,
// so the policy the cases run under really is the relaxed one.
func TestTheStartingRequestsAreCovered(t *testing.T) {
	for name, r := range map[string]request{"Trade STG": releaseStg, "platform STG": releasePlatform, "platform PROD": releasePlatformProd, "apply": applyPlan, "migration": runMigration} {
		if d := newWorld(t).decide(r); !d.Auto {
			t.Errorf("%s: not covered: %q", name, d.Reasons)
		}
	}
	w := newWorld(t)
	w.facts.Files[platform] = []string{"api/internal/releasepolicy/engine.go", "api/internal/releasepolicy/engine_test.go"}
	if d := w.decide(releasePlatform); !d.Auto || !strings.Contains(d.ClauseText(), "trust_anchor_self_checked") {
		t.Errorf("a self-checked trust-anchor change: auto = %v clauses = %s reasons = %q", d.Auto, d.ClauseText(), d.Reasons)
	}
	w = newWorld(t)
	w.platformMigration(reversibleSQL)
	if d := w.decide(releasePlatformProd); !d.Auto || !strings.Contains(d.ClauseText(), "ddl_reversible") {
		t.Errorf("a new migration that cannot destroy rows: auto = %v clauses = %s reasons = %q", d.Auto, d.ClauseText(), d.Reasons)
	}
	w = newWorld(t)
	w.migration("CREATE TABLE orders (id bigint);\n", "CREATE TABLE orders (id bigint);\nALTER TABLE orders ADD COLUMN note text;\n")
	if d := w.decide(releaseStg); !d.Auto || !strings.Contains(d.ClauseText(), "additive_ddl") {
		t.Errorf("an additive migration: auto = %v clauses = %s reasons = %q", d.Auto, d.ClauseText(), d.Reasons)
	}
}
