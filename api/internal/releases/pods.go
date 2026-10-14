package releases

import (
	"context"
	"errors"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PodDigests reads the image digest that container runs in every pod of a
// deployment (TD-343): the digest of the container status's imageID, as the
// delivery's rollout check reads it. Pods being deleted count too: while one
// still runs, so does its image. A pod whose container is not running and
// ready, or whose imageID holds no digest, makes the read fail, as does a
// deployment with no pod: what runs is then not known.
func (s *Service) PodDigests(ctx context.Context, namespace, deployment, container string) ([]string, error) {
	if s.clients == nil {
		return nil, errors.New("releases: no cluster client")
	}
	core, _, err := s.clients()
	if err != nil {
		return nil, err
	}
	dep, err := core.AppsV1().Deployments(namespace).Get(ctx, deployment, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	sel, err := metav1.LabelSelectorAsSelector(dep.Spec.Selector)
	if err != nil || sel.Empty() {
		return nil, fmt.Errorf("releases: %s/%s has no usable selector", namespace, deployment)
	}
	pods, err := core.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: sel.String()})
	if err != nil {
		return nil, err
	}
	var out []string
	for _, p := range pods.Items {
		found := false
		for _, cs := range p.Status.ContainerStatuses {
			if cs.Name != container {
				continue
			}
			found = true
			at := strings.LastIndex(cs.ImageID, "@")
			if !cs.Ready || cs.State.Running == nil || at < 0 || !digestRE.MatchString(cs.ImageID[at+1:]) {
				return nil, fmt.Errorf("releases: pod %s/%s: %s is not running a known digest", namespace, p.Name, container)
			}
			out = append(out, cs.ImageID[at+1:])
		}
		if !found {
			return nil, fmt.Errorf("releases: pod %s/%s has no %s container status", namespace, p.Name, container)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("releases: %s/%s has no pod", namespace, deployment)
	}
	return out, nil
}
