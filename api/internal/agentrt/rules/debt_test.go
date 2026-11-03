package rules_test

import (
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

var debtProjects = text("debt-projects", `projects:
  - {key: zeta, name: "Zeta", group: product, owns: ["zeta/**"], text_scan: strict}
  - {key: crew, name: "Crew", group: crew, owns: ["bifrost-platform/**"]}
acceptance_runners: {bifrost-platform: [api/internal/]}
budgets: {}
`)

// A debt-projects rule reads its projects, sorted by key, and nothing else;
// its body is the canonical JSON of what was read.
func TestParseDebtProjects(t *testing.T) {
	r := parse(t, debtProjects)
	body, err := r.BodyJSON()
	if err != nil {
		t.Fatal(err)
	}
	want := `{"acceptance_runners":{"bifrost-platform":["api/internal/"]},"budgets":{},"direction":"initial","effective_at":"2026-10-15T00:00:00Z","expires_at":"2027-01-15T00:00:00Z","projects":[{"group":"crew","key":"crew","name":"Crew","owns":["bifrost-platform/**"]},{"group":"product","key":"zeta","name":"Zeta","owns":["zeta/**"],"text_scan":"strict"}],"rule_id":"debt-projects","rule_version":1}`
	if r.Kind != rules.KindDebtProjects || string(body) != want {
		t.Fatalf("kind %s, body\n%s\nwant\n%s", r.Kind, body, want)
	}
	if p := r.DebtProjects.Project("zeta"); p == nil || p.TextScan != rules.TextScanStrict {
		t.Fatalf("zeta: %+v", p)
	}
	if r.DebtProjects.Project("nope") != nil {
		t.Fatal("a project the rule does not list was found")
	}
	for name, s := range map[string]string{
		"no projects":       text("debt-projects", "projects: []\n"),
		"a key twice":       strings.Replace(debtProjects, "key: zeta", "key: crew", 1),
		"a bad key":         strings.Replace(debtProjects, "key: zeta", "key: Zeta", 1),
		"another text_scan": strings.Replace(debtProjects, "text_scan: strict", "text_scan: loose", 1),
		"an unknown field":  strings.Replace(debtProjects, "group: crew,", "group: crew, color: red,", 1),
		"a budget":          strings.Replace(debtProjects, "budgets: {}", "budgets: {crew: 3}", 1),
		"an argument":       strings.Replace(debtProjects, "rule_id: debt-projects", "rule_id: debt-projects:stg", 1),
	} {
		if _, err := rules.Parse([]byte(s)); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
}
