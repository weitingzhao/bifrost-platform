package releases

import (
	"context"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

// TD-343: the digests a deployment's pods run, read from the container
// statuses; a pod not running and ready, without a digest, or a deployment
// without pods is an error, never an answer.
func TestPodDigests(t *testing.T) {
	ctx := context.Background()
	d := "sha256:" + strings.Repeat("a", 64)
	dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "platform-api", Namespace: "ns"},
		Spec: appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "platform-api"}}}}
	pod := func(name, container, imageID string, ready bool) *corev1.Pod {
		st := corev1.ContainerStatus{Name: container, ImageID: imageID, Ready: ready}
		if ready {
			st.State.Running = &corev1.ContainerStateRunning{}
		}
		return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns", Labels: map[string]string{"app": "platform-api"}},
			Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{st}}}
	}
	other := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "ns", Labels: map[string]string{"app": "x"}}}
	read := func(objs ...any) ([]string, error) {
		core := k8sfake.NewSimpleClientset()
		for _, o := range objs {
			var err error
			switch v := o.(type) {
			case *appsv1.Deployment:
				_, err = core.AppsV1().Deployments("ns").Create(ctx, v, metav1.CreateOptions{})
			case *corev1.Pod:
				_, err = core.CoreV1().Pods("ns").Create(ctx, v, metav1.CreateOptions{})
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		s := &Service{clients: func() (kubernetes.Interface, dynamic.Interface, error) { return core, nil, nil }}
		return s.PodDigests(ctx, "ns", "platform-api", "platform-api")
	}
	got, err := read(dep, pod("p1", "platform-api", "docker-pullable://registry/platform-api@"+d, true),
		pod("p2", "platform-api", "registry/platform-api@"+d, true), other)
	if err != nil || len(got) != 2 || got[0] != d || got[1] != d {
		t.Fatalf("digests = %v %v", got, err)
	}
	for name, objs := range map[string][]any{
		"not ready":     {dep, pod("p1", "platform-api", "registry/x@"+d, false)},
		"no digest":     {dep, pod("p1", "platform-api", "registry/x:latest", true)},
		"bad digest":    {dep, pod("p1", "platform-api", "registry/x@sha256:short", true)},
		"no container":  {dep, pod("p1", "sidecar", "registry/x@"+d, true)},
		"no pod":        {dep, other},
		"no deployment": {pod("p1", "platform-api", "registry/x@"+d, true)},
	} {
		if got, err := read(objs...); err == nil {
			t.Errorf("%s: read %v", name, got)
		}
	}
}
