package delivery

import (
	"context"
	"fmt"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/releases"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var stgApplicationGVR = schema.GroupVersionResource{Group: "argoproj.io", Version: "v1alpha1", Resource: "applications"}
var stgDeployments = []string{"platform-api", "platform-workers", "platform-console"}

// ReadWorkloads preserves every PodDigests condition and adds the template
// digest plus a bracket around all three reads. No bool collapses read errors
// or unsettled deployments into a trusted baseline. The returned JSON objects
// can enter B5-a's canonical encoder directly, without a caller conversion.
func (a *STGReleaseAdapter) ReadWorkloads(ctx context.Context, apiDigest, consoleDigest string) (map[string]any, error) {
	if !stgDigest.MatchString(apiDigest) || !stgDigest.MatchString(consoleDigest) {
		return nil, ErrSTGEvidence
	}
	ctx, cancel := stgContext(ctx)
	defer cancel()
	core, err := a.core()
	if err != nil {
		return nil, err
	}
	deps := core.AppsV1().Deployments(stgNamespace)
	before := map[string]*appsv1.Deployment{}
	for _, name := range stgDeployments {
		dep, e := deps.Get(ctx, name, metav1.GetOptions{})
		if e != nil {
			return nil, e
		}
		if dep.UID == "" {
			return nil, ErrSTGEvidence
		}
		before[name] = dep
	}
	out := map[string]any{}
	for _, name := range stgDeployments {
		expected := apiDigest
		container := "platform-api"
		if name == "platform-console" {
			expected = consoleDigest
			container = "platform-console"
		}
		dep := before[name]
		containers := dep.Spec.Template.Spec.Containers
		if len(containers) != 1 || containers[0].Name != container || imageDigest(containers[0].Image) != expected {
			return nil, fmt.Errorf("%w: %s template digest", ErrSTGEvidence, name)
		}
		digests, e := releases.ReadPodDigests(ctx, core, stgNamespace, name, container)
		if e != nil {
			return nil, e
		}
		if len(digests) == 0 {
			return nil, ErrSTGEvidence
		}
		for _, digest := range digests {
			if digest != expected {
				return nil, fmt.Errorf("%w: %s pod digest", ErrSTGEvidence, name)
			}
		}
		out[name] = map[string]any{"uid": string(dep.UID), "generation": dep.Generation, "digest": expected}
	}
	for _, name := range stgDeployments {
		dep, e := deps.Get(ctx, name, metav1.GetOptions{})
		if e != nil {
			return nil, e
		}
		if dep.UID != before[name].UID || dep.Generation != before[name].Generation {
			return nil, fmt.Errorf("%w: deployment changed during read", ErrSTGUnknown)
		}
	}
	return out, nil
}

func imageDigest(image string) string {
	i := strings.LastIndex(image, "@")
	if i < 1 || !stgDigest.MatchString(image[i+1:]) {
		return ""
	}
	return image[i+1:]
}

// ReadPin checks the current Argo desired pin. The caller must also retain
// Content's successful gitops-sync TaskRun; a matching image alone cannot
// prove that a particular run deployed it.
func (a *STGReleaseAdapter) ReadPin(ctx context.Context, apiDigest, consoleDigest string) (map[string]any, error) {
	if !stgDigest.MatchString(apiDigest) || !stgDigest.MatchString(consoleDigest) {
		return nil, ErrSTGEvidence
	}
	ctx, cancel := stgContext(ctx)
	defer cancel()
	dyn, err := a.dynamic()
	if err != nil {
		return nil, err
	}
	app, err := dyn.Resource(stgApplicationGVR).Namespace(STGReleaseNamespace).Get(ctx, stgNamespace, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	images, _, err := unstructured.NestedStringSlice(app.Object, "spec", "source", "kustomize", "images")
	if err != nil || len(images) != 2 {
		return nil, fmt.Errorf("%w: Argo pins", ErrSTGEvidence)
	}
	_, multiple, _ := unstructured.NestedFieldNoCopy(app.Object, "spec", "sources")
	if multiple || app.GetUID() == "" {
		return nil, ErrSTGEvidence
	}
	expected := map[string]string{"bifrost-platform-api": apiDigest, "bifrost-platform-console": consoleDigest}
	seen := map[string]bool{}
	for _, image := range images {
		parts := strings.Split(image, "=")
		if len(parts) != 2 {
			return nil, ErrSTGEvidence
		}
		if !strings.HasPrefix(parts[0], "registry.cicd.svc.cluster.local:5000/") {
			return nil, ErrSTGEvidence
		}
		name := strings.TrimPrefix(parts[0], "registry.cicd.svc.cluster.local:5000/")
		if seen[name] || expected[name] == "" || imageDigest(parts[1]) != expected[name] {
			return nil, ErrSTGEvidence
		}
		target := strings.Split(parts[1], "@")[0]
		if !strings.HasSuffix(target, "/"+name) || strings.ContainsAny(target, " \t\n") {
			return nil, ErrSTGEvidence
		}
		seen[name] = true
	}
	return map[string]any{"name": app.GetName(), "uid": string(app.GetUID()), "resource_version": app.GetResourceVersion(), "images": images}, nil
}
