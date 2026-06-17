package cluster

import (
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
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

// SetClientFactoryForTest injects a fake kubernetes client (unit tests only).
func (s *Service) SetClientFactoryForTest(factory func() (kubernetes.Interface, string, error)) {
	s.clientFactory = factory
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

// KubernetesClient exposes the cluster kube client for sibling packages (e.g. gitops P3 probe).
func (s *Service) KubernetesClient() (kubernetes.Interface, string, error) {
	return s.buildClient()
}

// RestConfig loads kubeconfig for dynamic clients (Argo CD Application CRDs).
func (s *Service) RestConfig() (*rest.Config, string, error) {
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
	return cfg, path, nil
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
		return resolveSyncScript(v)
	}
	return resolveSyncScript("")
}

// resolveSyncScript finds fetch-kubeconfig.sh. API often runs with cwd=api/, so a
// ../bifrost-trade-infra path from .env must be resolved against repo roots.
func resolveSyncScript(configured string) string {
	infraRel := filepath.Join("bifrost-trade-infra", "scripts", "k3s", "fetch-kubeconfig.sh")

	try := func(p string) (string, bool) {
		p = filepath.Clean(p)
		if _, err := os.Stat(p); err == nil {
			if abs, err := filepath.Abs(p); err == nil {
				return abs, true
			}
			return p, true
		}
		return "", false
	}

	if configured != "" {
		if filepath.IsAbs(configured) {
			if p, ok := try(configured); ok {
				return p
			}
			return configured
		}
		if abs, err := filepath.Abs(configured); err == nil {
			if p, ok := try(abs); ok {
				return p
			}
		}
		if wd, err := os.Getwd(); err == nil {
			for _, base := range []string{wd, filepath.Join(wd, ".."), filepath.Join(wd, "../..")} {
				if p, ok := try(filepath.Join(base, configured)); ok {
					return p
				}
			}
		}
	}

	if wd, err := os.Getwd(); err == nil {
		for _, base := range []string{wd, filepath.Join(wd, ".."), filepath.Join(wd, "../..")} {
			if p, ok := try(filepath.Join(base, "..", infraRel)); ok {
				return p
			}
			if p, ok := try(filepath.Join(base, infraRel)); ok {
				return p
			}
		}
	}
	if root := os.Getenv("PLATFORM_PROJECT_ROOT"); root != "" {
		if p, ok := try(filepath.Join(root, "..", infraRel)); ok {
			return p
		}
	}
	return filepath.Join("..", "..", "bifrost-trade-infra", "scripts", "k3s", "fetch-kubeconfig.sh")
}

func syncEnabled() bool {
	v := os.Getenv("PLATFORM_CLUSTER_SYNC_ENABLED")
	return v == "1" || v == "true" || v == "yes"
}
