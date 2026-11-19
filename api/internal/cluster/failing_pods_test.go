package cluster

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func phasePod(ns, name string, phase corev1.PodPhase, labels map[string]string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name, Labels: labels},
		Status:     corev1.PodStatus{Phase: phase},
	}
}

func TestFailedTektonStepIsNotAFailingWorkload(t *testing.T) {
	// 2026-09-28: verify-research fails by design on a first build; its pod held
	// the cluster degraded and every launch gate NO-GO until the run was pruned.
	step := phasePod("cicd", "bifrost-deliver-research-1-verify-research-pod", corev1.PodFailed,
		map[string]string{tektonTaskRunLabel: "bifrost-deliver-research-1-verify-research"})
	job := phasePod("research", "research-harness-1-abc", corev1.PodFailed, nil)
	pods := []corev1.Pod{step, job}

	if isFailingPod(step) {
		t.Fatal("finished Tekton step counted as a failing workload")
	}
	if !isFailingPod(job) {
		t.Fatal("failed Job pod no longer counted")
	}
	if got := countFailingPods(pods); got != 1 {
		t.Fatalf("countFailingPods = %d, want 1", got)
	}
	details := collectFailingPodDetails(pods, time.Now())
	if len(details) != 1 || details[0].Namespace != "research" {
		t.Fatalf("details = %+v, want only the research Job pod", details)
	}
}

func TestTektonStepStuckPullingIsStillFailing(t *testing.T) {
	p := phasePod("cicd", "bifrost-build-x-pod", corev1.PodPending,
		map[string]string{tektonTaskRunLabel: "bifrost-build-x"})
	p.Status.ContainerStatuses = []corev1.ContainerStatus{{
		State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}},
	}}
	if !isFailingPod(p) {
		t.Fatal("a Tekton step that cannot pull its image is a live fault")
	}
}
