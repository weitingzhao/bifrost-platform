package landingci

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
)

// 2026-11-06 B5 stayed blocked on a run whose api-scope value was
// "incremental\n" (fixed in CI by df534843). A result value reads without
// its surrounding whitespace, so that run reads as incremental.
func TestReleaseCIResultTrailingNewline(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	for name, tc := range map[string]struct {
		objs          []runtime.Object
		status, scope string
	}{
		"full with a newline": {[]runtime.Object{withScope(t, releaseCI(t, "full", now.Add(-time.Hour)), "full\n")}, "Succeeded", APIScopeFull},
		"incremental with a newline": {[]runtime.Object{withScope(t, otherCI(t, "other-full", now.Add(-10*time.Hour)), APIScopeFull),
			withScope(t, releaseCI(t, "inc", now.Add(-time.Hour)), " incremental\n")}, "Succeeded", APIScopeIncremental},
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := drillService(t, tc.objs...)
			got, err := c.ReadRelease(context.Background(), factSHA1)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != tc.status || got.Reason != "" || got.Scope() != tc.scope || got.Results[APIScopeResult] != tc.scope {
				t.Fatalf("got status=%s reason=%q scope=%q", got.Status, got.Reason, got.Scope())
			}
			if _, err := got.Record(); err != nil {
				t.Fatal("not accepted evidence:", err)
			}
		})
	}
}
