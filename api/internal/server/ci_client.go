package server

import (
	"context"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landingci"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

// landingGateRuns reads the daily full CI's runs through the bus run cache;
// nil (tests with a bare Server) lists them.
func (s *Server) landingGateRuns() func(string, func(*unstructured.Unstructured) bool, func() ([]unstructured.Unstructured, error)) ([]unstructured.Unstructured, error) {
	if s.delivery == nil {
		return nil
	}
	return func(pipeline string, keep func(*unstructured.Unstructured) bool, listed func() ([]unstructured.Unstructured, error)) ([]unstructured.Unstructured, error) {
		return s.delivery.GateRuns(delivery.RouteLandingFull, []string{pipeline}, keep, listed)
	}
}

// The CI client's Kubernetes transports are wired here, not through delivery.
func (s *Server) landingCIClient() *landingci.Client {
	namespace := "cicd"
	// Tests build a bare Server; production always has a config.
	if s.cfg != nil {
		if entry := s.cfg.DefaultCluster(); entry != nil && entry.ResolvedStackNamespace() != "" {
			namespace = entry.ResolvedStackNamespace()
		}
	}
	return &landingci.Client{Namespace: namespace, GateRuns: s.landingGateRuns(), Dynamic: func() (dynamic.Interface, error) {
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
