package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landingci"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynfake "k8s.io/client-go/dynamic/fake"
)

func TestS65InfraPreparedWiring(t *testing.T) {
	t.Parallel()
	gvr := schema.GroupVersionResource{Group: "tekton.dev", Version: "v1", Resource: "pipelineruns"}
	fake := dynfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{gvr: "PipelineRunList"})
	ci := &landingci.Client{Dynamic: func() (dynamic.Interface, error) { return fake, nil }}
	s := &Server{delivery: delivery.NewHandlerForTest(&delivery.Service{}, nil)}
	c := &controller.Controller{Drill: &controller.DrillSources{}, DrillGit: map[string]controller.DrillGit{"bifrost-trade-infra": &landinggit.Git{}}}
	s.wireInfraLandingCI(c, ci)
	o := landing.Operation{R: strings.Repeat("a", 40), M0: strings.Repeat("b", 40), PlatformRevision: strings.Repeat("c", 40), Generation: 2, PreparedAt: time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)}
	ctx := context.Background()
	if err := c.Drill.EnsureInfraCI(ctx, o); err != nil {
		t.Fatal(err)
	}
	name := landing.InfraCIName(o.R, o.M0, o.PlatformRevision, 2)
	run, err := fake.Resource(gvr).Namespace("cicd").Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	run.SetUID("selected-uid")
	run.SetCreationTimestamp(metav1.NewTime(o.PreparedAt.Add(time.Minute)))
	results := []any{}
	for name, value := range map[string]string{"infra-commit": o.R, "base-commit": o.M0, "platform-commit": o.PlatformRevision, "tools-from": o.M0, "doc-policy": "pass", "purpose": "landing"} {
		results = append(results, map[string]any{"name": name, "value": value})
	}
	if err = unstructured.SetNestedSlice(run.Object, results, "status", "results"); err != nil {
		t.Fatal(err)
	}
	if err = unstructured.SetNestedSlice(run.Object, []any{map[string]any{"type": "Succeeded", "status": "True"}}, "status", "conditions"); err != nil {
		t.Fatal(err)
	}
	if _, err = fake.Resource(gvr).Namespace("cicd").Update(ctx, run, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	got, err := c.Drill.InfraCI(ctx, o)
	if err != nil || got.Status != "Succeeded" || got.UID != "selected-uid" || len(got.Results) != 6 {
		t.Fatal(got, err)
	}
	o.PlatformRevision = strings.Repeat("d", 40)
	got, err = c.Drill.InfraCI(ctx, o)
	if err != nil || got.Status != "Missing" {
		t.Fatal("other preparation reused green", got, err)
	}
	o.PlatformRevision = ""
	calls := len(fake.Actions())
	if _, err = c.Drill.InfraCI(ctx, o); err == nil {
		t.Fatal("historical unpinned operation read CI")
	}
	if len(fake.Actions()) != calls {
		t.Fatal("invalid key queried Kubernetes")
	}
}

// Serial: process environment controls worker assembly. Both cases configure
// infra live; only adding the signed repo rule enables its CI sources.
func TestS65UnsignedInfraHasNoCISources(t *testing.T) {
	t.Setenv("AGENTRT_LANDING_LIVE_REPOS", "bifrost-platform,bifrost-trade-infra")
	t.Setenv("AGENTRT_LANDER_USERNAME_FILE", "/not-read/username")
	t.Setenv("AGENTRT_LANDER_PASSWORD_FILE", "/not-read/password")
	s := &Server{delivery: delivery.NewHandlerForTest(&delivery.Service{}, nil), cluster: cluster.NewHandler(&config.Config{}, nil)}
	for _, signed := range []bool{true, false} {
		c := &controller.Controller{Env: "prod", DB: &startup704DB{signed: signed}, LandingCards: completeCardQuestions()}
		s.wireLandingDrill(c)
		if c.Drill == nil || c.Drill.CI == nil || c.Drill.EnsureCI == nil {
			t.Fatal("platform sources lost")
		}
		if (c.Drill.InfraCI != nil) != signed || (c.Drill.EnsureInfraCI != nil) != signed || (c.Drill.InfraPlatformHead != nil) != signed {
			t.Fatal("unsigned infra sources constructed", signed)
		}
	}
}
