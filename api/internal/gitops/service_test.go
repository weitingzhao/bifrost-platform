package gitops

import (
	"fmt"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

func newTestService(
	entry *config.ClusterEntry,
	clientset kubernetes.Interface,
	dyn dynamic.Interface,
) *Service {
	svc := NewService(entry)
	cs := cluster.NewService(entry)
	cs.SetClientFactoryForTest(func() (kubernetes.Interface, string, error) {
		return clientset, "fake", nil
	})
	svc.cluster = cs
	if dyn != nil {
		svc.dynamicFactory = func() (dynamic.Interface, error) {
			return dyn, nil
		}
	}
	return svc
}

func TestAppsArgoCDNotInstalled(t *testing.T) {
	clientset := k8sfake.NewSimpleClientset()
	entry := &config.ClusterEntry{
		ID: "test",
		GitOps: config.GitOpsConfig{
			ArgoCDNamespace:       "cicd",
			ApplicationsNamespace: "cicd",
		},
	}
	svc := newTestService(entry, clientset, nil)

	resp := svc.Apps(t.Context())
	if resp.ArgoCDStatus != "not_installed" {
		t.Fatalf("argocd_status: got %q want not_installed", resp.ArgoCDStatus)
	}
	if resp.Reachability != probe.ReachDegraded {
		t.Fatalf("reachability: got %q want degraded", resp.Reachability)
	}
}

func TestAppsArgoCDServerWithoutApplications(t *testing.T) {
	replicas := int32(1)
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "cicd"}}
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "argocd-server", Namespace: "cicd"},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas:     1,
			AvailableReplicas: 1,
		},
	}
	clientset := k8sfake.NewSimpleClientset(ns, deploy)
	entry := &config.ClusterEntry{
		ID: "test",
		GitOps: config.GitOpsConfig{
			ArgoCDNamespace:       "cicd",
			ApplicationsNamespace: "cicd",
		},
	}
	svc := newTestService(entry, clientset, nil)
	svc.dynamicFactory = func() (dynamic.Interface, error) {
		return nil, fmt.Errorf("the server could not find the requested resource (applications.argoproj.io)")
	}

	resp := svc.Apps(t.Context())
	if resp.ArgoCDStatus != "installed" {
		t.Fatalf("argocd_status: got %q want installed", resp.ArgoCDStatus)
	}
	if resp.Reachability != probe.ReachDegraded {
		t.Fatalf("reachability: got %q want degraded (CRD missing)", resp.Reachability)
	}
	if resp.Server == nil || resp.Server.Name != "argocd-server" {
		t.Fatalf("server: %+v", resp.Server)
	}
}

func TestAppsListsApplications(t *testing.T) {
	replicas := int32(1)
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "cicd"}}
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "argocd-server", Namespace: "cicd"},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas:     1,
			AvailableReplicas: 1,
		},
	}
	clientset := k8sfake.NewSimpleClientset(ns, deploy)

	app := &unstructured.Unstructured{}
	app.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "argoproj.io",
		Version: "v1alpha1",
		Kind:    "Application",
	})
	app.SetName("bifrost-stg")
	app.SetNamespace("cicd")
	_ = unstructured.SetNestedField(app.Object, "default", "spec", "project")
	_ = unstructured.SetNestedField(app.Object, "bifrost-stg", "spec", "destination", "namespace")
	_ = unstructured.SetNestedField(app.Object, "Synced", "status", "sync", "status")
	_ = unstructured.SetNestedField(app.Object, "Healthy", "status", "health", "status")
	_ = unstructured.SetNestedField(app.Object, "abc123", "status", "sync", "revision")
	_ = unstructured.SetNestedField(app.Object, "https://github.com/example/repo.git", "spec", "source", "repoURL")
	_ = unstructured.SetNestedField(app.Object, "k8s/overlays/stg", "spec", "source", "path")
	_ = unstructured.SetNestedField(app.Object, "main", "spec", "source", "targetRevision")
	_ = unstructured.SetNestedField(app.Object, map[string]any{"prune": true, "selfHeal": true}, "spec", "syncPolicy", "automated")
	_ = unstructured.SetNestedSlice(app.Object, []any{
		map[string]any{"id": int64(1), "revision": "rev-old"},
		map[string]any{"id": int64(2), "revision": "abc123"},
	}, "status", "history")

	scheme := runtime.NewScheme()
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{
		applicationGVR: "ApplicationList",
	}, app)

	entry := &config.ClusterEntry{
		ID: "test",
		GitOps: config.GitOpsConfig{
			ArgoCDNamespace:       "cicd",
			ApplicationsNamespace: "cicd",
		},
	}
	svc := newTestService(entry, clientset, dyn)

	resp := svc.Apps(t.Context())
	if resp.ArgoCDStatus != "installed" {
		t.Fatalf("argocd_status: got %q want installed", resp.ArgoCDStatus)
	}
	if resp.Reachability != probe.ReachOK {
		t.Fatalf("reachability: got %q want ok", resp.Reachability)
	}
	if len(resp.Apps) != 1 {
		t.Fatalf("apps: got %d want 1", len(resp.Apps))
	}
	if resp.Apps[0].Name != "bifrost-stg" || resp.Apps[0].SyncStatus != "Synced" {
		t.Fatalf("app view: %+v", resp.Apps[0])
	}
	if resp.Apps[0].SourcePath != "k8s/overlays/stg" || !resp.Apps[0].AutomatedSync {
		t.Fatalf("app spec fields: %+v", resp.Apps[0])
	}
	if resp.Apps[0].HistoryCount != 2 {
		t.Fatalf("history_count: got %d want 2", resp.Apps[0].HistoryCount)
	}
}

