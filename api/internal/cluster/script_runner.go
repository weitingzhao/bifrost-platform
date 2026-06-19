package cluster

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RunInfraK3sScript executes a bifrost-trade-infra/scripts/k3s script with kubeconfig env.
func (s *Service) RunInfraK3sScript(scriptName string, extraEnv []string) error {
	absScript := ResolveInfraScript("", scriptName)
	if _, err := os.Stat(absScript); err != nil {
		return fmt.Errorf("install script not found: %s", absScript)
	}

	kubeconfig := s.kubeconfigPath()
	if kubeconfig == "" {
		home, _ := os.UserHomeDir()
		kubeconfig = filepath.Join(home, ".kube", "bifrost-k3s.yaml")
	}

	ns := "cicd"
	if s.entry != nil {
		ns = s.entry.ResolvedStackNamespace()
	}

	cmd := exec.Command("bash", absScript)
	env := append(os.Environ(),
		fmt.Sprintf("KUBECONFIG=%s", kubeconfig),
		fmt.Sprintf("PLATFORM_KUBECONFIG=%s", kubeconfig),
		fmt.Sprintf("CICD_NAMESPACE=%s", ns),
	)
	env = append(env, extraEnv...)
	cmd.Env = env

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}
