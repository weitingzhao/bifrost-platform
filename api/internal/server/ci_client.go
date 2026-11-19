package server

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landingci"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
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
	namespace := landingci.DefaultNamespace
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
	}, Logs: landingci.PodLogs(func() (kubernetes.Interface, error) {
		client, _, err := s.cluster.Service().KubernetesClient()
		return client, err
	}, namespace)}
}
