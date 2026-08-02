package devsession

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
)

const sessionDesiredReplicasAnnotation = "bifrost.dev/session-desired-replicas"

// K8sProvider maps catalog Deployments to DevSession via the cluster client.
type K8sProvider struct {
	cluster *cluster.Service
	catalog *SessionsCatalog
	env     string
}

func NewK8sProvider(clusterSvc *cluster.Service, catalog *SessionsCatalog, env string) *K8sProvider {
	if catalog == nil {
		catalog = &SessionsCatalog{Envs: map[string][]CatalogEntry{}}
	}
	return &K8sProvider{cluster: clusterSvc, catalog: catalog, env: env}
}

func (p *K8sProvider) List(ctx context.Context) ([]DevSession, error) {
	clientset, err := p.client()
	if err != nil {
		return nil, fmt.Errorf("kubernetes unavailable: %w", err)
	}
	entries, err := p.resolveEntries(ctx, clientset)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return []DevSession{}, nil
	}

	out := make([]DevSession, 0, len(entries))
	for _, e := range entries {
		sess, mapErr := p.mapDeployment(ctx, clientset, e)
		if mapErr != nil {
			// Surface missing/unreachable workloads as stopped/error rows, not a hard fail.
			sess = baseSession(e, p.env)
			sess.Status = "error"
			if apierrors.IsNotFound(mapErr) {
				sess.Status = "stopped"
			}
			falseVal := false
			sess.HealthOK = &falseVal
		}
		out = append(out, sess)
	}
	return out, nil
}

