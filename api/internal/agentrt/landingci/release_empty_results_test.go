package landingci

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

// A full green run of the current bifrost-ci-platform names failed-tests,
// api-packages, api-store-run and api-scope-why, empty on a full run with no
// failure (infra test-api since 2026-11-06). B5 refused every such run with
// ci_results; those four may be empty, every other result may not.
func TestReleaseCIEmptyOptionalResults(t *testing.T) {
	t.Parallel()
	at := time.Now().UTC().Add(-time.Hour)
	with := func(o *unstructured.Unstructured, extra ...map[string]any) *unstructured.Unstructured {
		r, _, _ := unstructured.NestedSlice(o.Object, "status", "results")
		for _, e := range extra {
			r = append(r, e)
		}
		setField(t, o, r, "status", "results")
		return o
	}
	empty := func(name string) map[string]any { return map[string]any{"name": name, "value": ""} }
	for name, tc := range map[string]struct {
		obj            runtime.Object
		status, reason string
	}{
		"full green with the four empty": {with(withScope(t, releaseCI(t, "full", at), "full"),
			empty(FailedTestsResult), empty(APIPackagesResult), empty(APIStoreRunResult), empty(APIScopeWhyResult)), "Succeeded", ""},
		"another result empty": {with(withScope(t, releaseCI(t, "odd", at), "full"), empty("ui-digest")), "Unknown", "ci_results"},
		"empty scope":          {withScope(t, releaseCI(t, "noscope", at), ""), "Unknown", "ci_results"},
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := drillService(t, tc.obj)
			got, err := c.ReadRelease(context.Background(), factSHA1)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != tc.status || got.Reason != tc.reason {
				t.Fatalf("got status=%s reason=%q", got.Status, got.Reason)
			}
		})
	}
}
