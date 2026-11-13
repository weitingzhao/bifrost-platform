package server

import (
	"context"
	"os"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landingci"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
)

// Landing transports are wired here, not through delivery. Evidence and
// creation policy live entirely within the protected landingci package.
func (s *Server) landingCIClient() *landingci.Client {
	namespace := "cicd"
	// Tests build a bare Server; production always has a config.
	if s.cfg != nil {
		if entry := s.cfg.DefaultCluster(); entry != nil && entry.ResolvedStackNamespace() != "" {
			namespace = entry.ResolvedStackNamespace()
		}
	}
	return &landingci.Client{Namespace: namespace, BaseParam: controller.LandingCIBaseParam(os.Getenv), Dynamic: func() (dynamic.Interface, error) {
		cfg, _, err := s.cluster.Service().RestConfig()
		if err != nil {
			return nil, err
		}
		return dynamic.NewForConfig(cfg)
	}, ConfigMap: func(ctx context.Context, name string) (map[string]string, bool, error) {
		client, _, err := s.cluster.Service().KubernetesClient()
		if err != nil {
			return nil, false, err
		}
		cm, err := client.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		return cm.Data, true, nil
	}, Logs: func(ctx context.Context, pod, container string) (string, error) {
		client, _, err := s.cluster.Service().KubernetesClient()
		if err != nil {
			return "", err
		}
		lines, limit := int64(200), int64(4*landing.CIFailureLogBytes)
		b, err := client.CoreV1().Pods(namespace).GetLogs(pod, &corev1.PodLogOptions{Container: container, TailLines: &lines, LimitBytes: &limit}).DoRaw(ctx)
		return string(b), err
	}}
}
