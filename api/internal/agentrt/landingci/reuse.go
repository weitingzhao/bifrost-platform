package landingci

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ValidReuse verifies a tested-by run against the listed bifrost-ci-platform
// runs. Callers apply their own scope policy.
func ValidReuse(item unstructured.Unstructured, items []unstructured.Unstructured, revision string) bool {
	if !ClaimsReuse(item) {
		return false
	}
	out := reuseResult(item, items, revision)
	return out.Status == "Succeeded" && out.Reason == ""
}

// reuseResult checks the tree-reuse contract against the reader's existing
// bifrost-ci-platform run list. Every new run reports platform-tree, the
// 40-hex tree it tested. A reuse of P is Succeeded, reports platform-commit=P,
// resolved ui-commit and infra-commit, platform-tree, and tested-by naming the
// original run; api-scope, api-packages, api-store-run and api-scope-why are
// copied from that original. The original must be listed, have no tested-by,
// pass releaseResult for its own revision, be green, have the same tree and
// sibling commits, and have been created strictly before the reuse.
// Its scope decides acceptance: full, incremental, or
// console as Partial. Invalid reuse is Unknown/ci_reuse. Runs without
// platform-tree retain the legacy behavior.
func reuseResult(item unstructured.Unstructured, items []unstructured.Unstructured, revision string) Evidence {
	out := releaseResult(item, revision)
	values, counts := namedValues(item, "status", "results")
	if !ClaimsReuse(item) {
		return out
	}
	fail := func() Evidence {
		out.Status, out.Reason = "Unknown", "ci_reuse"
		return out
	}
	if out.Status != "Succeeded" || out.Reason != "" || counts["tested-by"] != 1 || !fullSHA.MatchString(values["platform-tree"]) || out.CreatedAt.IsZero() {
		return fail()
	}
	var original *unstructured.Unstructured
	for i := range items {
		if items[i].GetName() == values["tested-by"] {
			if original != nil {
				return fail()
			}
			original = &items[i]
		}
	}
	if original == nil {
		return fail()
	}
	params, _ := namedValues(*original, "spec", "params")
	r := releaseResult(*original, params["revision"])
	if ClaimsReuse(*original) || r.Status != "Succeeded" || r.Reason != "" || r.CreatedAt.IsZero() || !r.CreatedAt.Before(out.CreatedAt) {
		return fail()
	}
	for _, name := range []string{"platform-tree", "ui-commit", "infra-commit", APIScopeResult, APIPackagesResult, APIStoreRunResult, APIScopeWhyResult} {
		a, aok := values[name]
		b, bok := r.Results[name]
		if aok != bok || a != b {
			return fail()
		}
	}
	return out
}
