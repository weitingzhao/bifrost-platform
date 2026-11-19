package cluster

import (
	"fmt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"reflect"
	"testing"
	"time"
)

func baselinePodWorkload(p corev1.Pod) WorkloadView {
	ready := 0
	total := len(p.Status.ContainerStatuses)
	restarts := int32(0)
	for _, cs := range p.Status.ContainerStatuses {
		if cs.Ready {
			ready++
		}
		restarts += cs.RestartCount
	}
	phase := string(p.Status.Phase)
	reach := podReachability(phase)
	return WorkloadView{
		Namespace:    p.Namespace,
		Kind:         "Pod",
		Name:         p.Name,
		Ready:        fmt.Sprintf("%d/%d", ready, maxInt(total, len(p.Spec.Containers))),
		Status:       phase,
		Restarts:     restarts,
		Age:          formatAge(p.CreationTimestamp.Time),
		Reachability: reach,
	}
}

func TestPodProjectionOriginalParity(t *testing.T) {
	for _, phase := range []corev1.PodPhase{"", corev1.PodPending, corev1.PodRunning, corev1.PodSucceeded, corev1.PodFailed, corev1.PodUnknown, "other"} {
		for _, count := range []int{0, 1, 4} {
			p := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: "ns", CreationTimestamp: metav1.NewTime(time.Now().Add(-49 * time.Hour))}, Spec: corev1.PodSpec{Containers: make([]corev1.Container, count)}, Status: corev1.PodStatus{Phase: phase, ContainerStatuses: []corev1.ContainerStatus{{Ready: true, RestartCount: 3}, {Ready: false, RestartCount: 7}}}}
			if got, want := podWorkload(p), baselinePodWorkload(p); !reflect.DeepEqual(got, want) {
				t.Fatalf("phase=%s count=%d got=%#v want=%#v", phase, count, got, want)
			}
		}
	}
	if got, want := podWorkload(corev1.Pod{}), baselinePodWorkload(corev1.Pod{}); !reflect.DeepEqual(got, want) {
		t.Fatalf("zero: %#v != %#v", got, want)
	}
}
