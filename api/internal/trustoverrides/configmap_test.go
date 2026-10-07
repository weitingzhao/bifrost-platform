package trustoverrides

import (
	"context"
	"errors"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentgovernance"
)

func TestTrustOverrideConfigMapRoundTrip(t *testing.T) {
	core := k8sfake.NewSimpleClientset()
	s := NewConfigMapStore("bifrost-platform-test", func() (kubernetes.Interface, error) { return core, nil })
	ctx := context.Background()
	if got, err := s.List(ctx); err != nil || len(got) != 0 {
		t.Fatalf("empty store: %v, %v", got, err)
	}
	if err := s.Put(ctx, agentgovernance.TrustOverride{SkillID: "research-loop-batch", Level: "L0"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(ctx, agentgovernance.TrustOverride{SkillID: "release", Level: "L2"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got["research-loop-batch"].Level != "L0" || got["release"].Level != "L2" || got["release"].AppliedAt.IsZero() {
		t.Fatalf("round trip = %+v", got)
	}
}

func TestTrustOverrideConfigMapWriteFailAndReadFail(t *testing.T) {
	core := k8sfake.NewSimpleClientset()
	forbidden := apierrors.NewForbidden(schema.GroupResource{Resource: "configmaps"}, configMapName, errors.New("rbac"))
	core.PrependReactor("create", "configmaps", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, forbidden
	})
	s := NewConfigMapStore("bifrost-platform-test", func() (kubernetes.Interface, error) { return core, nil })
	if err := s.Put(context.Background(), agentgovernance.TrustOverride{SkillID: "x", Level: "L0"}); err == nil {
		t.Fatal("Put with a forbidden create returned no error")
	}
	core.PrependReactor("get", "configmaps", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, forbidden
	})
	if _, err := s.List(context.Background()); err == nil {
		t.Fatal("List with a forbidden get returned no error")
	}
}
