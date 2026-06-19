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

func (s *Service) EnsureMetricsServer() (ActuationResponse, error) {
	now := time.Now().UTC()
	target := "kube-system/metrics-server"

	if !metricsServerScriptEnabled() {
		return ActuationResponse{
			OK:          false,
			Action:      "ensure-metrics-server",
			Target:      target,
			Message:     "metrics-server install disabled (set PLATFORM_METRICS_SERVER_ENABLED=1)",
			GeneratedAt: now,
		}, nil
	}

	absScript := ResolveInfraScript("", "install-metrics-server.sh")
	if v := os.Getenv("PLATFORM_METRICS_SERVER_SCRIPT"); v != "" {
		absScript = ResolveInfraScript(v, "install-metrics-server.sh")
	}
	if _, err := os.Stat(absScript); err != nil {
		return ActuationResponse{
			OK:          false,
			Action:      "ensure-metrics-server",
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
			Action:      "ensure-metrics-server",
			Target:      target,
			Message:     fmt.Sprintf("install failed: %s", msg),
			GeneratedAt: now,
		}, err
	}

	return ActuationResponse{
		OK:          true,
		Action:      "ensure-metrics-server",
		Target:      target,
		Changed:     true,
		Message:     "metrics-server installed or already ready",
		GeneratedAt: now,
	}, nil
}

func metricsServerScriptEnabled() bool {
	v := os.Getenv("PLATFORM_METRICS_SERVER_ENABLED")
	if v == "0" || v == "false" || v == "no" {
		return false
	}
	return v == "1" || v == "true" || v == "yes" || v == ""
}