func TestApplicationFromUnstructured(t *testing.T) {
	app := &unstructured.Unstructured{}
	app.SetName("demo")
	app.SetNamespace("cicd")
	_ = unstructured.SetNestedField(app.Object, "prod", "spec", "project")
	_ = unstructured.SetNestedField(app.Object, "bifrost", "spec", "destination", "namespace")
	_ = unstructured.SetNestedField(app.Object, "OutOfSync", "status", "sync", "status")
	_ = unstructured.SetNestedField(app.Object, "Degraded", "status", "health", "status")
	_ = unstructured.SetNestedSlice(app.Object, []any{
		map[string]any{
			"type":    "ComparisonError",
			"message": "failed to list tlsoptions.traefik.io",
		},
	}, "status", "conditions")

	view := applicationFromUnstructured(*app)
	if view.Project != "prod" || view.Destination != "bifrost" {
		t.Fatalf("view: %+v", view)
	}
	if view.SyncStatus != "OutOfSync" || view.HealthStatus != "Degraded" {
		t.Fatalf("status: %+v", view)
	}
	if len(view.Conditions) != 1 || view.PrimaryCondition == "" {
		t.Fatalf("conditions: %+v primary=%q", view.Conditions, view.PrimaryCondition)
	}
}

func testApplicationWithHistory(revisions ...string) *unstructured.Unstructured {
	app := &unstructured.Unstructured{}
	app.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "argoproj.io",
		Version: "v1alpha1",
		Kind:    "Application",
	})
	app.SetName("bifrost-stg")
	app.SetNamespace("cicd")
	history := make([]any, 0, len(revisions))
	for i, rev := range revisions {
		history = append(history, map[string]any{
			"id":       int64(i + 1),
			"revision": rev,
		})
	}
	_ = unstructured.SetNestedSlice(app.Object, history, "status", "history")
	return app
}

func TestPreviousHistoryRevision(t *testing.T) {
	app := testApplicationWithHistory("rev-old", "rev-current")
	rev, err := previousHistoryRevision(app)
	if err != nil {
		t.Fatalf("previousHistoryRevision: %v", err)
	}
	if rev != "rev-old" {
		t.Fatalf("revision: got %q want rev-old", rev)
	}
}

func TestPreviousHistoryRevisionInsufficient(t *testing.T) {
	app := testApplicationWithHistory("rev-only")
	if _, err := previousHistoryRevision(app); err == nil {
		t.Fatal("expected error for single history entry")
	}
}

func TestRollbackApplicationUsesPreviousHistory(t *testing.T) {
	app := testApplicationWithHistory("rev-old", "rev-current")
	scheme := runtime.NewScheme()
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{
		applicationGVR: "ApplicationList",
	}, app)

	entry := &config.ClusterEntry{
		ID: "test",
		GitOps: config.GitOpsConfig{
			ArgoCDNamespace:       "cicd",
			ApplicationsNamespace: "cicd",
		},
	}
	svc := newTestService(entry, k8sfake.NewSimpleClientset(), dyn)

	resp, err := svc.RollbackApplication(t.Context(), "bifrost-stg", "")
	if err != nil {
		t.Fatalf("RollbackApplication: %v", err)
	}
	if !resp.OK || resp.Action != "gitops.rollback" {
		t.Fatalf("resp: %+v", resp)
	}
	if !strings.Contains(resp.Message, "rev-old") {
		t.Fatalf("message: %q", resp.Message)
	}
}

func TestRollbackApplicationExplicitRevision(t *testing.T) {
	app := testApplicationWithHistory("rev-old", "rev-current")
	scheme := runtime.NewScheme()
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{
		applicationGVR: "ApplicationList",
	}, app)

	entry := &config.ClusterEntry{
		ID: "test",
		GitOps: config.GitOpsConfig{
			ApplicationsNamespace: "cicd",
		},
	}
	svc := newTestService(entry, k8sfake.NewSimpleClientset(), dyn)

	resp, err := svc.RollbackApplication(t.Context(), "bifrost-stg", "rev-explicit")
	if err != nil {
		t.Fatalf("RollbackApplication: %v", err)
	}
	if !strings.Contains(resp.Message, "rev-explicit") {
		t.Fatalf("message: %q", resp.Message)
	}
}
