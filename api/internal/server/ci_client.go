package server

import (
	"context"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landingci"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/dynamic"
)

// The CI client's Kubernetes transports are wired here, not through delivery.
func (s *Server) landingCIClient() *landingci.Client {
	namespace := "cicd"
	// Tests build a bare Server; production always has a config.
	if s.cfg != nil {
		if entry := s.cfg.DefaultCluster(); entry != nil && entry.ResolvedStackNamespace() != "" {
			namespace = entry.ResolvedStackNamespace()
		}
	}
	return &landingci.Client{Namespace: namespace, Dynamic: func() (dynamic.Interface, error) {
		cfg, _, err := s.cluster.Service().RestConfig()
		if err != nil {
			return nil, err
		}
		return dynamic.NewForConfig(cfg)
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
