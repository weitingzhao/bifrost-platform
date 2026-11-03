package store

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// The acceptance spec of a debt (DESIGN-tech-debt-runtime part 2 §6.4): a
// closed grammar, an unknown key refused, as plan/rule.go reads a done rule.
// Only its shape is read here; judging it is 5.8-4's. The runtime runs no
// command of it, ever: ci_test reads CI evidence, promql a read-only query,
// owner is a card, manual a record and its second writer's check.
//
//	{"kind":"ci_test","repo":"<repo>","path":"<file>","name":"<test>"}
//	{"kind":"promql","query":"<expr>","expect":{"eq"|"lt"|"le"|"gt"|"ge":<int>}}
//	{"kind":"owner","say":"<where, what to run>","expect":"<what passes>"}
//	{"kind":"manual","say":"<what to check>","expect":"<what passes>"}
const maxAcceptanceItems = 20

var acceptanceKeys = map[string][]string{
	"ci_test": {"kind", "repo", "path", "name"},
	"promql":  {"kind", "query", "expect"},
	"owner":   {"kind", "say", "expect"},
	"manual":  {"kind", "say", "expect"},
}

var promqlComparisons = []string{"eq", "lt", "le", "gt", "ge"}

func checkAcceptance(spec []any) error {
	if len(spec) > maxAcceptanceItems {
		return invalid("an acceptance spec has at most %d items, not %d", maxAcceptanceItems, len(spec))
	}
	for i, raw := range spec {
		what := fmt.Sprintf("acceptance item %d", i+1)
		item, ok := raw.(map[string]any)
		if !ok {
			return invalid("%s is an object", what)
		}
		kind, _ := item["kind"].(string)
		keys, ok := acceptanceKeys[kind]
		if !ok {
			return invalid("%s is of kind %q, not one of ci_test, promql, owner, manual", what, kind)
		}
		for k := range item {
			if !slices.Contains(keys, k) {
				return invalid("%s (%s) takes %v, not %q", what, kind, keys, k)
			}
		}
		for _, k := range keys {
			if k == "kind" || (kind == "promql" && k == "expect") {
				continue
			}
			s, ok := item[k].(string)
			if !ok || strings.TrimSpace(s) == "" || utf8.RuneCountInString(s) > 2000 {
				return invalid("%s (%s) needs %s: a text of 1 to 2000 characters", what, kind, k)
			}
			if err := checkDebtText(fmt.Sprintf("%s's %s", what, k), s, 8000); err != nil {
				return err
			}
		}
		if kind == "ci_test" && !debtRepoRE.MatchString(item["repo"].(string)) {
			return invalid("%s: %q is not a repository name", what, item["repo"])
		}
		if kind == "promql" {
			exp, ok := item["expect"].(map[string]any)
			if !ok || len(exp) != 1 {
				return invalid("%s (promql) expects one comparison of %v with a whole number", what, promqlComparisons)
			}
			for op, n := range exp {
				if !slices.Contains(promqlComparisons, op) || !ofKind(n, kInt) {
					return invalid("%s (promql) expects one comparison of %v with a whole number", what, promqlComparisons)
				}
			}
		}
	}
	return nil
}
