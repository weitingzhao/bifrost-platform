package landingci

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func drillService(t *testing.T, objs ...runtime.Object) (*Client, *dynfake.FakeDynamicClient) {
	t.Helper()
	d := dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{pipelineRunGVR: "PipelineRunList"}, objs...)
	s := &Client{Dynamic: func() (dynamic.Interface, error) { return d, nil }}
	return s, d
}
func drillCI(t *testing.T, name, status string, at time.Time) *unstructured.Unstructured {
	t.Helper()
	o := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "tekton.dev/v1", "kind": "PipelineRun", "metadata": map[string]any{"name": name, "namespace": "cicd", "labels": map[string]any{"bifrost.io/repo": "bifrost-platform"}},
		"spec":   map[string]any{"params": []any{map[string]any{"name": "revision", "value": factSHA1}}},
		"status": map[string]any{"conditions": []any{map[string]any{"type": "Succeeded", "status": status}}},
	}}
	if err := unstructured.SetNestedField(o.Object, "bifrost-ci-platform", "spec", "pipelineRef", "name"); err != nil {
		t.Fatal(err)
	}
	o.SetCreationTimestamp(metav1.NewTime(at))
	return o
}

// CUT §11.7 item 3: the run's UID and its platform-commit / ui-commit
// results come with it; a malformed result is no result.
func TestLandingCIReadsTheRunsResults(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)
	ui := "c83f40b40e65e52f8592ec6b73c5f6987c116e93"
	green := drillCI(t, "green", "True", now)
	green.SetUID("uid-green")
	if err := unstructured.SetNestedSlice(green.Object, []any{
		map[string]any{"name": "platform-commit", "value": factSHA1},
		map[string]any{"name": "ui-commit", "value": ui},
		map[string]any{"name": "infra-commit", "value": "not-a-sha"},
	}, "status", "results"); err != nil {
		t.Fatal(err)
	}
	s, _ := drillService(t, green)
	ci, e := s.LandingCI(context.Background(), factSHA1, now.Add(-time.Hour))
	if e != nil || ci.UID != "uid-green" || ci.PlatformCommit != factSHA1 || ci.UICommit != ui {
		t.Fatal(ci, e)
	}
	bad := drillCI(t, "bad", "True", now)
	if err := unstructured.SetNestedSlice(bad.Object, []any{map[string]any{"name": "ui-commit", "value": "main"}}, "status", "results"); err != nil {
		t.Fatal(err)
	}
	s, _ = drillService(t, bad)
	if ci, _ = s.LandingCI(context.Background(), factSHA1, now.Add(-time.Hour)); ci.UICommit != "" || ci.PlatformCommit != "" {
		t.Fatal("a malformed result read as a commit", ci)
	}
}

func TestLandingDrillLatestRunAndReadFailures(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)
	for _, status := range []string{"False", "Unknown", ""} {
		t.Run(status, func(t *testing.T) {
			t.Parallel()
			s, _ := drillService(t, drillCI(t, "old-green", "True", now.Add(-time.Minute)), drillCI(t, "new", status, now))
			ci, e := s.LandingCI(context.Background(), factSHA1, now.Add(-time.Hour))
			if e != nil {
				t.Fatal(e)
			}
			if ci.Run != "new" || ci.Status == "Succeeded" {
				t.Fatal(ci)
			}
		})
	}
	s, d := drillService(t)
	d.PrependReactor("list", "pipelineruns", func(ktesting.Action) (bool, runtime.Object, error) { return true, nil, errors.New("offline") })
	if _, e := s.LandingCI(context.Background(), factSHA1, time.Time{}); e == nil {
		t.Fatal("read error became missing")
	}
	s, _ = drillService(t, drillCI(t, "green", "True", now))
	ci, e := s.LandingCI(context.Background(), factSHA1, now)
	if e != nil || ci.Status != "Missing" {
		t.Fatal(ci, e)
	}
}
func TestLandingDrillCreateIdempotencyAndCollision(t *testing.T) {
	t.Parallel()
	s, d := drillService(t)
	ctx := context.Background()
	name := landing.CIName("operation", 1)
	d.PrependReactor("create", "pipelineruns", func(action ktesting.Action) (bool, runtime.Object, error) {
		obj := action.(ktesting.CreateAction).GetObject()
		if err := d.Tracker().Create(pipelineRunGVR, obj, "cicd"); err != nil {
			t.Fatal(err)
		}
		return true, nil, errors.New("connection lost after create")
	})
	for range 3 {
		if e := s.EnsureLandingCI(ctx, name, factSHA1); e != nil {
			t.Fatal(e)
		}
	}
	creates := 0
	for _, a := range d.Actions() {
		if a.GetVerb() == "create" {
			creates++
		}
	}
	if creates != 1 {
		t.Fatal("duplicate run", creates)
	}
	if e := s.EnsureLandingCI(ctx, name, factSHA2); e == nil {
		t.Fatal("adopted wrong revision")
	}
	if e := s.EnsureLandingCI(ctx, "bifrost-deliver-platform", factSHA1); e == nil {
		t.Fatal("delivery name accepted")
	}
	if e := s.EnsureLandingCI(ctx, landing.CIName("operation", 2), "main"); e == nil {
		t.Fatal("floating ref accepted")
	}
}

