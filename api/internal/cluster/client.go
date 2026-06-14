package cluster

import (
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

type Service struct {
	entry         *config.ClusterEntry
	clientFactory func() (kubernetes.Interface, string, error)
}

func NewService(entry *config.ClusterEntry) *Service {
	return &Service{entry: entry}
}

func (s *Service) Entry() *config.ClusterEntry {
	return s.entry
}

func (s *Service) kubeconfigPath() string {
	if s.entry == nil {
		return ""
	}
	return s.entry.KubeconfigPath()
}

func (s *Service) buildClient() (kubernetes.Interface, string, error) {
	if s.clientFactory != nil {
		return s.clientFactory()
	}
	path := s.kubeconfigPath()
	if path == "" {
		return nil, path, &ClientError{
			Reachability: probe.ReachFail,
			Detail:       "kubeconfig path not configured (set PLATFORM_KUBECONFIG)",
		}
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, path, &ClientError{
				Reachability: probe.ReachFail,
				Detail:       fmt.Sprintf("kubeconfig not found: %s (run Sync kubeconfig or make k3s-fetch-kubeconfig)", path),
			}
		}
		return nil, path, &ClientError{
			Reachability: probe.ReachFail,
			Detail:       fmt.Sprintf("kubeconfig unreadable: %s", path),
		}
	}

	cfg, err := clientcmd.BuildConfigFromFlags("", path)
	if err != nil {
		return nil, path, &ClientError{
			Reachability: probe.ReachFail,
			Detail:       fmt.Sprintf("invalid kubeconfig %s: %v", path, err),
		}
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, path, &ClientError{
			Reachability: probe.ReachFail,
			Detail:       fmt.Sprintf("kubernetes client: %v", err),
		}
	}
	return clientset, path, nil
}

func defaultSyncScript() string {
	if v := os.Getenv("PLATFORM_CLUSTER_SYNC_SCRIPT"); v != "" {
		return v
	}
	if wd, err := os.Getwd(); err == nil {
		candidates := []string{
			filepath.Join(wd, "..", "bifrost-trade-infra", "scripts", "k3s", "fetch-kubeconfig.sh"),
			filepath.Join(wd, "..", "..", "bifrost-trade-infra", "scripts", "k3s", "fetch-kubeconfig.sh"),
		}
		for _, p := range candidates {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return "../bifrost-trade-infra/scripts/k3s/fetch-kubeconfig.sh"
}

func syncEnabled() bool {
	v := os.Getenv("PLATFORM_CLUSTER_SYNC_ENABLED")
	return v == "1" || v == "true" || v == "yes"
}
