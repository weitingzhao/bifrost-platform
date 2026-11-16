package resourceview

import (
	"fmt"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	corev1 "k8s.io/api/core/v1"
)

func Pod(p corev1.Pod, age string, reachForPhase func(string) probe.Reachability) DeploymentView {
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
	reach := reachForPhase(phase)
	return DeploymentView{
		Namespace:    p.Namespace,
		Kind:         "Pod",
		Name:         p.Name,
		Ready:        fmt.Sprintf("%d/%d", ready, max(total, len(p.Spec.Containers))),
		Status:       phase,
		Restarts:     restarts,
		Age:          age,
		Reachability: reach,
	}
}
