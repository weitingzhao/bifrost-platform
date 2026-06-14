package cluster

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ActuationResponse struct {
	OK          bool      `json:"ok"`
	Action      string    `json:"action"`
	Target      string    `json:"target"`
	Changed     bool      `json:"changed"`
	Message     string    `json:"message"`
	Namespaces  []string  `json:"namespaces,omitempty"`
	GeneratedAt time.Time `json:"generated_at"`
}

type RolloutRestartRequest struct {
	Namespace string `json:"namespace"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
}

type ScaleRequest struct {
	Namespace string `json:"namespace"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Replicas  int32  `json:"replicas"`
}

type LogsResponse struct {
	Namespace string `json:"namespace"`
	Pod       string `json:"pod"`
	Container string `json:"container,omitempty"`
	TailLines int64  `json:"tail_lines"`
	Logs      string `json:"logs"`
}

func (s *Service) EnsureBifrostNamespaces(ctx context.Context) (ActuationResponse, error) {
	now := time.Now().UTC()
	namespaces := []string{"bifrost"}
	if s.entry != nil && len(s.entry.BifrostNamespaces) > 0 {
		namespaces = s.entry.BifrostNamespaces
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
	changed := deploy.Spec.Replicas == nil || *deploy.Spec.Replicas != req.Replicas
	deploy.Spec.Replicas = &req.Replicas
	_, err = clientset.AppsV1().Deployments(req.Namespace).Update(ctx, deploy, metav1.UpdateOptions{})
	if err != nil {
		return ActuationResponse{OK: false, Action: "scale", Target: req.Target(), Message: err.Error(), GeneratedAt: now}, err
	}
	return ActuationResponse{OK: true, Action: "scale", Target: req.Target(), Changed: changed, Message: fmt.Sprintf("deployment scaled to %d replicas", req.Replicas), GeneratedAt: now}, nil
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
	defer stream.Close()
	data, err := io.ReadAll(stream)
	if err != nil {
		return LogsResponse{}, err
	}
	return LogsResponse{Namespace: namespace, Pod: name, Container: container, TailLines: tailLines, Logs: string(data)}, nil
}

func deploymentWorkload(d appsv1.Deployment) WorkloadView {
	replicas := int32(0)
	if d.Spec.Replicas != nil {
		replicas = *d.Spec.Replicas
	}
	reach := podReachability("Running")
	status := "Ready"
	if d.Status.ReadyReplicas < replicas {
		reach = podReachability("Pending")
		status = "Progressing"
	}
	if replicas > 0 && d.Status.AvailableReplicas == 0 {
		reach = podReachability("Failed")
		status = "Unavailable"
	}
	return WorkloadView{
		Namespace:    d.Namespace,
		Kind:         "Deployment",
		Name:         d.Name,
		Ready:        fmt.Sprintf("%d/%d", d.Status.ReadyReplicas, replicas),
		Status:       status,
		Restarts:     0,
		Age:          formatAge(d.CreationTimestamp.Time),
		Reachability: reach,
	}
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

func (r RolloutRestartRequest) Target() string {
	return r.Namespace + "/" + r.Kind + "/" + r.Name
}

func (r ScaleRequest) Target() string {
	return r.Namespace + "/" + r.Kind + "/" + r.Name
}
