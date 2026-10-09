package workactions

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuationpolicy"
)

func TestPlanSyncsBeforeTheRun(t *testing.T) {
	var order []string
	dyn := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	svc := &Service{
		Policy: &actuationpolicy.Policy{
			Delivery: actuationpolicy.Delivery{Namespace: "cicd", Pipeline: "apply-pipe", ServiceAccount: "sa"},
			Apply: actuationpolicy.Apply{
				Repos:            []actuationpolicy.Repo{{Name: "example-repo", Prefixes: []string{"k8s"}}},
				DaemonDeployment: "daemon",
			},
		},
		Sync: func(context.Context, string, string) error {
			order = append(order, "sync")
			return nil
		},
		Clients: Clients{Dynamic: func() (dynamic.Interface, error) {
			order = append(order, "client")
			return dyn, nil
		}},
	}
	// dynamic.Interface is the interface; the fake client implements it.
	_ = dyn
	name, err := svc.Plan(context.Background(), "example-repo", "k8s/app.yaml", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	if name == "" {
		t.Fatal("empty plan name")
	}
	if len(order) < 2 || order[0] != "sync" {
		t.Fatalf("order = %v, want sync before the run client", order)
	}
}

func TestPlanDoesNotStartWhenSyncFails(t *testing.T) {
	started := false
	svc := &Service{
		Policy: &actuationpolicy.Policy{
			Delivery: actuationpolicy.Delivery{Namespace: "cicd", Pipeline: "apply-pipe"},
			Apply: actuationpolicy.Apply{
				Repos:            []actuationpolicy.Repo{{Name: "example-repo", Prefixes: []string{"k8s"}}},
				DaemonDeployment: "daemon",
			},
		},
		Sync: func(context.Context, string, string) error {
			return errSync
		},
		Clients: Clients{Dynamic: func() (dynamic.Interface, error) {
			started = true
			return nil, nil
		}},
	}
	if _, err := svc.Plan(context.Background(), "example-repo", "k8s/app.yaml", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err == nil {
		t.Fatal("plan succeeded without a mirror")
	}
	if started {
		t.Fatal("the run started after sync failed")
	}
}

type syncError string

func (e syncError) Error() string { return string(e) }

const errSync syncError = "mirror unavailable"