func (p *K8sProvider) Logs(ctx context.Context, name string, lines int) (*LogResponse, error) {
	clientset, err := p.client()
	if err != nil {
		return nil, fmt.Errorf("kubernetes unavailable: %w", err)
	}
	entry, err := p.resolveEntry(ctx, clientset, name)
	if err != nil {
		return nil, err
	}
	if lines <= 0 {
		lines = 200
	}
	if lines > 2000 {
		lines = 2000
	}
	podName, err := p.pickPod(ctx, clientset, entry.Namespace, entry.Deployment)
	if err != nil {
		return &LogResponse{Name: name, Lines: []string{err.Error()}}, nil
	}
	resp, err := p.cluster.PodLogs(ctx, entry.Namespace, podName, int64(lines), "")
	if err != nil {
		return nil, fmt.Errorf("pod logs: %w", err)
	}
	raw := strings.Split(strings.ReplaceAll(resp.Logs, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		if line == "" && len(out) == 0 {
			continue
		}
		out = append(out, line)
	}
	// Drop trailing empty line from split.
	if len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return &LogResponse{Name: name, Lines: out}, nil
}

func (p *K8sProvider) Control(ctx context.Context, name, action string) (*ControlResponse, error) {
	clientset, err := p.client()
	if err != nil {
		return &ControlResponse{
			Name: name, Action: action, Success: false, Message: err.Error(),
		}, fmt.Errorf("kubernetes unavailable: %w", err)
	}
	entry, err := p.resolveEntry(ctx, clientset, name)
	if err != nil {
		return &ControlResponse{
			Name: name, Action: action, Success: false, Message: err.Error(),
		}, err
	}

	switch action {
	case "clear-logs":
		return &ControlResponse{
			Name: name, Action: action, Success: true,
			Message: "not applicable in cluster mode",
		}, nil
	case "restart":
		resp, err := p.cluster.RolloutRestart(ctx, cluster.RolloutRestartRequest{
			Namespace: entry.Namespace,
			Kind:      "Deployment",
			Name:      entry.Deployment,
		})
		return &ControlResponse{
			Name: name, Action: action, Success: resp.OK, Message: resp.Message,
		}, errAsNilWhenMessage(err, resp.OK)
	case "stop":
		return p.scaleStop(ctx, entry, name)
	case "start":
		return p.scaleStart(ctx, entry, name)
	default:
		return &ControlResponse{
			Name: name, Action: action, Success: false, Message: "unknown action: " + action,
		}, fmt.Errorf("unknown action: %s", action)
	}
}

func (p *K8sProvider) scaleStop(ctx context.Context, entry *CatalogEntry, name string) (*ControlResponse, error) {
	clientset, err := p.client()
	if err != nil {
		return &ControlResponse{Name: name, Action: "stop", Success: false, Message: err.Error()}, nil
	}
	deploy, err := clientset.AppsV1().Deployments(entry.Namespace).Get(ctx, entry.Deployment, metav1.GetOptions{})
	if err != nil {
		return &ControlResponse{Name: name, Action: "stop", Success: false, Message: err.Error()}, nil
	}
	current := int32(0)
	if deploy.Spec.Replicas != nil {
		current = *deploy.Spec.Replicas
	}
	if current > 0 {
		if deploy.Annotations == nil {
			deploy.Annotations = map[string]string{}
		}
		deploy.Annotations[sessionDesiredReplicasAnnotation] = strconv.Itoa(int(current))
		_, _ = clientset.AppsV1().Deployments(entry.Namespace).Update(ctx, deploy, metav1.UpdateOptions{})
	}
	resp, err := p.cluster.Scale(ctx, cluster.ScaleRequest{
		Namespace: entry.Namespace,
		Kind:      "Deployment",
		Name:      entry.Deployment,
		Replicas:  0,
	})
	return &ControlResponse{
		Name: name, Action: "stop", Success: resp.OK, Message: resp.Message,
	}, errAsNilWhenMessage(err, resp.OK)
}

func (p *K8sProvider) scaleStart(ctx context.Context, entry *CatalogEntry, name string) (*ControlResponse, error) {
	clientset, err := p.client()
	if err != nil {
		return &ControlResponse{Name: name, Action: "start", Success: false, Message: err.Error()}, nil
	}
	deploy, err := clientset.AppsV1().Deployments(entry.Namespace).Get(ctx, entry.Deployment, metav1.GetOptions{})
	if err != nil {
		return &ControlResponse{Name: name, Action: "start", Success: false, Message: err.Error()}, nil
	}
	target := int32(1)
	if deploy.Annotations != nil {
		if raw := strings.TrimSpace(deploy.Annotations[sessionDesiredReplicasAnnotation]); raw != "" {
			if n, parseErr := strconv.Atoi(raw); parseErr == nil && n > 0 && n <= 20 {
				target = int32(n)
			}
		}
	}
	resp, err := p.cluster.Scale(ctx, cluster.ScaleRequest{
		Namespace: entry.Namespace,
		Kind:      "Deployment",
		Name:      entry.Deployment,
		Replicas:  target,
	})
	return &ControlResponse{
		Name: name, Action: "start", Success: resp.OK, Message: resp.Message,
	}, errAsNilWhenMessage(err, resp.OK)
}

func (p *K8sProvider) client() (kubernetes.Interface, error) {
	if p.cluster == nil {
		return nil, fmt.Errorf("cluster client not configured")
	}
	clientset, _, err := p.cluster.KubernetesClient()
	return clientset, err
}

// resolveEntries returns catalog entries merged with annotation-discovered Deployments.
// Catalog wins on session name or namespace/deployment collisions.
func (p *K8sProvider) resolveEntries(ctx context.Context, clientset kubernetes.Interface) ([]CatalogEntry, error) {
	catalog := p.catalog.EntriesForEnv(p.env)
	byName := make(map[string]struct{}, len(catalog))
	byDeploy := make(map[string]struct{}, len(catalog))
	out := make([]CatalogEntry, 0, len(catalog)+8)
	for _, e := range catalog {
		byName[e.Name] = struct{}{}
		byDeploy[e.Namespace+"/"+e.Deployment] = struct{}{}
		out = append(out, e)
	}
	discovered, err := p.discoverAnnotatedEntries(ctx, clientset)
	if err != nil {
		// Soft-fail discovery: keep catalog rows even if one namespace list fails.
		return out, nil
	}
	for _, e := range discovered {
		if _, ok := byName[e.Name]; ok {
			continue
		}
		if _, ok := byDeploy[e.Namespace+"/"+e.Deployment]; ok {
			continue
		}
		byName[e.Name] = struct{}{}
		byDeploy[e.Namespace+"/"+e.Deployment] = struct{}{}
		out = append(out, e)
	}
	return out, nil
}

func (p *K8sProvider) resolveEntry(ctx context.Context, clientset kubernetes.Interface, name string) (*CatalogEntry, error) {
	if e := p.catalog.Lookup(p.env, name); e != nil {
		return e, nil
	}
	entries, err := p.resolveEntries(ctx, clientset)
	if err != nil {
		return nil, err
	}
	for i := range entries {
		if entries[i].Name == name {
			e := entries[i]
			return &e, nil
		}
	}
	return nil, fmt.Errorf("unknown session: %s", name)
}

func (p *K8sProvider) discoverAnnotatedEntries(ctx context.Context, clientset kubernetes.Interface) ([]CatalogEntry, error) {
	nss := p.catalog.DiscoveryNamespacesForEnv(p.env)
	if len(nss) == 0 {
		return nil, nil
	}
	var out []CatalogEntry
	var firstErr error
	for _, ns := range nss {
		list, err := clientset.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for i := range list.Items {
			d := &list.Items[i]
			anns := d.Annotations
			if anns == nil {
				anns = d.Spec.Template.Annotations
			}
			// Prefer Deployment-level annotation; fall back to pod template.
			if !sessionAnnotationEnabled(anns) {
				if !sessionAnnotationEnabled(d.Spec.Template.Annotations) {
					continue
				}
				anns = d.Spec.Template.Annotations
			}
			ports := containerPortsFromDeploy(d)
			out = append(out, entryFromAnnotatedDeployment(d.Namespace, d.Name, anns, ports))
		}
	}
	if len(out) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

func containerPortsFromDeploy(deploy *appsv1.Deployment) []int {
	if deploy == nil {
		return nil
	}
	var ports []int
	seen := map[int]struct{}{}
	for _, c := range deploy.Spec.Template.Spec.Containers {
		for _, p := range c.Ports {
			port := int(p.ContainerPort)
			if port <= 0 {
				continue
			}
			if _, ok := seen[port]; ok {
				continue
			}
			seen[port] = struct{}{}
			ports = append(ports, port)
		}
	}
	return ports
}

func (p *K8sProvider) mapDeployment(ctx context.Context, clientset kubernetes.Interface, e CatalogEntry) (DevSession, error) {
	deploy, err := clientset.AppsV1().Deployments(e.Namespace).Get(ctx, e.Deployment, metav1.GetOptions{})
	if err != nil {
		return DevSession{}, err
	}
	sess := baseSession(e, p.env)
	desired := int32(0)
	if deploy.Spec.Replicas != nil {
		desired = *deploy.Spec.Replicas
	}
	ready := deploy.Status.ReadyReplicas
	sess.DesiredReplicas = &desired
	sess.ReadyReplicas = &ready
	sess.ImageTag = firstImageTag(deploy)
	sess.Status = k8sStatus(desired, ready, deploy.Status.AvailableReplicas)
	ok := sess.Status == "running"
	sess.HealthOK = &ok

	pods, podErr := listDeployPods(ctx, clientset, e.Namespace, deploy)
	if podErr == nil && len(pods) > 0 {
		restarts := 0
		var oldest *time.Time
		for _, pod := range pods {
			for _, cs := range pod.Status.ContainerStatuses {
				restarts += int(cs.RestartCount)
			}
			if pod.Status.Phase == corev1.PodRunning && pod.Status.StartTime != nil {
				t := pod.Status.StartTime.Time
				if oldest == nil || t.Before(*oldest) {
					oldest = &t
				}
			}
		}
		sess.Restarts = restarts
		if oldest != nil {
			sess.UptimeSec = int(time.Since(*oldest).Seconds())
		}
	}
	return sess, nil
}

func (p *K8sProvider) pickPod(ctx context.Context, clientset kubernetes.Interface, namespace, deployment string) (string, error) {
	deploy, err := clientset.AppsV1().Deployments(namespace).Get(ctx, deployment, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	pods, err := listDeployPods(ctx, clientset, namespace, deploy)
	if err != nil {
		return "", err
	}
	if len(pods) == 0 {
		return "", fmt.Errorf("no pods for deployment %s/%s", namespace, deployment)
	}
	// Prefer Running, then any.
	for _, pod := range pods {
		if pod.Status.Phase == corev1.PodRunning {
			return pod.Name, nil
		}
	}
	return pods[0].Name, nil
}

func listDeployPods(ctx context.Context, clientset kubernetes.Interface, namespace string, deploy *appsv1.Deployment) ([]corev1.Pod, error) {
	if deploy.Spec.Selector == nil {
		return nil, fmt.Errorf("deployment %s/%s has no selector", namespace, deploy.Name)
	}
	selector, err := metav1.LabelSelectorAsSelector(deploy.Spec.Selector)
	if err != nil {
		return nil, err
	}
	list, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

func baseSession(e CatalogEntry, env string) DevSession {
	return DevSession{
		Name:      e.Name,
		Label:     e.Label,
		Group:     e.Group,
		Ports:     e.Ports,
		Status:    "stopped",
		Mode:      ModeK8s,
		Env:       env,
		Namespace: e.Namespace,
	}
}

func k8sStatus(desired, ready, available int32) string {
	if desired == 0 {
		return "stopped"
	}
	if ready >= desired && available > 0 {
		return "running"
	}
	if available == 0 {
		return "error"
	}
	return "error"
}

func firstImageTag(deploy *appsv1.Deployment) string {
	if deploy == nil || len(deploy.Spec.Template.Spec.Containers) == 0 {
		return ""
	}
	image := deploy.Spec.Template.Spec.Containers[0].Image
	if image == "" {
		return ""
	}
	// digest form
	if strings.Contains(image, "@sha256:") {
		parts := strings.Split(image, "@")
		if len(parts) == 2 {
			return parts[1]
		}
	}
	if i := strings.LastIndex(image, ":"); i >= 0 && !strings.Contains(image[i+1:], "/") {
		return image[i+1:]
	}
	return image
}

// errAsNilWhenMessage keeps HTTP handlers returning ControlResponse body on soft failures.
func errAsNilWhenMessage(err error, ok bool) error {
	if ok {
		return nil
	}
	// Scale/Rollout already put detail in Message; do not force 400 via handler err path.
	_ = err
	return nil
}
