//bifrost:owner coordination
package server

import (
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/weitingzhao/bifrost-platform/api/internal/bus/regressions"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
)

// 2026-11-17: the refused landing's reason is the land TaskRun's result
// "refusal" (pipeline-land.yaml, CI line 08:53); the wake reads it there.
func TestTaskRunResult(t *testing.T) {
	tr := unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{"params": []any{
		map[string]any{"name": "branch", "value": "refs/heads/ready/x"},
	}}, "status": map[string]any{"results": []any{
		map[string]any{"name": "outcome", "value": ""},
		map[string]any{"name": "refusal", "value": "refused: commit bc6b49b22fa9 has no Change-Id trailer"},
	}}}}
	if got := taskRunValue([]unstructured.Unstructured{{Object: map[string]any{}}, tr}, "refusal"); got != "refused: commit bc6b49b22fa9 has no Change-Id trailer" {
		t.Fatalf("got %q", got)
	}
	// A name the task did not write as a result is read from its params
	// (the tripwire's branch, 2026-11-17).
	if got := taskRunValue([]unstructured.Unstructured{tr}, "branch"); got != "refs/heads/ready/x" {
		t.Fatalf("a param: %q", got)
	}
	if got := taskRunValue([]unstructured.Unstructured{tr}, "absent"); got != "" {
		t.Fatalf("a result the task did not write: %q", got)
	}
}

// 2026-11-17: the release context lists the open regressions of its
// repository (5d0ac234 reached PROD carrying a known one); each keeps which
// commit, what is broken, who said so and when, and none is an empty list.
func TestKnownRegressionsCarryWhatTheReleaserReads(t *testing.T) {
	at := time.Date(2026, 11, 17, 21, 29, 0, 0, time.FixedZone("CST", -6*3600))
	got := knownRegressions([]regressions.Regression{{Repo: "bifrost-platform", Introduced: "a6862d84", Impact: "module activity", By: "thread:local_x", KnownAt: at}})
	want := delivery.KnownRegression{Introduced: "a6862d84", Impact: "module activity", By: "thread:local_x", KnownAt: "2026-11-18T03:29:00Z"}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %+v", got)
	}
	if none := knownRegressions(nil); none == nil || len(none) != 0 {
		t.Fatalf("none open = %#v", none)
	}
}