func TestLandingDrillRerunIdentityAtSecondPrecision(t *testing.T) {
	t.Parallel()
	intent := time.Now().UTC().Truncate(time.Second).Add(100 * time.Millisecond)
	name := landing.CIName("land:task:1", 2)
	for _, marked := range []bool{false, true} {
		obj := drillCI(t, name, "True", intent.Add(100*time.Millisecond))
		if marked {
			labels := obj.GetLabels()
			labels["bifrost.io/trigger"] = "agentrt-landing-drill"
			obj.SetLabels(labels)
		}
		s, _ := drillService(t, obj)
		ci, err := s.LandingCI(context.Background(), factSHA1, intent.Add(-time.Minute))
		if err != nil || ci.Run != name || !ci.CreatedAt.Equal(intent.Truncate(time.Second)) {
			t.Fatal(ci, err)
		}
		if (ci.Status == "Succeeded") != marked {
			t.Fatal("unverified attempt identity", marked, ci)
		}
	}
}
func TestLandingDrillWindowReadOnly(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)
	for _, tc := range []struct {
		name, body    string
		blocked, fail bool
	}{
		{"expired", `{"who":"other","expires_at":"` + now.Add(-time.Minute).Format(time.RFC3339) + `"}`, false, false},
		{"live", `{"who":"other","expires_at":"` + now.Add(time.Minute).Format(time.RFC3339) + `"}`, true, false},
		{"broken", "{", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, _ := drillService(t)
			client := k8sfake.NewClientset(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: releaseWindowConfigMap, Namespace: "cicd"}, Data: map[string]string{releaseWindowDataKey: tc.body}})
			s.ConfigMap = func(ctx context.Context, name string) (map[string]string, bool, error) {
				cm, err := client.CoreV1().ConfigMaps("cicd").Get(ctx, name, metav1.GetOptions{})
				if apierrors.IsNotFound(err) {
					return nil, false, nil
				}
				if err != nil {
					return nil, false, err
				}
				return cm.Data, true, nil
			}
			blocked, e := s.windowBlocked(context.Background(), now)
			if blocked != tc.blocked || (e != nil) != tc.fail {
				t.Fatal(blocked, e)
			}
			for _, a := range client.Actions() {
				if a.GetVerb() != "get" {
					t.Fatal("window read mutated", a.GetVerb())
				}
			}
		})
	}
	s, _ := drillService(t)
	client := k8sfake.NewClientset()
	s.ConfigMap = func(ctx context.Context, name string) (map[string]string, bool, error) {
		cm, err := client.CoreV1().ConfigMaps("cicd").Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		return cm.Data, true, nil
	}
	if blocked, e := s.windowBlocked(context.Background(), now); e != nil || blocked {
		t.Fatal(blocked, e)
	}
	client.PrependReactor("get", "configmaps", func(ktesting.Action) (bool, runtime.Object, error) { return true, nil, errors.New("offline") })
	if blocked, e := s.windowBlocked(context.Background(), now); e == nil || !blocked {
		t.Fatal(blocked, e)
	}
}

const factSHA1 = "1111111111111111111111111111111111111111"
const factSHA2 = "2222222222222222222222222222222222222222"
