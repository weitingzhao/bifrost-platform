package cluster

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

// PodExecFunc runs a command inside a CNPG postgres container (injectable for tests).
type PodExecFunc func(ctx context.Context, kubeconfig, namespace, pod, container string, command ...string) (string, error)

func (s *Service) podExec() PodExecFunc {
	if s != nil && s.execFunc != nil {
		return s.execFunc
	}
	return nil
}

// SetPodExecForTest injects a fake pod exec (unit tests only).
func (s *Service) SetPodExecForTest(fn PodExecFunc) {
	s.execFunc = fn
}

// ExecOnPrimary runs command in the postgres container of the CNPG primary pod
// (namespace data). It is the adapter the tenant's data clone runs its
// pg_dump, psql and catalog reads through.
func (s *Service) ExecOnPrimary(ctx context.Context, primary string, command ...string) (string, error) {
	if fn := s.podExec(); fn != nil {
		kubeconfig := s.kubeconfigPath()
		if kubeconfig == "" {
			home, _ := os.UserHomeDir()
			kubeconfig = filepath.Join(home, ".kube", "bifrost-k3s.yaml")
		}
		return fn(ctx, kubeconfig, cnpgNamespace, primary, "postgres", command...)
	}
	// Prefer client-go: in-cluster platform-api image has no kubectl binary.
	out, err := s.execViaAPI(ctx, cnpgNamespace, primary, "postgres", command...)
	if err == nil {
		return out, nil
	}
	// Local escape hatch when REST exec is unavailable but kubectl is on PATH.
	if _, lookErr := exec.LookPath("kubectl"); lookErr == nil {
		kubeconfig := s.kubeconfigPath()
		if kubeconfig == "" {
			home, _ := os.UserHomeDir()
			kubeconfig = filepath.Join(home, ".kube", "bifrost-k3s.yaml")
		}
		fallback, kerr := defaultKubectlExec(ctx, kubeconfig, cnpgNamespace, primary, "postgres", command...)
		if kerr == nil {
			return fallback, nil
		}
		return "", fmt.Errorf("%w (kubectl fallback: %v)", err, kerr)
	}
	return "", err
}

// ResolveCNPGPrimary names the pod of the CNPG cluster's current primary
// (status.currentPrimary of the Cluster resource), the pod ExecOnPrimary runs
// in.
func (s *Service) ResolveCNPGPrimary(ctx context.Context) (string, error) {
	dyn, err := s.buildDynamicClient()
	if err != nil {
		return "", fmt.Errorf("dynamic client: %w", err)
	}
	obj, err := dyn.Resource(cnpgClusterGVR).Namespace(cnpgNamespace).Get(ctx, cnpgClusterName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get CNPG cluster: %w", err)
	}
	primary := strings.TrimSpace(stringFromUnstructured(obj, "status", "currentPrimary"))
	if primary == "" {
		return "", fmt.Errorf("CNPG cluster %s has no currentPrimary", cnpgClusterName)
	}
	return primary, nil
}

func (s *Service) execViaAPI(ctx context.Context, namespace, pod, container string, command ...string) (string, error) {
	if s == nil {
		return "", fmt.Errorf("cluster service unavailable")
	}
	cfg, _, err := s.RestConfig()
	if err != nil {
		return "", err
	}
	clientset, _, err := s.buildClient()
	if err != nil {
		return "", err
	}
	req := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(pod).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   append([]string{}, command...),
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(cfg, "POST", req.URL())
	if err != nil {
		return "", fmt.Errorf("pod exec executor %s/%s: %w", namespace, pod, err)
	}
	var stdout, stderr bytes.Buffer
	if err := executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	}); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("pod exec %s/%s: %s", namespace, pod, msg)
	}
	return stdout.String(), nil
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
