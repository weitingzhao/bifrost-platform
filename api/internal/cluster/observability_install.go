package cluster

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func (s *Service) EnsureKubePrometheusStack() (ActuationResponse, error) {
	now := time.Now().UTC()
	target := "monitoring/kube-prometheus-stack"

	if !observabilityInstallEnabled() {
		return ActuationResponse{
			OK:          false,
			Action:      "ensure-kube-prometheus-stack",
			Target:      target,
			Message:     "observability install disabled (set PLATFORM_OBSERVABILITY_INSTALL_ENABLED=1)",
			GeneratedAt: now,
		}, nil
	}

	absScript := ResolveInfraScript("", "install-kube-prometheus-stack.sh")
	if v := os.Getenv("PLATFORM_OBSERVABILITY_INSTALL_SCRIPT"); v != "" {
		absScript = ResolveInfraScript(v, "install-kube-prometheus-stack.sh")
	}
	if _, err := os.Stat(absScript); err != nil {
		return ActuationResponse{
			OK:          false,
			Action:      "ensure-kube-prometheus-stack",
			Target:      target,
			Message:     fmt.Sprintf("install script not found: %s", absScript),
			GeneratedAt: now,
		}, nil
	}

	kubeconfig := s.kubeconfigPath()
	if kubeconfig == "" {
		home, _ := os.UserHomeDir()
		kubeconfig = filepath.Join(home, ".kube", "bifrost-k3s.yaml")
	}

	cmd := exec.Command(absScript)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("KUBECONFIG=%s", kubeconfig),
		fmt.Sprintf("PLATFORM_KUBECONFIG=%s", kubeconfig),
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return ActuationResponse{
			OK:          false,
			Action:      "ensure-kube-prometheus-stack",
			Target:      target,
			Message:     fmt.Sprintf("install failed: %s", msg),
			GeneratedAt: now,
		}, err
	}

	return ActuationResponse{
		OK:          true,
		Action:      "ensure-kube-prometheus-stack",
		Target:      target,
		Changed:     true,
		Message:     "kube-prometheus-stack installed or already ready",
		GeneratedAt: now,
	}, nil
}

func observabilityInstallEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("PLATFORM_OBSERVABILITY_INSTALL_ENABLED")))
	return v == "1" || v == "true" || v == "yes"
}
