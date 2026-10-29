package releases

import (
	"context"
	"errors"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

// revisionKey is the annotation a Deployment and its ReplicaSets carry.
const revisionKey = "deployment.kubernetes.io/revision"

// PodDigests reads the image digest that container runs in every pod of a
// deployment (TD-343), under the conditions the delivery's rollout check
// (task-rollout-verify-platform) sets, so a deployment reads as known only
// once it has settled (review 167 A1): the Deployment's generation is
// observed and every replica updated, ready and available; its selector is
// plain match labels; every active pod belongs to the current revision's
// ReplicaSet of this Deployment, as many as its replicas; and each pod runs
// the one container named, with no init or ephemeral container, and one
// status for it, running and ready, whose imageID holds a digest. A pod
// being deleted must run the same digest too: while it runs, so does its
// image. Anything else is an error: what runs is then not known.
func (s *Service) PodDigests(ctx context.Context, namespace, deployment, container string) ([]string, error) {
	if s.clients == nil {
		return nil, errors.New("releases: no cluster client")
	}
	core, _, err := s.clients()
	if err != nil {
		return nil, err
	}
	return ReadPodDigests(ctx, core, namespace, deployment, container)
}

// ReadPodDigests exposes the same strict workload read to evidence adapters
// with an injected Kubernetes client, without constructing a recorder.
func ReadPodDigests(ctx context.Context, core kubernetes.Interface, namespace, deployment, container string) ([]string, error) {
	if core == nil {
		return nil, errors.New("releases: no cluster client")
	}
	dep, err := core.AppsV1().Deployments(namespace).Get(ctx, deployment, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	sel, replicas, err := settled(dep)
	if err != nil {
		return nil, fmt.Errorf("releases: %s/%s: %w", namespace, deployment, err)
	}
	pods, err := core.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: sel})
	if err != nil {
		return nil, err
	}
	if pods.Continue != "" {
		return nil, fmt.Errorf("releases: %s/%s: the pod list is incomplete", namespace, deployment)
	}
	sets := map[string]bool{} // ReplicaSet name -> of the current revision of this Deployment
	var out []string
	active := 0
	for i := range pods.Items {
		p := &pods.Items[i]
		if p.DeletionTimestamp == nil && p.Status.Phase != corev1.PodSucceeded && p.Status.Phase != corev1.PodFailed {
			active++
			current, err := currentRevision(ctx, core, dep, p, sets)
			if err != nil {
				return nil, err
			}
			if !current {
				return nil, fmt.Errorf("releases: pod %s/%s is not of the current revision", namespace, p.Name)
			}
		}
		d, err := runs(p, container)
		if err != nil {
			return nil, fmt.Errorf("releases: pod %s/%s: %w", namespace, p.Name, err)
		}
		out = append(out, d)
	}
	if active != replicas {
		return nil, fmt.Errorf("releases: %s/%s has %d active pods, not %d", namespace, deployment, active, replicas)
	}
	return out, nil
}

// settled checks that a Deployment has rolled out completely and returns
// its selector and replica count.
func settled(dep *appsv1.Deployment) (string, int, error) {
	if dep.Spec.Replicas == nil || *dep.Spec.Replicas < 1 {
		return "", 0, errors.New("no positive replica count")
	}
	replicas := *dep.Spec.Replicas
	st := dep.Status
	switch {
	case dep.Generation < 1 || st.ObservedGeneration != dep.Generation:
		return "", 0, errors.New("its generation is not observed")
	case st.Replicas != replicas || st.UpdatedReplicas != replicas || st.ReadyReplicas != replicas || st.AvailableReplicas != replicas:
		return "", 0, errors.New("its replicas are not fully rolled out")
	case dep.Annotations[revisionKey] == "":
		return "", 0, errors.New("it has no revision")
	case dep.Spec.Selector == nil || len(dep.Spec.Selector.MatchLabels) == 0 || len(dep.Spec.Selector.MatchExpressions) > 0:
		return "", 0, errors.New("its selector is not plain match labels")
	}
	return labels.SelectorFromSet(dep.Spec.Selector.MatchLabels).String(), int(replicas), nil
}

// currentRevision: the pod's controller is a ReplicaSet whose controller is
// this Deployment and whose revision is the Deployment's (sets caches the
// ReplicaSets read).
func currentRevision(ctx context.Context, core kubernetes.Interface, dep *appsv1.Deployment, p *corev1.Pod, sets map[string]bool) (bool, error) {
	owner := metav1.GetControllerOf(p)
	if owner == nil || owner.Kind != "ReplicaSet" {
		return false, nil
	}
	if ok, seen := sets[string(owner.UID)]; seen {
		return ok, nil
	}
	rs, err := core.AppsV1().ReplicaSets(p.Namespace).Get(ctx, owner.Name, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	up := metav1.GetControllerOf(rs)
	ok := rs.UID == owner.UID && up != nil && up.Kind == "Deployment" && up.UID == dep.UID &&
		rs.Annotations[revisionKey] == dep.Annotations[revisionKey]
	sets[string(owner.UID)] = ok
	return ok, nil
}

// runs is the digest the pod's one container runs.
func runs(p *corev1.Pod, container string) (string, error) {
	if len(p.Spec.Containers) != 1 || p.Spec.Containers[0].Name != container ||
		len(p.Spec.InitContainers) > 0 || len(p.Spec.EphemeralContainers) > 0 ||
		len(p.Status.InitContainerStatuses) > 0 || len(p.Status.EphemeralContainerStatuses) > 0 {
		return "", fmt.Errorf("it does not run %s alone", container)
	}
	if len(p.Status.ContainerStatuses) != 1 || p.Status.ContainerStatuses[0].Name != container {
		return "", fmt.Errorf("it has no single status for %s", container)
	}
	cs := p.Status.ContainerStatuses[0]
	at := strings.LastIndex(cs.ImageID, "@")
	switch {
	case !cs.Ready:
		return "", fmt.Errorf("%s is not ready", container)
	case cs.State.Running == nil:
		return "", fmt.Errorf("%s is not running", container)
	case at < 0 || !digestRE.MatchString(cs.ImageID[at+1:]):
		return "", fmt.Errorf("%s runs no known digest", container)
	}
	return cs.ImageID[at+1:], nil
}
