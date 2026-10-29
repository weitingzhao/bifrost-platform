package landingci

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Keep the reader's transient Unknown separate from definite malformed
// terminal results. All stages retain the same Kubernetes UID.
func TestReview711ReleaseCIUnknownRunningSucceeded(t *testing.T) {
	t.Parallel()
	o := releaseCI(t, "same-ci", time.Now().UTC().Add(-time.Hour))
	terminal := o.DeepCopy()
	read := func() ReleaseEvidence {
		t.Helper()
		c, _ := drillService(t, o)
		e, err := c.ReadRelease(context.Background(), factSHA1)
		if err != nil || e.UID != string(o.GetUID()) {
			t.Fatal("read or identity", e, err)
		}
		return e
	}
	if e := read(); e.Status != "Succeeded" || e.Reason != "" {
		t.Fatal("same-entry success control", e)
	}
	unstructured.RemoveNestedField(o.Object, "status")
	if e := read(); e.Status != "Unknown" || e.Reason != "" {
		t.Fatal("new CI must be transient Unknown", e)
	}
	setField(t, o, []any{map[string]any{"type": "Succeeded", "status": "Unknown", "reason": "Running"}}, "status", "conditions")
	if e := read(); e.Status != "Running" || e.Reason != "" {
		t.Fatal("same UID running", e)
	}
	o = terminal.DeepCopy()
	if e := read(); e.Status != "Succeeded" || e.Reason != "" {
		t.Fatal("same UID success", e)
	}
	for _, mode := range []string{"missing", "duplicate"} {
		o = terminal.DeepCopy()
		if mode == "missing" {
			unstructured.RemoveNestedField(o.Object, "status", "results")
		} else {
			r, _, _ := unstructured.NestedSlice(o.Object, "status", "results")
			setField(t, o, append(r, r[0]), "status", "results")
		}
		if e := read(); e.Status != "Unknown" || e.Reason != "ci_results" {
			t.Fatal("terminal malformed results must remain definite errors", mode, e)
		}
	}
}
