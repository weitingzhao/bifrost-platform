package landingci

import (
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

func greenRun(t *testing.T, name string, at time.Time) *unstructured.Unstructured {
	t.Helper()
	obj := drillCI(t, name, "True", at)
	obj.SetUID(types.UID("uid-" + name))
	if err := unstructured.SetNestedSlice(obj.Object, []any{
		map[string]any{"name": "platform-commit", "value": factSHA1},
		map[string]any{"name": "ui-commit", "value": factSHA2},
		map[string]any{"name": "infra-commit", "value": factSHA2},
	}, "status", "results"); err != nil {
		t.Fatal(err)
	}
	return obj
}

func releaseCI(t *testing.T, name string, at time.Time) *unstructured.Unstructured {
	t.Helper()
	o := greenRun(t, name, at)
	params, _, _ := unstructured.NestedSlice(o.Object, "spec", "params")
	setField(t, o, append(params, map[string]any{"name": "repo-url", "value": platformRepoURL}), "spec", "params")
	return o
}

// TD-533: the reuse step writes an empty tested-by when it reused nothing. Only
// a tested-by naming a run claims a reuse; a repeated one still does.
func TestClaimsReuseNeedsANamedRun(t *testing.T) {
	at := time.Now().UTC().Add(-time.Hour)
	for _, tc := range []struct {
		name   string
		values []string
		want   bool
	}{
		{"absent", nil, false},
		{"empty", []string{""}, false},
		{"blank", []string{"  "}, false},
		{"named", []string{"original"}, true},
		{"repeated", []string{"", ""}, true},
	} {
		o := releaseCI(t, "run", at)
		entries, _, _ := unstructured.NestedSlice(o.Object, "status", "results")
		for _, v := range tc.values {
			entries = append(entries, map[string]any{"name": "tested-by", "value": v})
		}
		setField(t, o, entries, "status", "results")
		if got := ClaimsReuse(*o); got != tc.want {
			t.Errorf("%s: ClaimsReuse %v, want %v", tc.name, got, tc.want)
		}
	}
}
