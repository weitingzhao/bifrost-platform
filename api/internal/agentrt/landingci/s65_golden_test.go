package landingci

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Captured with read.go/create.go from PROD 0cf36126 via a Go overlay.
// No regeneration mode: changing production code cannot update this oracle.
// W-31 B5 deliberately edited only the two "ensure" rows: each platform spec
// gains repo-url (DESIGN-B5-v3 §4.2); every read row is unchanged.
func TestS65T47PlatformGolden(t *testing.T) {
	t.Parallel()
	cases := map[string]func(*unstructured.Unstructured){
		"green":          func(*unstructured.Unstructured) {},
		"missing status": func(o *unstructured.Unstructured) { delete(o.Object, "status") },
		"running": func(o *unstructured.Unstructured) {
			setField(t, o, []any{map[string]any{"type": "Succeeded", "status": "Unknown", "reason": "Running"}}, "status", "conditions")
		},
		"failed": func(o *unstructured.Unstructured) {
			setField(t, o, []any{map[string]any{"type": "Succeeded", "status": "False"}}, "status", "conditions")
		},
		"missing results":  func(o *unstructured.Unstructured) { unstructured.RemoveNestedField(o.Object, "status", "results") },
		"malformed result": func(o *unstructured.Unstructured) { changeNamed(t, o, "status", "results", "ui-commit", "main") },
		"mismatched result": func(o *unstructured.Unstructured) {
			changeNamed(t, o, "status", "results", "platform-commit", factSHA2)
		},
		"duplicate result": func(o *unstructured.Unstructured) {
			r, _, _ := unstructured.NestedSlice(o.Object, "status", "results")
			setField(t, o, append(r, r[0]), "status", "results")
		},
		"duplicate revision": func(o *unstructured.Unstructured) {
			p, _, _ := unstructured.NestedSlice(o.Object, "spec", "params")
			setField(t, o, append(p, map[string]any{"name": "revision", "value": factSHA2}), "spec", "params")
		},
		"wrong revision": func(o *unstructured.Unstructured) { changeNamed(t, o, "spec", "params", "revision", factSHA2) },
		"wrong repo label": func(o *unstructured.Unstructured) {
			o.SetLabels(map[string]string{"bifrost.io/repo": "bifrost-trade-infra"})
		},
		"no repo label":         func(o *unstructured.Unstructured) { o.SetLabels(nil) },
		"inline":                func(o *unstructured.Unstructured) { setField(t, o, map[string]any{}, "spec", "pipelineSpec") },
		"wrong pipeline":        func(o *unstructured.Unstructured) { setField(t, o, "bifrost-ci-infra", "spec", "pipelineRef", "name") },
		"resolver":              func(o *unstructured.Unstructured) { setField(t, o, "git", "spec", "pipelineRef", "resolver") },
		"unmarked landing name": func(o *unstructured.Unstructured) { o.SetName(landing.CIName("golden", 1)) },
		"unmarked infra name":   func(o *unstructured.Unstructured) { o.SetName("landing-ci-infra-0123456789abcdef01234567-1") },
		"marked landing name": func(o *unstructured.Unstructured) {
			o.SetName(landing.CIName("golden", 1))
			labels := o.GetLabels()
			labels["bifrost.io/trigger"] = "agentrt-landing-drill"
			o.SetLabels(labels)
		},
		"tied green": func(o *unstructured.Unstructured) { o.SetCreationTimestamp(metav1.NewTime(s65At.Add(-time.Minute))) },
		"tied red": func(o *unstructured.Unstructured) {
			o.SetCreationTimestamp(metav1.NewTime(s65At.Add(-time.Minute)))
			setField(t, o, []any{map[string]any{"type": "Succeeded", "status": "False"}}, "status", "conditions")
		},
		"at preparation fence": func(o *unstructured.Unstructured) { o.SetCreationTimestamp(metav1.NewTime(s65At.Add(-time.Hour))) },
	}
	snapshot := map[string]any{}
	for name, mutate := range cases {
		old, newest := greenRun(t, "old", s65At.Add(-time.Minute)), greenRun(t, "new", s65At)
		for _, o := range []*unstructured.Unstructured{old, newest} {
			p, _, _ := unstructured.NestedSlice(o.Object, "spec", "params")
			setField(t, o, append(p, map[string]any{"name": "repo-url", "value": PlatformRepoURL}), "spec", "params")
		}
		mutate(newest)
		c, _ := drillService(t, old, newest)
		legacy, err := c.LandingCI(context.Background(), factSHA1, s65At.Add(-time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		pinned, err := c.Read(context.Background(), pinnedKey(), s65At.Add(-time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		released, err := c.ReadRelease(context.Background(), factSHA1)
		if err != nil {
			t.Fatal(err)
		}
		snapshot[name] = map[string]any{"legacy": platformGoldenRun(legacy), "pinned": map[string]any{"run": platformGoldenRun(pinned.CIRun), "results": pinned.Results, "reason": pinned.Reason}, "release": map[string]any{"run": platformGoldenRun(released.CIRun), "results": released.Results, "reason": released.Reason, "pipeline": released.PipelineRef, "repo_url": released.RepoURL}}
	}
	for _, mode := range []string{"legacy", "pinned"} {
		c, d := drillService(t)
		name := landing.CIName("golden", 1)
		var err error
		if mode == "legacy" {
			err = c.EnsureLandingCI(context.Background(), name, factSHA1)
		} else {
			err = c.Ensure(context.Background(), name, pinnedKey())
		}
		if err != nil {
			t.Fatal(err)
		}
		obj, err := d.Resource(pipelineRunGVR).Namespace("cicd").Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		snapshot["ensure "+mode] = obj.Object
	}
	var got []byte
	keys := make([]string, 0, len(snapshot))
	for key := range snapshot {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		row, err := json.Marshal(map[string]any{key: snapshot[key]})
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, row...)
		got = append(got, '\n')
	}
	want, err := os.ReadFile("testdata/s65-platform.golden.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("platform evidence/spec changed from PROD golden\ngot: %s", got)
	}
}
func platformGoldenRun(ci landing.CIRun) map[string]any {
	return map[string]any{"run": ci.Run, "uid": ci.UID, "revision": ci.R, "status": ci.Status, "created_at": ci.CreatedAt.UTC().Format(time.RFC3339Nano), "platform": ci.PlatformCommit, "ui": ci.UICommit, "infra": ci.InfraCommit}
}
