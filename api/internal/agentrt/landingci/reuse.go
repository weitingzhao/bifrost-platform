package landingci

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"time"
)

// reuseResult checks the tree-reuse contract against the reader's existing
// bifrost-ci-platform run list. Every new run reports platform-tree, the
// 40-hex tree it tested. A reuse of P is Succeeded, reports platform-commit=P,
// resolved ui-commit and infra-commit, platform-tree, and tested-by naming the
// original run; api-scope, api-packages, api-store-run and api-scope-why are
// copied from that original. The original must be listed, have no tested-by,
// pass releaseResult for its own revision, be green, have the same tree and
// sibling commits, and have been created strictly before the reuse.
// Its scope decides acceptance: full, incremental only while FullFresh, or
// console as Partial. Invalid reuse is Unknown/ci_reuse. Runs without
// platform-tree retain the legacy behavior.
func reuseResult(item unstructured.Unstructured, items []unstructured.Unstructured, revision string) Evidence {
	out := releaseResult(item, revision)
	values, counts := namedValues(item, "status", "results")
	if counts["tested-by"] == 0 {
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
	_, originalCounts := namedValues(*original, "status", "results")
	if originalCounts["tested-by"] != 0 || r.Status != "Succeeded" || r.Reason != "" || r.CreatedAt.IsZero() || !r.CreatedAt.Before(out.CreatedAt) {
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

// landingReuse additionally binds an incremental original to the landing base.
func landingReuse(item unstructured.Unstructured, items []unstructured.Unstructured, revision string) Evidence {
	out := reuseResult(item, items, revision)
	if _, named := out.Results["tested-by"]; !named || out.Reason != "" {
		return out
	}
	e := ReleaseEvidence{ReadAt: time.Now().UTC()}
	e.LastFullRun, e.LastFullCommit, e.LastFullAt = lastFull(items, e.ReadAt)
	if !e.releaseScope(out.Results) {
		out.Status, out.Reason = "Unknown", "ci_reuse"
		return out
	}
	params, counts := namedValues(item, "spec", "params")
	if out.Results[APIScopeResult] == APIScopeIncremental && counts["base"] > 0 {
		for _, original := range items {
			if original.GetName() == out.Results["tested-by"] {
				p, n := namedValues(original, "spec", "params")
				if counts["base"] != 1 || n["base"] != 1 || p["base"] != params["base"] {
					out.Status, out.Reason = "Unknown", "ci_reuse"
				}
			}
		}
	}
	return out
}
