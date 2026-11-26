package cluster

import (
	"context"
	"errors"
	"fmt"
	"github.com/weitingzhao/bifrost-platform/api/internal/kubeapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/resourceview"
	"io"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// The actuation shapes live in kubeapi (2026-11-19) so that other domains can
// name them without importing this package.
type (
	ActuationResponse     = kubeapi.ActuationResponse
	RolloutRestartRequest = kubeapi.RolloutRestartRequest
	ScaleRequest          = kubeapi.ScaleRequest
	LogsResponse          = kubeapi.LogsResponse
)

// EnsureBifrostNamespaces creates the named namespaces that do not exist yet.
// Every name must be listed under bifrost_namespaces in this platform's config,
// and at least one name is required: the list carries legacy names nobody uses
// (2026-11-16, card #585 would have created bifrost and data-warehouse along
// with bifrost-bus), so the caller says exactly which ones it wants.
func (s *Service) EnsureBifrostNamespaces(ctx context.Context, names []string) (ActuationResponse, error) {
	now := time.Now().UTC()
	namespaces, err := s.selectBifrostNamespaces(names)
	if err != nil {
		return ActuationResponse{OK: false, Action: "ensure-bifrost-namespaces", Target: strings.Join(names, ","), Message: err.Error(), GeneratedAt: now}, err
	}
	clientset, _, err := s.buildClient()
	if err != nil {
		return ActuationResponse{OK: false, Action: "ensure-bifrost-namespaces", Target: strings.Join(namespaces, ","), Message: err.Error(), GeneratedAt: now}, err
	}

	created := make([]string, 0)
	for _, ns := range namespaces {
		name := strings.TrimSpace(ns)
		if name == "" {
			continue
		}
		_, getErr := clientset.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
		if getErr == nil {
			continue
		}
		if !apierrors.IsNotFound(getErr) {
			return ActuationResponse{OK: false, Action: "ensure-bifrost-namespaces", Target: name, Message: getErr.Error(), GeneratedAt: now}, getErr
		}
		_, createErr := clientset.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
				Labels: map[string]string{
					"app.kubernetes.io/part-of": "bifrost",
					"bifrost.dev/managed-by":    "platform-api",
				},
			},
		}, metav1.CreateOptions{})
		if createErr != nil && !apierrors.IsAlreadyExists(createErr) {
			return ActuationResponse{OK: false, Action: "ensure-bifrost-namespaces", Target: name, Message: createErr.Error(), GeneratedAt: now}, createErr
		}
		if createErr == nil {
			created = append(created, name)
		}
	}

	message := "bifrost namespaces already exist"
	if len(created) > 0 {
		message = fmt.Sprintf("created namespaces: %s", strings.Join(created, ", "))
	}
	return ActuationResponse{
		OK:          true,
		Action:      "ensure-bifrost-namespaces",
		Target:      strings.Join(namespaces, ","),
		Changed:     len(created) > 0,
		Message:     message,
		Namespaces:  namespaces,
		GeneratedAt: now,
	}, nil
}

// ErrNamespaceSelection marks a names list the config does not allow.
var ErrNamespaceSelection = errors.New("namespace selection")

func (s *Service) selectBifrostNamespaces(names []string) ([]string, error) {
	allowed := map[string]bool{}
	if s.entry != nil {
		for _, ns := range s.entry.BifrostNamespaces {
			allowed[strings.TrimSpace(ns)] = true
		}
	}
	out := make([]string, 0, len(names))
	seen := map[string]bool{}
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" || seen[n] {
			continue
		}
		if !allowed[n] {
			return nil, fmt.Errorf("%w: %q is not under bifrost_namespaces in this platform's config", ErrNamespaceSelection, n)
		}
		seen[n] = true
		out = append(out, n)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: name at least one namespace", ErrNamespaceSelection)
	}
	return out, nil
}

func (s *Service) RolloutRestart(ctx context.Context, req RolloutRestartRequest) (ActuationResponse, error) {
	now := time.Now().UTC()
	if err := validateDeploymentTarget(req.Namespace, req.Kind, req.Name); err != nil {
		return ActuationResponse{OK: false, Action: "rollout-restart", Target: req.Target(), Message: err.Error(), GeneratedAt: now}, err
	}
	clientset, _, err := s.buildClient()
	if err != nil {
		return ActuationResponse{OK: false, Action: "rollout-restart", Target: req.Target(), Message: err.Error(), GeneratedAt: now}, err
	}
	deploy, err := clientset.AppsV1().Deployments(req.Namespace).Get(ctx, req.Name, metav1.GetOptions{})
	if err != nil {
		return ActuationResponse{OK: false, Action: "rollout-restart", Target: req.Target(), Message: err.Error(), GeneratedAt: now}, err
	}
	if deploy.Spec.Template.Annotations == nil {
		deploy.Spec.Template.Annotations = map[string]string{}
	}
	deploy.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = now.Format(time.RFC3339)
	_, err = clientset.AppsV1().Deployments(req.Namespace).Update(ctx, deploy, metav1.UpdateOptions{})
	if err != nil {
		return ActuationResponse{OK: false, Action: "rollout-restart", Target: req.Target(), Message: err.Error(), GeneratedAt: now}, err
	}
	return ActuationResponse{OK: true, Action: "rollout-restart", Target: req.Target(), Changed: true, Message: "deployment rollout restart requested", GeneratedAt: now}, nil
}

