package cluster

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// PodExecFunc runs a command inside a CNPG postgres container (injectable for tests).
type PodExecFunc func(ctx context.Context, kubeconfig, namespace, pod, container string, command ...string) (string, error)

func (s *Service) podExec() PodExecFunc {
	if s != nil && s.execFunc != nil {
		return s.execFunc
	}
	return defaultKubectlExec
}

// SetPodExecForTest injects a fake pod exec (unit tests only).
func (s *Service) SetPodExecForTest(fn PodExecFunc) {
	s.execFunc = fn
}

func (s *Service) execOnPrimary(ctx context.Context, primary string, command ...string) (string, error) {
	kubeconfig := s.kubeconfigPath()
	if kubeconfig == "" {
		home, _ := os.UserHomeDir()
		kubeconfig = filepath.Join(home, ".kube", "bifrost-k3s.yaml")
	}
	return s.podExec()(ctx, kubeconfig, cnpgNamespace, primary, "postgres", command...)
}

// ExecSQLOnPrimary runs `psql -tAc <sql>` on the CNPG primary against ``database``.
// Used by plugin probes (e.g. market-data ingest_freshness).
func (s *Service) ExecSQLOnPrimary(ctx context.Context, database, sql string) (string, error) {
	if s == nil {
		return "", fmt.Errorf("cluster service unavailable")
	}
	db := strings.TrimSpace(database)
	if db == "" {
		return "", fmt.Errorf("database is required")
	}
	primary, err := s.resolveCNPGPrimary(ctx)
	if err != nil {
		return "", err
	}
	return s.execOnPrimary(ctx, primary, "psql", "-U", "postgres", "-d", db, "-tAc", sql)
}

func defaultKubectlExec(ctx context.Context, kubeconfig, namespace, pod, container string, command ...string) (string, error) {
	args := []string{"exec", "-n", namespace, pod, "-c", container, "--"}
	args = append(args, command...)
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	cmd.Env = append(os.Environ(), "KUBECONFIG="+kubeconfig)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("kubectl exec %s: %s", pod, msg)
	}
	return stdout.String(), nil
}
