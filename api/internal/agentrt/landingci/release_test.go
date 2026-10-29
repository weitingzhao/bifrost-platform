package landingci

import (
	"context"
	"errors"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	kt "k8s.io/client-go/testing"
)

func releaseCI(t *testing.T, name string, at time.Time) *unstructured.Unstructured {
	t.Helper()
	o := greenRun(t, name, at)
	params, _, _ := unstructured.NestedSlice(o.Object, "spec", "params")
	setField(t, o, append(params, map[string]any{"name": "repo-url", "value": PlatformRepoURL}), "spec", "params")
	return o
}
func TestReleaseCIRejectsLatestWithSuccessfulControl(t *testing.T) {
	t.Parallel()
	at := time.Now().UTC().Add(-time.Hour)
	cases := map[string]struct {
		status string
		mutate func(*testing.T, *unstructured.Unstructured)
	}{
		"failed": {"Failed", func(t *testing.T, o *unstructured.Unstructured) {
			setField(t, o, []any{map[string]any{"type": "Succeeded", "status": "False"}}, "status", "conditions")
		}},
		"running": {"Running", func(t *testing.T, o *unstructured.Unstructured) {
			setField(t, o, []any{map[string]any{"type": "Succeeded", "status": "Unknown", "reason": "Running"}}, "status", "conditions")
		}},
		"missing result": {"Unknown", func(t *testing.T, o *unstructured.Unstructured) {
			unstructured.RemoveNestedField(o.Object, "status", "results")
		}},
		"duplicate result": {"Unknown", func(t *testing.T, o *unstructured.Unstructured) {
			r, _, _ := unstructured.NestedSlice(o.Object, "status", "results")
			setField(t, o, append(r, r[0]), "status", "results")
		}},
		"illegal sha": {"Unknown", func(t *testing.T, o *unstructured.Unstructured) {
			r, _, _ := unstructured.NestedSlice(o.Object, "status", "results")
			r[1].(map[string]any)["value"] = "main"
			setField(t, o, r, "status", "results")
		}},
		"wrong P": {"Unknown", func(t *testing.T, o *unstructured.Unstructured) {
			r, _, _ := unstructured.NestedSlice(o.Object, "status", "results")
			r[0].(map[string]any)["value"] = factSHA2
			setField(t, o, r, "status", "results")
		}},
		"no uid": {"Unknown", func(t *testing.T, o *unstructured.Unstructured) { o.SetUID("") }},
		"missing repo": {"Unknown", func(t *testing.T, o *unstructured.Unstructured) {
			setField(t, o, []any{map[string]any{"name": "revision", "value": factSHA1}}, "spec", "params")
		}},
		"resolver": {"Unknown", func(t *testing.T, o *unstructured.Unstructured) {
			setField(t, o, "git", "spec", "pipelineRef", "resolver")
		}},
		"inline": {"Unknown", func(t *testing.T, o *unstructured.Unstructured) {
			setField(t, o, map[string]any{}, "spec", "pipelineSpec")
		}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			old, newest := releaseCI(t, "old", at.Add(-time.Minute)), releaseCI(t, "new", at)
			read := func() ReleaseEvidence {
				t.Helper()
				c, _ := drillService(t, old, newest)
				got, err := c.ReadRelease(context.Background(), factSHA1)
				if err != nil {
					t.Fatal(err)
				}
				return got
			}
			if got := read(); got.Status != "Succeeded" || got.Run != "new" || got.UICommit != factSHA2 || got.InfraCommit != factSHA2 {
				t.Fatal("control", got)
			}
			tc.mutate(t, newest)
			if got := read(); got.Status != tc.status || got.Run != "new" {
				t.Fatal("refusal", got)
			}
		})
	}
}
func TestReleaseCITiesAndRepositoryFilter(t *testing.T) {
	t.Parallel()
	at := time.Now().UTC().Add(-time.Hour)
	a, b := releaseCI(t, "a", at), releaseCI(t, "b", at)
	read := func() ReleaseEvidence {
		t.Helper()
		c, _ := drillService(t, b, a)
		got, err := c.ReadRelease(context.Background(), factSHA1)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := read(); got.Run != "a" || got.Status != "Succeeded" {
		t.Fatal(got)
	}
	r, _, _ := unstructured.NestedSlice(b.Object, "status", "results")
	r[1].(map[string]any)["value"] = factSHA1
	setField(t, b, r, "status", "results")
	if got := read(); got.Status != "Unknown" || got.Reason != "ci_tie" {
		t.Fatal(got)
	}
	// An unrelated repository cannot poison this repository's CI choice.
	p, _, _ := unstructured.NestedSlice(b.Object, "spec", "params")
	for _, v := range p {
		m := v.(map[string]any)
		if m["name"] == "repo-url" {
			m["value"] = "https://other/repo.git"
		}
	}
	setField(t, b, p, "spec", "params")
	if got := read(); got.Status != "Succeeded" || got.Run != "a" {
		t.Fatal(got)
	}
}
func TestReleaseCIPaginationReadFailureAndRepeatedPage(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"newest", "read error", "repeated"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			at := time.Now().UTC().Add(-time.Hour)
			old, newest := releaseCI(t, "old", at.Add(-time.Minute)), releaseCI(t, "new", at)
			c, dyn := drillService(t)
			page := 0
			dyn.PrependReactor("list", "pipelineruns", func(action kt.Action) (bool, runtime.Object, error) {
				page++
				if page == 1 {
					return true, &unstructured.UnstructuredList{Object: map[string]any{"metadata": map[string]any{"continue": "next"}}, Items: []unstructured.Unstructured{*old}}, nil
				}
				if mode == "read error" {
					return true, nil, errors.New("unreadable")
				}
				if mode == "repeated" {
					return true, &unstructured.UnstructuredList{Object: map[string]any{"metadata": map[string]any{"continue": "next"}}}, nil
				}
				return true, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{*newest}}, nil
			})
			got, err := c.ReadRelease(context.Background(), factSHA1)
			if mode == "newest" {
				if err != nil || got.Run != "new" || got.Status != "Succeeded" {
					t.Fatal(got, err)
				}
			} else if err == nil {
				t.Fatal("accepted unread list", got)
			}
		})
	}
	c, _ := drillService(t)
	if got, err := c.ReadRelease(context.Background(), factSHA1); err != nil || got.Status != "Missing" {
		t.Fatal(got, err)
	}
	if _, err := c.ReadRelease(context.Background(), "main"); err == nil {
		t.Fatal("bad revision")
	}
	// Same entry after removing an old CI still accepts its surviving successor.
	at := metav1.Now().Add(-time.Hour)
	c, _ = drillService(t, releaseCI(t, "successor", at))
	if got, err := c.ReadRelease(context.Background(), factSHA1); err != nil || got.Status != "Succeeded" {
		t.Fatal(got, err)
	}
}

func TestReleaseCIDuplicateRevisionCannotReviveOlderGreen(t *testing.T) {
	t.Parallel()
	at := time.Now().UTC().Add(-time.Hour)
	old, newest := releaseCI(t, "old", at.Add(-time.Minute)), releaseCI(t, "new", at)
	c, _ := drillService(t, old, newest)
	if got, err := c.ReadRelease(context.Background(), factSHA1); err != nil || got.Status != "Succeeded" || got.Run != "new" {
		t.Fatal("control", got, err)
	}
	params, _, _ := unstructured.NestedSlice(newest.Object, "spec", "params")
	setField(t, newest, append(params, map[string]any{"name": "revision", "value": factSHA2}), "spec", "params")
	c, _ = drillService(t, old, newest)
	if got, err := c.ReadRelease(context.Background(), factSHA1); err != nil || got.Status != "Unknown" || got.Run != "new" {
		t.Fatal(got, err)
	}
}
