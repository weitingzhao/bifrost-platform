package delivery

import (
	"context"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestPipelineRunObservationsAllPagesWithoutLabels(t *testing.T) {
	t.Parallel()
	dyn := dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{pipelineRunGVR: "PipelineRunList"})
	calls := 0
	dyn.PrependReactor("list", "pipelineruns", func(action k8stesting.Action) (bool, runtime.Object, error) {
		a := action.(k8stesting.ListAction)
		if a.GetNamespace() != "cicd" || !a.GetListRestrictions().Labels.Empty() {
			t.Fatalf("filtered list: %+v", action)
		}
		page := &unstructured.UnstructuredList{}
		name := "first"
		if calls == 0 {
			page.SetContinue("next")
		} else {
			name = "second"
		}
		page.Items = []unstructured.Unstructured{{Object: map[string]any{"metadata": map[string]any{"name": name}}}}
		calls++
		return true, page, nil
	})
	s := NewService(&config.ClusterEntry{ID: "test"})
	s.SetDynamicFactoryForTest(func() (dynamic.Interface, error) { return dyn, nil })
	items, err := s.PipelineRunObservations(context.Background())
	if err != nil || calls != 2 || len(items) != 2 || items[1].GetName() != "second" {
		t.Fatalf("items: %+v calls=%d err=%v", items, calls, err)
	}
}
