package cluster

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func (s *Service) SyncKubeconfig() SyncResponse {
	if !syncEnabled() {
		return SyncResponse{
			OK:      false,
			Path:    s.kubeconfigPath(),
			Message: "kubeconfig sync disabled (set PLATFORM_CLUSTER_SYNC_ENABLED=1)",
		}
	}

	script := defaultSyncScript()
	absScript, err := filepath.Abs(script)
	if err != nil {
		absScript = script
	}
	if _, err := os.Stat(absScript); err != nil {
		return SyncResponse{
			OK:      false,
			Path:    s.kubeconfigPath(),
			Message: fmt.Sprintf("sync script not found: %s", absScript),
		}
	}

	outPath := s.kubeconfigPath()
	if outPath == "" {
		home, _ := os.UserHomeDir()
		outPath = filepath.Join(home, ".kube", "bifrost-k3s.yaml")
	}

	remote := "vision@192.168.10.73"
	nodeIP := "192.168.10.73"
	if s.entry != nil {
		if s.entry.SSHHost != "" {
			remote = s.entry.SSHHost
		}
		if s.entry.NodeIP != "" {
			nodeIP = s.entry.NodeIP
		}
	}

	cmd := exec.Command(absScript, remote)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("KUBECONFIG_OUT=%s", outPath),
		fmt.Sprintf("K3S_NODE_IP=%s", nodeIP),
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return SyncResponse{
			OK:      false,
			Path:    outPath,
			Message: fmt.Sprintf("sync failed: %s", msg),
		}
	}

	return SyncResponse{
		OK:      true,
		Path:    outPath,
		Message: fmt.Sprintf("kubeconfig synced to %s", outPath),
	}
}
