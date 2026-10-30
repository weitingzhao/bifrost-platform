package landingci

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	ktesting "k8s.io/client-go/testing"
)

var s65At = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

func infraKey() Key {
	return Key{Repo: "bifrost-trade-infra", Revision: factSHA1, Base: factSHA2,
		Siblings: map[string]string{"bifrost-platform": "3333333333333333333333333333333333333333"}, Generation: 1}
}
func infraRun(t *testing.T, name string, at time.Time) *unstructured.Unstructured {
	t.Helper()
	key := infraKey()
	o := drillCI(t, name, "True", at)
	o.SetUID(types.UID("uid-" + name))
	o.SetLabels(map[string]string{"bifrost.io/repo": key.Repo, "bifrost.io/trigger": "agentrt-landing-drill"})
	setField(t, o, "bifrost-ci-infra", "spec", "pipelineRef", "name")
	// Independent fixture: do not derive either side from the implementation.
	setField(t, o, []any{
		map[string]any{"name": "revision", "value": key.Revision},
		map[string]any{"name": "base", "value": key.Base},
		map[string]any{"name": "platformRevision", "value": key.Siblings["bifrost-platform"]},
		map[string]any{"name": "generation", "value": "1"},
		map[string]any{"name": "purpose", "value": "landing"},
	}, "spec", "params")
	setField(t, o, []any{
		map[string]any{"name": "infra-commit", "value": key.Revision},
		map[string]any{"name": "base-commit", "value": key.Base},
		map[string]any{"name": "platform-commit", "value": key.Siblings["bifrost-platform"]},
		map[string]any{"name": "tools-from", "value": key.Base},
		map[string]any{"name": "doc-policy", "value": "pass"},
		map[string]any{"name": "purpose", "value": "landing"},
	}, "status", "results")
	return o
}
func changeNamed(t *testing.T, o *unstructured.Unstructured, group, field, name string, value any) {
	t.Helper()
	list, _, _ := unstructured.NestedSlice(o.Object, group, field)
	for _, raw := range list {
		m := raw.(map[string]any)
		if m["name"] == name {
			m["value"] = value
		}
	}
	setField(t, o, list, group, field)
}
func readInfra(t *testing.T, objects ...runtime.Object) Evidence {
	t.Helper()
	c, _ := drillService(t, objects...)
	got, err := c.Read(context.Background(), infraKey(), s65At.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestS65T40ToT43NewestInfraEvidence(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, status, reason string
		mutate               func(*testing.T, *unstructured.Unstructured)
	}{
		{"T40 no status or results", "Unknown", "", func(t *testing.T, o *unstructured.Unstructured) { delete(o.Object, "status") }},
		{"T40 running without results", "Running", "", func(t *testing.T, o *unstructured.Unstructured) {
			setField(t, o, []any{map[string]any{"type": "Succeeded", "status": "Unknown", "reason": "Running"}}, "status", "conditions")
			unstructured.RemoveNestedField(o.Object, "status", "results")
		}},
		{"T41 failed", "Failed", "", func(t *testing.T, o *unstructured.Unstructured) {
			setField(t, o, []any{map[string]any{"type": "Succeeded", "status": "False"}}, "status", "conditions")
			unstructured.RemoveNestedField(o.Object, "status", "results")
		}},
		{"missing UID", "Unknown", "ci_identity", func(t *testing.T, o *unstructured.Unstructured) { o.SetUID("") }},
		{"remote pipeline", "Unknown", "ci_pipeline_mismatch", func(t *testing.T, o *unstructured.Unstructured) {
			setField(t, o, "git", "spec", "pipelineRef", "resolver")
		}},
	}
	for name, value := range map[string]string{"infra-commit": factSHA2, "base-commit": factSHA1, "platform-commit": factSHA1, "tools-from": factSHA1, "doc-policy": "skipped", "purpose": "push"} {
		cases = append(cases, struct {
			name, status, reason string
			mutate               func(*testing.T, *unstructured.Unstructured)
		}{
			"T42 T43 mismatch " + name, "Failed", "ci_result_mismatch", func(t *testing.T, o *unstructured.Unstructured) {
				changeNamed(t, o, "status", "results", name, value)
			},
		})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			old, latest := infraRun(t, "old", s65At.Add(-time.Minute)), infraRun(t, "new", s65At)
			if got := readInfra(t, old, latest); got.Status != "Succeeded" || got.Run != "new" || got.UID != "uid-new" || len(got.Results) != 6 || !reflect.DeepEqual(got.Results, got.CIRun.Results) {
				t.Fatal("control", got)
			}
			tc.mutate(t, latest)
			got := readInfra(t, old, latest)
			if got.Run != "new" || got.Status != tc.status || got.Reason != tc.reason {
				t.Fatal(got)
			}
		})
	}
}
func TestS65InfraMissingDuplicateAndInvalidResults(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"infra-commit", "base-commit", "platform-commit", "tools-from", "doc-policy", "purpose"} {
		for _, change := range []string{"missing", "duplicate", "empty", "non-string", "invalid"} {
			t.Run(name+"/"+change, func(t *testing.T) {
				t.Parallel()
				old, o := infraRun(t, "old", s65At.Add(-time.Minute)), infraRun(t, "new", s65At)
				if got := readInfra(t, old, o); got.Status != "Succeeded" {
					t.Fatal("control", got)
				}
				results, _, _ := unstructured.NestedSlice(o.Object, "status", "results")
				for i, raw := range results {
					m := raw.(map[string]any)
					if m["name"] != name {
						continue
					}
					switch change {
					case "missing":
						results = append(results[:i], results[i+1:]...)
					case "duplicate":
						results = append(results, m)
					case "empty":
						m["value"] = ""
					case "non-string":
						m["value"] = []any{"pass"}
					case "invalid":
						m["value"] = "not-valid"
					}
					break
				}
				setField(t, o, results, "status", "results")
				want, reason := "Unknown", "ci_result_missing"
				if change == "invalid" {
					want, reason = "Failed", "ci_result_mismatch"
				}
				if got := readInfra(t, old, o); got.Run != "new" || got.Status != want || got.Reason != reason {
					t.Fatal(got)
				}
			})
		}
	}
}
func TestS65T44T45InfraSelectionKey(t *testing.T) {
	t.Parallel()
	changes := map[string]func(*testing.T, *unstructured.Unstructured){
		"T44 push": func(t *testing.T, o *unstructured.Unstructured) {
			changeNamed(t, o, "spec", "params", "purpose", "push")
		},
		"T45 platform pipeline": func(t *testing.T, o *unstructured.Unstructured) {
			setField(t, o, "bifrost-ci-platform", "spec", "pipelineRef", "name")
		},
		"T45 inline": func(t *testing.T, o *unstructured.Unstructured) {
			setField(t, o, map[string]any{}, "spec", "pipelineSpec")
		},
	}
	for _, name := range []string{"revision", "base", "platformRevision", "generation"} {
		changes[name] = func(t *testing.T, o *unstructured.Unstructured) {
			value := "4444444444444444444444444444444444444444"
			if name == "generation" {
				value = "2"
			}
			changeNamed(t, o, "spec", "params", name, value)
		}
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			o := infraRun(t, "new", s65At)
			if got := readInfra(t, o); got.Status != "Succeeded" {
				t.Fatal("control", got)
			}
			change(t, o)
			if got := readInfra(t, o); got.Status != "Missing" || got.Run != "" {
				t.Fatal(got)
			}
		})
	}
	// Labels cannot turn the wrong parameter key into the requested key, or
	// hide a newer matching run from ordering.
	o := infraRun(t, "new", s65At)
	o.SetLabels(map[string]string{"bifrost.io/repo": "bifrost-platform", "bifrost.io/revision": factSHA2})
	if got := readInfra(t, o); got.Status != "Succeeded" {
		t.Fatal("labels selected the run", got)
	}
}
func TestS65InfraPaginationAndTies(t *testing.T) {
	t.Parallel()
	for _, status := range []string{"True", "False", "Unknown"} {
		t.Run(status, func(t *testing.T) {
			t.Parallel()
			c, d := drillService(t)
			latest := infraRun(t, "z", s65At)
			setField(t, latest, []any{map[string]any{"type": "Succeeded", "status": status}}, "status", "conditions")
			pages := 0
			c.Dynamic = func() (dynamic.Interface, error) {
				return pagedClient{Interface: d, list: func(opts metav1.ListOptions) (*unstructured.UnstructuredList, error) {
					pages++
					if opts.Limit != 500 || opts.LabelSelector != "" {
						t.Fatal(opts)
					}
					list := &unstructured.UnstructuredList{}
					switch opts.Continue {
					case "":
						list.Items = []unstructured.Unstructured{*infraRun(t, "a", s65At)}
						list.SetContinue("next")
					case "next":
						list.Items = []unstructured.Unstructured{*latest}
					default:
						t.Fatal(opts)
					}
					return list, nil
				}}, nil
			}
			got, err := c.Read(context.Background(), infraKey(), s65At.Add(-time.Hour))
			want := "Unknown"
			if status == "True" {
				want = "Succeeded"
			}
			if err != nil || got.Run != "z" || got.Status != want || pages != 2 {
				t.Fatal(got, err, pages)
			}
		})
	}
	name := landing.InfraCIName(factSHA1, factSHA2, infraKey().Siblings["bifrost-platform"], 1)
	run := infraRun(t, name, s65At)
	if got := readInfra(t, run); got.Status != "Succeeded" {
		t.Fatal("control", got)
	}
	labels := run.GetLabels()
	delete(labels, "bifrost.io/trigger")
	run.SetLabels(labels)
	if got := readInfra(t, infraRun(t, "old", s65At.Add(-time.Minute)), run); got.Status != "Unknown" || got.Run != name {
		t.Fatal(got)
	}
}
func TestS65T46EnsureInfraCollision(t *testing.T) {
	t.Parallel()
	changes := []string{"base", "revision", "platformRevision", "generation", "purpose", "pipeline", "inline", "extra", "duplicate"}
	for _, label := range []string{"tekton.dev/pipeline", "bifrost.io/repo", "bifrost.io/trigger", "bifrost.io/revision", "bifrost.io/base", "bifrost.io/purpose"} {
		changes = append(changes, "label:"+label)
	}
	for _, change := range changes {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			c, d := drillService(t)
			key := infraKey()
			ctx := context.Background()
			name := landing.InfraCIName(key.Revision, key.Base, key.Siblings["bifrost-platform"], key.Generation)
			for range 2 {
				if err := c.Ensure(ctx, name, key); err != nil {
					t.Fatal("control", err)
				}
			}
			res := d.Resource(pipelineRunGVR).Namespace("cicd")
			o, err := res.Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			values, counts := namedValues(*o, "spec", "params")
			if len(values) != 5 || len(counts) != 5 || values["base"] != factSHA2 || values["purpose"] != "landing" || values["generation"] != "1" {
				t.Fatal(values, counts)
			}
			switch change {
			case "pipeline":
				setField(t, o, "bifrost-ci-platform", "spec", "pipelineRef", "name")
			case "inline":
				setField(t, o, map[string]any{}, "spec", "pipelineSpec")
			case "extra", "duplicate":
				params, _, _ := unstructured.NestedSlice(o.Object, "spec", "params")
				extra := params[0]
				if change == "extra" {
					extra = map[string]any{"name": "extra", "value": "x"}
				}
				setField(t, o, append(params, extra), "spec", "params")
			default:
				if len(change) > 6 && change[:6] == "label:" {
					labels := o.GetLabels()
					labels[change[6:]] = "different"
					o.SetLabels(labels)
				} else {
					value := "4444444444444444444444444444444444444444"
					if change == "generation" {
						value = "2"
					}
					if change == "purpose" {
						value = "push"
					}
					changeNamed(t, o, "spec", "params", change, value)
				}
			}
			if _, err = res.Update(ctx, o, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			if err = c.Ensure(ctx, name, key); err == nil || err.Error() != "landing CI name collision" {
				t.Fatal("adopted collision", err)
			}
			creates := 0
			for _, a := range d.Actions() {
				if a.GetVerb() == "create" {
					creates++
				}
			}
			if creates != 1 {
				t.Fatal("collision caused write", creates)
			}
		})
	}
}
func TestS65InfraCreateRecovery(t *testing.T) {
	t.Parallel()
	for _, result := range []string{"lost response", "already exists", "failed before create"} {
		t.Run(result, func(t *testing.T) {
			t.Parallel()
			c, d := drillService(t)
			key := infraKey()
			ctx := context.Background()
			name := landing.InfraCIName(key.Revision, key.Base, key.Siblings["bifrost-platform"], key.Generation)
			creates := 0
			d.PrependReactor("create", "pipelineruns", func(a ktesting.Action) (bool, runtime.Object, error) {
				creates++
				if result != "failed before create" {
					if err := d.Tracker().Create(pipelineRunGVR, a.(ktesting.CreateAction).GetObject(), "cicd"); err != nil {
						t.Fatal(err)
					}
				}
				if result == "already exists" {
					return true, nil, apierrors.NewAlreadyExists(pipelineRunGVR.GroupResource(), name)
				}
				return true, nil, errors.New("connection lost")
			})
			err := c.Ensure(ctx, name, key)
			if result == "failed before create" {
				if err == nil {
					t.Fatal("lost creation acknowledged")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = c.Ensure(ctx, name, key); err != nil || creates != 1 {
				t.Fatal(err, creates)
			}
		})
	}
}
func TestS65InfraIdentity(t *testing.T) {
	t.Parallel()
	key := infraKey()
	// Calculated independently from S6 §6.1's literal, checked in as a golden.
	want := "landing-ci-infra-149dac20fd0e144385b3896a-1"
	name := landing.InfraCIName(key.Revision, key.Base, key.Siblings["bifrost-platform"], key.Generation)
	if name != want {
		t.Fatal(name)
	}
	for _, change := range []string{"repo", "revision", "base", "sibling", "generation", "name"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			key := infraKey()
			c, d := drillService(t)
			if err := c.Ensure(context.Background(), name, key); err != nil {
				t.Fatal("control", err)
			}
			candidateName := name
			switch change {
			case "repo":
				key.Repo = "unknown"
			case "revision":
				key.Revision = "main"
			case "base":
				key.Base = "main"
			case "sibling":
				key.Siblings["bifrost-platform"] = "main"
			case "generation":
				key.Generation = 0
			case "name":
				candidateName = fmt.Sprint(name, "-extra")
			}
			before := len(d.Actions())
			if err := c.Ensure(context.Background(), candidateName, key); err == nil {
				t.Fatal("invalid identity accepted")
			}
			if len(d.Actions()) != before {
				t.Fatal("invalid identity contacted K8s")
			}
		})
	}
}
