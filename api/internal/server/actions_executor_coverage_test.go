//bifrost:owner approvals

package server

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/workactions"
)

// 2026-11-16, card #585: ensure_bifrost_namespaces was added to the catalog
// (runner platform, tier C) without an executor. The Owner approved it and the
// platform failed it with "no executor for ensure_bifrost_namespaces". Every
// catalog action the platform runs after approval must have an executor bound.
//
// Known gaps, listed so the list can only shrink: the four release-policy
// actions are tier B and called directly through their guarded routes, so no
// card reaches them today; binding them belongs to the release-policy owners.
var executorGaps = map[string]bool{
	"release_policy_install": true,
	"release_policy_stage":   true,
	"release_freeze":         true,
	"release_unfreeze":       true,
}

func TestEveryPlatformRunActionHasAnExecutor(t *testing.T) {
	s := &Server{work: &workactions.Handler{Svc: &workactions.Service{}}}
	s.bindActionExecutors()
	for _, a := range actions.Catalog() {
		if a.Runner != "" && a.Runner != actions.RunnerPlatform {
			continue
		}
		if executorGaps[a.ID] {
			if actions.HasExecutor(a.ID) {
				t.Errorf("%s now has an executor; remove it from executorGaps", a.ID)
			}
			continue
		}
		if !actions.HasExecutor(a.ID) {
			t.Errorf("%s: runner platform but no executor; an approved card would fail with \"no executor for %s\"", a.ID, a.ID)
		}
	}
}
