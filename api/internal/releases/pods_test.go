package releases

import (
	"context"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

// podWorld is a settled deployment: generation observed, two replicas
// rolled out, the current revision's ReplicaSet, two pods running the one
// container.
type podWorld struct {
	dep  *appsv1.Deployment
	rs   *appsv1.ReplicaSet
	pods []*corev1.Pod
}

var (
	digestA = "sha256:" + strings.Repeat("a", 64)
	digestB = "sha256:" + strings.Repeat("b", 64)
)

func settledWorld() *podWorld {
	two := int32(2)
	ctl := true
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "platform-api", Namespace: "ns", UID: types.UID("dep-uid"), Generation: 4,
			Annotations: map[string]string{revisionKey: "3"}},
		Spec:   appsv1.DeploymentSpec{Replicas: &two, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "platform-api"}}},
		Status: appsv1.DeploymentStatus{ObservedGeneration: 4, Replicas: 2, UpdatedReplicas: 2, ReadyReplicas: 2, AvailableReplicas: 2},
	}
	rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "platform-api-r3", Namespace: "ns", UID: types.UID("rs-uid"),
		Annotations:     map[string]string{revisionKey: "3"},
		OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "platform-api", UID: dep.UID, Controller: &ctl}}}}
	w := &podWorld{dep: dep, rs: rs}
	for _, name := range []string{"p1", "p2"} {
		w.pods = append(w.pods, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns", Labels: map[string]string{"app": "platform-api"},
				OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: rs.Name, UID: rs.UID, Controller: &ctl}}},
			Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "platform-api"}}},
			Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Name: "platform-api", Ready: true,
				ImageID: "docker-pullable://registry/platform-api@" + digestA, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}},
		})
	}
	return w
}

func (w *podWorld) read(t *testing.T) ([]string, error) {
	t.Helper()
	objs := []runtime.Object{w.dep, w.rs}
	for _, p := range w.pods {
		objs = append(objs, p)
	}
	core := k8sfake.NewSimpleClientset(objs...)
	s := &Service{clients: func() (kubernetes.Interface, dynamic.Interface, error) { return core, nil, nil }}
	return s.PodDigests(context.Background(), "ns", "platform-api", "platform-api")
}

// TD-343 and review 167 A1, B2: the digests of a settled deployment's pods;
// any sign that it has not settled, or runs more than the one container,
// is an error, never an answer.
func TestPodDigests(t *testing.T) {
	got, err := settledWorld().read(t)
	if err != nil || len(got) != 2 || got[0] != digestA || got[1] != digestA {
		t.Fatalf("settled = %v %v", got, err)
	}
	// A pod being deleted still counts its digest; it is not one of the replicas.
	w := settledWorld()
	gone := w.pods[0].DeepCopy()
	gone.Name, gone.DeletionTimestamp = "p0", &metav1.Time{}
	gone.Status.ContainerStatuses[0].ImageID = "registry/platform-api@" + digestB
	w.pods = append(w.pods, gone)
	if got, err = w.read(t); err != nil || len(got) != 3 || !strings.Contains(strings.Join(got, ","), digestB) {
		t.Fatalf("with a pod being deleted = %v %v", got, err)
	}

	for name, spoil := range map[string]func(w *podWorld){
		"generation not observed": func(w *podWorld) { w.dep.Status.ObservedGeneration = 3 },
		"not rolled out":          func(w *podWorld) { w.dep.Status.UpdatedReplicas = 1 },
		"not available":           func(w *podWorld) { w.dep.Status.AvailableReplicas = 1 },
		"no replicas":             func(w *podWorld) { w.dep.Spec.Replicas = nil },
		"no revision":             func(w *podWorld) { delete(w.dep.Annotations, revisionKey) },
		"no selector":             func(w *podWorld) { w.dep.Spec.Selector = nil },
		"selector expressions": func(w *podWorld) {
			w.dep.Spec.Selector.MatchExpressions = []metav1.LabelSelectorRequirement{{Key: "x", Operator: metav1.LabelSelectorOpExists}}
		},
		"an old revision's pod":    func(w *podWorld) { w.rs.Annotations[revisionKey] = "2" },
		"another deployment's set": func(w *podWorld) { w.rs.OwnerReferences[0].UID = "other" },
		"a pod with no controller": func(w *podWorld) { w.pods[0].OwnerReferences = nil },
		"fewer pods than replicas": func(w *podWorld) { w.pods = w.pods[:1] },
		"a sidecar": func(w *podWorld) {
			w.pods[0].Spec.Containers = append(w.pods[0].Spec.Containers, corev1.Container{Name: "proxy"})
		},
		"an init container": func(w *podWorld) { w.pods[0].Spec.InitContainers = []corev1.Container{{Name: "init"}} },
		"two statuses for the container": func(w *podWorld) {
			w.pods[0].Status.ContainerStatuses = append(w.pods[0].Status.ContainerStatuses, w.pods[0].Status.ContainerStatuses[0])
		},
		"another container's status": func(w *podWorld) { w.pods[0].Status.ContainerStatuses[0].Name = "proxy" },
		"running, not ready":         func(w *podWorld) { w.pods[0].Status.ContainerStatuses[0].Ready = false },
		"ready, not running":         func(w *podWorld) { w.pods[0].Status.ContainerStatuses[0].State.Running = nil },
		"no digest":                  func(w *podWorld) { w.pods[0].Status.ContainerStatuses[0].ImageID = "registry/platform-api:latest" },
		"a malformed digest":         func(w *podWorld) { w.pods[0].Status.ContainerStatuses[0].ImageID = "registry/x@sha256:short" },
	} {
		w := settledWorld()
		spoil(w)
		if got, err := w.read(t); err == nil {
			t.Errorf("%s: read %v", name, got)
		}
	}
}