func (s *Service) Scale(ctx context.Context, req ScaleRequest) (ActuationResponse, error) {
	now := time.Now().UTC()
	if err := validateDeploymentTarget(req.Namespace, req.Kind, req.Name); err != nil {
		return ActuationResponse{OK: false, Action: "scale", Target: req.Target(), Message: err.Error(), GeneratedAt: now}, err
	}
	if req.Replicas < 0 || req.Replicas > 20 {
		err := fmt.Errorf("replicas must be between 0 and 20")
		return ActuationResponse{OK: false, Action: "scale", Target: req.Target(), Message: err.Error(), GeneratedAt: now}, err
	}
	clientset, _, err := s.buildClient()
	if err != nil {
		return ActuationResponse{OK: false, Action: "scale", Target: req.Target(), Message: err.Error(), GeneratedAt: now}, err
	}
	deploy, err := clientset.AppsV1().Deployments(req.Namespace).Get(ctx, req.Name, metav1.GetOptions{})
	if err != nil {
		return ActuationResponse{OK: false, Action: "scale", Target: req.Target(), Message: err.Error(), GeneratedAt: now}, err
	}
	current := int32(0)
	if deploy.Spec.Replicas != nil {
		current = *deploy.Spec.Replicas
	}
	// D10 freeze: any replica increase of the Trade daemon is refused until
	// spine decisions[id=D10].status is UNLOCKED. Scale-down stays allowed.
	// Fail closed when the spine cannot be read (same field preflight.js reads).
	if err := s.refuseDaemonScaleUp(req.Name, current, req.Replicas); err != nil {
		return ActuationResponse{OK: false, Action: "scale", Target: req.Target(), Message: err.Error(), GeneratedAt: now}, err
	}
	changed := current != req.Replicas
	deploy.Spec.Replicas = &req.Replicas
	_, err = clientset.AppsV1().Deployments(req.Namespace).Update(ctx, deploy, metav1.UpdateOptions{})
	if err != nil {
		return ActuationResponse{OK: false, Action: "scale", Target: req.Target(), Message: err.Error(), GeneratedAt: now}, err
	}
	// The message is what the audit record keeps: the replicas before are the
	// way back (decision #783, wall:106: scaling is tier B because it is
	// undone by scaling back).
	return ActuationResponse{OK: true, Action: "scale", Target: req.Target(), Changed: changed,
		Message: fmt.Sprintf("deployment scaled from %d to %d replicas; undo: scale back to %d", current, req.Replicas, current), GeneratedAt: now}, nil
}

func (s *Service) DeletePod(ctx context.Context, namespace, name string) (ActuationResponse, error) {
	now := time.Now().UTC()
	if namespace == "" || name == "" {
		err := fmt.Errorf("namespace and pod name are required")
		return ActuationResponse{OK: false, Action: "delete-pod", Target: namespace + "/" + name, Message: err.Error(), GeneratedAt: now}, err
	}
	clientset, _, err := s.buildClient()
	if err != nil {
		return ActuationResponse{OK: false, Action: "delete-pod", Target: namespace + "/Pod/" + name, Message: err.Error(), GeneratedAt: now}, err
	}
	err = clientset.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return ActuationResponse{OK: false, Action: "delete-pod", Target: namespace + "/Pod/" + name, Message: err.Error(), GeneratedAt: now}, err
	}
	return ActuationResponse{OK: true, Action: "delete-pod", Target: namespace + "/Pod/" + name, Changed: true, Message: "pod delete requested", GeneratedAt: now}, nil
}

func (s *Service) PodLogs(ctx context.Context, namespace, name string, tailLines int64, container string) (LogsResponse, error) {
	if tailLines <= 0 {
		tailLines = 200
	}
	if tailLines > 2000 {
		tailLines = 2000
	}
	clientset, _, err := s.buildClient()
	if err != nil {
		return LogsResponse{}, err
	}
	req := clientset.CoreV1().Pods(namespace).GetLogs(name, &corev1.PodLogOptions{
		Container: container,
		TailLines: &tailLines,
	})
	stream, err := req.Stream(ctx)
	if err != nil {
		return LogsResponse{}, err
	}
	defer func() { _ = stream.Close() }()
	data, err := io.ReadAll(stream)
	if err != nil {
		return LogsResponse{}, err
	}
	return LogsResponse{Namespace: namespace, Pod: name, Container: container, TailLines: tailLines, Logs: string(data)}, nil
}

func deploymentWorkload(d appsv1.Deployment) WorkloadView {
	return WorkloadView(resourceview.Deployment(d, formatAge(d.CreationTimestamp.Time), podReachability))
}

func validateDeploymentTarget(namespace, kind, name string) error {
	if namespace == "" || name == "" {
		return fmt.Errorf("namespace and name are required")
	}
	if kind != "Deployment" {
		return fmt.Errorf("only Deployment is supported in P1")
	}
	return nil
}
