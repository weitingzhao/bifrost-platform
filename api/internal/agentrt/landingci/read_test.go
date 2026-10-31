package landingci

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

func pinnedKey() Key {
	return Key{Repo: "bifrost-platform", Revision: factSHA1, Base: factSHA2, Siblings: map[string]string{"bifrost-ui": factSHA2, "bifrost-trade-infra": factSHA2}, Generation: 1}
}
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
func setField(t *testing.T, obj *unstructured.Unstructured, value any, fields ...string) {
	t.Helper()
	if err := unstructured.SetNestedField(obj.Object, value, fields...); err != nil {
		t.Fatal(err)
	}
}
func TestReadSelectsNewestBeforeCheckingEvidence(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, status, reason string
		mutate               func(*testing.T, *unstructured.Unstructured)
	}{
		{"running", "Running", "", func(t *testing.T, o *unstructured.Unstructured) {
			setField(t, o, []any{map[string]any{"type": "Succeeded", "status": "Unknown", "reason": "Running"}}, "status", "conditions")
			unstructured.RemoveNestedField(o.Object, "status", "results")
		}},
		{"failed", "Failed", "", func(t *testing.T, o *unstructured.Unstructured) {
			setField(t, o, []any{map[string]any{"type": "Succeeded", "status": "False"}}, "status", "conditions")
			unstructured.RemoveNestedField(o.Object, "status", "results")
		}},
		{"no status", "Unknown", "", func(t *testing.T, o *unstructured.Unstructured) { delete(o.Object, "status") }},
		{"remote pipeline", "Unknown", "ci_pipeline_mismatch", func(t *testing.T, o *unstructured.Unstructured) {
			setField(t, o, "git", "spec", "pipelineRef", "resolver")
		}},
		{"no results", "Unknown", "ci_result_missing", func(t *testing.T, o *unstructured.Unstructured) {
			unstructured.RemoveNestedField(o.Object, "status", "results")
		}},
		{"candidate mismatch", "Failed", "ci_result_mismatch", func(t *testing.T, o *unstructured.Unstructured) {
			results, _, _ := unstructured.NestedSlice(o.Object, "status", "results")
			results[0].(map[string]any)["value"] = factSHA2
			setField(t, o, results, "status", "results")
		}},
		{"sibling mismatch", "Failed", "ci_result_mismatch", func(t *testing.T, o *unstructured.Unstructured) {
			results, _, _ := unstructured.NestedSlice(o.Object, "status", "results")
			results[2].(map[string]any)["value"] = factSHA1
			setField(t, o, results, "status", "results")
		}},
		{"invalid sibling", "Failed", "ci_result_mismatch", func(t *testing.T, o *unstructured.Unstructured) {
			results, _, _ := unstructured.NestedSlice(o.Object, "status", "results")
			results[1].(map[string]any)["value"] = "main"
			setField(t, o, results, "status", "results")
		}},
		{"duplicate", "Unknown", "ci_result_missing", func(t *testing.T, o *unstructured.Unstructured) {
			results, _, _ := unstructured.NestedSlice(o.Object, "status", "results")
			setField(t, o, append(results, results[0]), "status", "results")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			old, latest := greenRun(t, "old", at.Add(-time.Minute)), greenRun(t, "new", at)
			read := func() Evidence {
				t.Helper()
				c, _ := drillService(t, old, latest)
				got, err := c.Read(context.Background(), pinnedKey(), at.Add(-time.Hour))
				if err != nil {
					t.Fatal(err)
				}
				return got
			}
			if got := read(); got.Status != "Succeeded" || got.Run != "new" || got.UID != "uid-new" || got.InfraCommit != factSHA2 || len(got.Results) != 3 {
				t.Fatal("control", got)
			}
			tc.mutate(t, latest)
			if got := read(); got.Run != "new" || got.Status != tc.status || got.Reason != tc.reason {
				t.Fatal(got)
			}
		})
	}
}
func TestReadFiltersBeforeLatestAndPreservesTies(t *testing.T) {
	t.Parallel()
	at := time.Now().UTC().Truncate(time.Second)
	for name, mutate := range map[string]func(*testing.T, *unstructured.Unstructured){
		"wrong pipeline": func(t *testing.T, o *unstructured.Unstructured) {
			setField(t, o, "bifrost-ci-infra", "spec", "pipelineRef", "name")
		},
		"inline": func(t *testing.T, o *unstructured.Unstructured) {
			setField(t, o, map[string]any{}, "spec", "pipelineSpec")
		},
		"wrong revision": func(t *testing.T, o *unstructured.Unstructured) {
			setField(t, o, []any{map[string]any{"name": "revision", "value": factSHA2}}, "spec", "params")
		},
		"wrong repo": func(t *testing.T, o *unstructured.Unstructured) {
			o.SetLabels(map[string]string{"bifrost.io/repo": "bifrost-trade-infra"})
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			old, latest := greenRun(t, "old", at.Add(-time.Minute)), greenRun(t, "new", at)
			c, _ := drillService(t, old, latest)
			if got, err := c.Read(context.Background(), pinnedKey(), time.Time{}); err != nil || got.Run != "new" {
				t.Fatal("control", got, err)
			}
			mutate(t, latest)
			c, _ = drillService(t, old, latest)
			if got, err := c.Read(context.Background(), pinnedKey(), time.Time{}); err != nil || got.Run != "old" {
				t.Fatal(got, err)
			}
		})
	}
	c, _ := drillService(t, greenRun(t, "a", at), greenRun(t, "z", at))
	if got, err := c.Read(context.Background(), pinnedKey(), time.Time{}); err != nil || got.Run != "z" || got.Status != "Succeeded" {
		t.Fatal("control", got, err)
	}
	c, _ = drillService(t, greenRun(t, "a", at), drillCI(t, "z", "False", at))
	if got, err := c.Read(context.Background(), pinnedKey(), time.Time{}); err != nil || got.Status != "Unknown" {
		t.Fatal(got, err)
	}
}
func TestReadPaginationAndInvalidKeys(t *testing.T) {
	t.Parallel()
	at := time.Now().UTC().Truncate(time.Second)
	c, d := drillService(t)
	pages := 0
	c.Dynamic = func() (dynamic.Interface, error) {
		return pagedClient{Interface: d, list: func(opts metav1.ListOptions) (*unstructured.UnstructuredList, error) {
			if opts.Limit != 500 {
				t.Fatal("missing page limit", opts)
			}
			list := &unstructured.UnstructuredList{}
			pages++
			switch opts.Continue {
			case "":
				list.Items = []unstructured.Unstructured{*greenRun(t, "old", at.Add(-time.Minute))}
				list.SetContinue("page2")
			case "page2":
				list.Items = []unstructured.Unstructured{*drillCI(t, "new", "False", at)}
			default:
				t.Fatal("unexpected continuation", opts.Continue)
			}
			if pages > 2 {
				t.Fatal("pagination repeated")
			}
			return list, nil
		}}, nil
	}
	got, err := c.Read(context.Background(), pinnedKey(), time.Time{})
	if err != nil || got.Run != "new" || got.Status != "Failed" || pages != 2 {
		t.Fatal(got, err, pages)
	}
	for name, mutate := range map[string]func(*Key){
		"repo": func(k *Key) { k.Repo = "unknown" }, "revision": func(k *Key) { k.Revision = "main" }, "base": func(k *Key) { k.Base = "" }, "siblings": func(k *Key) { delete(k.Siblings, "bifrost-ui") }, "generation": func(k *Key) { k.Generation = -1 },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c, _ := drillService(t, greenRun(t, "ok", at))
			key := pinnedKey()
			if got, err := c.Read(context.Background(), key, time.Time{}); err != nil || got.Status != "Succeeded" {
				t.Fatal("control", got, err)
			}
			mutate(&key)
			if _, err := c.Read(context.Background(), key, time.Time{}); err == nil {
				t.Fatal("invalid key")
			}
		})
	}
}
func TestEnsurePinnedChecksEveryParameterAndLabel(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"revision", "uiRevision", "infraRevision", "repo-url", "extra", "duplicate", "pipeline", "inline", "label"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			c, d := drillService(t)
			key, name, ctx := pinnedKey(), landing.CIName("op", 1), context.Background()
			if err := c.Ensure(ctx, name, key); err != nil {
				t.Fatal("control", err)
			}
			if err := c.Ensure(ctx, name, key); err != nil {
				t.Fatal("idempotent control", err)
			}
			obj, err := d.Resource(pipelineRunGVR).Namespace("cicd").Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			params, _, _ := unstructured.NestedSlice(obj.Object, "spec", "params")
			switch change {
			case "pipeline":
				setField(t, obj, "other", "spec", "pipelineRef", "name")
			case "inline":
				setField(t, obj, map[string]any{}, "spec", "pipelineSpec")
			case "label":
				obj.SetLabels(map[string]string{})
			case "duplicate":
				setField(t, obj, append(params, params[0]), "spec", "params")
			case "extra":
				setField(t, obj, append(params, map[string]any{"name": "source", "value": "elsewhere"}), "spec", "params")
			default:
				for _, p := range params {
					if p.(map[string]any)["name"] == change {
						p.(map[string]any)["value"] = "main"
					}
				}
				setField(t, obj, params, "spec", "params")
			}
			if _, err = d.Resource(pipelineRunGVR).Namespace("cicd").Update(ctx, obj, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			if err = c.Ensure(ctx, name, key); err == nil || err.Error() != "landing CI name collision" {
				t.Fatal("adopted collision", err)
			}
		})
	}
}
func TestLegacyPlatformSpecGolden(t *testing.T) {
	t.Parallel()
	got := platformSpec("bifrost-ci-platform", map[string]string{"revision": factSHA1, "uiRevision": "main"})
	// Captures the pre-extraction spec, including pod placement and workspace.
	want := map[string]any{"pipelineRef": map[string]any{"name": "bifrost-ci-platform"}, "params": []any{map[string]any{"name": "revision", "value": factSHA1}, map[string]any{"name": "uiRevision", "value": "main"}}, "taskRunTemplate": map[string]any{"podTemplate": map[string]any{"nodeSelector": map[string]any{"kubernetes.io/arch": "amd64"}, "tolerations": []any{map[string]any{"key": "node-role.kubernetes.io/control-plane", "operator": "Exists", "effect": "NoSchedule"}}}}, "workspaces": []any{map[string]any{"name": "source", "volumeClaimTemplate": map[string]any{"spec": map[string]any{"accessModes": []any{"ReadWriteOnce"}, "storageClassName": "local-path", "resources": map[string]any{"requests": map[string]any{"storage": "2Gi"}}}}}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(fmt.Sprint(got))
	}
}

// client-go's dynamic fake discards Continue in NewListAction. Keep the real
// ListOptions at the transport boundary to test pagination without a server.
type pageList func(metav1.ListOptions) (*unstructured.UnstructuredList, error)
type pagedClient struct {
	dynamic.Interface
	list pageList
}

func (c pagedClient) Resource(gvr schema.GroupVersionResource) dynamic.NamespaceableResourceInterface {
	return pagedNamespace{NamespaceableResourceInterface: c.Interface.Resource(gvr), list: c.list}
}

type pagedNamespace struct {
	dynamic.NamespaceableResourceInterface
	list pageList
}

func (c pagedNamespace) Namespace(ns string) dynamic.ResourceInterface {
	return pagedResource{ResourceInterface: c.NamespaceableResourceInterface.Namespace(ns), list: c.list}
}

type pagedResource struct {
	dynamic.ResourceInterface
	list pageList
}

func (c pagedResource) List(_ context.Context, opts metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	return c.list(opts)
}

func TestReadFailsOnRepeatedPage(t *testing.T) {
	t.Parallel()
	c, d := drillService(t)
	repeats := false
	c.Dynamic = func() (dynamic.Interface, error) {
		return pagedClient{Interface: d, list: func(opts metav1.ListOptions) (*unstructured.UnstructuredList, error) {
			list := &unstructured.UnstructuredList{}
			list.Items = []unstructured.Unstructured{*greenRun(t, "green", time.Now().UTC())}
			if opts.Continue == "" || repeats {
				list.SetContinue("page2")
			}
			return list, nil
		}}, nil
	}
	if got, err := c.Read(context.Background(), pinnedKey(), time.Time{}); err != nil || got.Status != "Succeeded" {
		t.Fatal("control", got, err)
	}
	repeats = true
	if _, err := c.Read(context.Background(), pinnedKey(), time.Time{}); err == nil {
		t.Fatal("repeated page accepted")
	}
}
