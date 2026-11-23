package computenodes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

// Moved from cluster/node_power_test.go on 2026-11-19 with the code they test.

func TestIsComputeManaged(t *testing.T) {
	entry := &config.ClusterEntry{
		ComputeNodes: []config.ComputeNodeSpec{{Name: "gpu-server"}},
	}
	svc := cluster.NewService(entry)
	managed := New().ClusterPort().Managed
	if !managed(svc, "gpu-server") {
		t.Fatal("expected gpu-server managed")
	}
	if managed(svc, "other") {
		t.Fatal("expected other not managed")
	}
}

func TestNodePowerManagedNode(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gpu-server",
			Annotations: map[string]string{
				annoWolMAC:      "aa:bb:cc:dd:ee:ff",
				annoPowerPolicy: "on-demand",
			},
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{{
				Type:   corev1.NodeReady,
				Status: corev1.ConditionTrue,
			}},
		},
	}
	clientset := fake.NewSimpleClientset(node)

	entry := &config.ClusterEntry{
		ID: "test",
		ComputeNodes: []config.ComputeNodeSpec{{
			Name:   "gpu-server",
			WolMAC: "aa:bb:cc:dd:ee:ff",
			Workloads: []config.ComputeWorkloadSpec{
				{Namespace: "ai", Deployment: "ollama", Label: "Ollama"},
			},
		}},
	}
	svc := cluster.NewService(entry)
	svc.SetClientFactoryForTest(func() (kubernetes.Interface, string, error) {
		return clientset, "fake", nil
	})

	resp, err := NodePower(t.Context(), svc, "gpu-server")
	if err != nil {
		t.Fatal(err)
	}
	if !resp.ComputeManaged || resp.PowerState != "online" || resp.NodeStatus != "Ready" {
		t.Fatalf("unexpected power response: %+v", resp)
	}
	if resp.WolMAC != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("wol mac: %q", resp.WolMAC)
	}
	if resp.Reachability != probe.ReachOK {
		t.Fatalf("reachability: %s", resp.Reachability)
	}
}

func TestNodePowerUnknownNode(t *testing.T) {
	svc := cluster.NewService(&config.ClusterEntry{ComputeNodes: []config.ComputeNodeSpec{{Name: "gpu-server"}}})
	_, err := NodePower(t.Context(), svc, "worker-1")
	if err == nil {
		t.Fatal("expected error for unmanaged node")
	}
}

func TestPowerOffRestoresOriginalSchedulingOnSSHFailure(t *testing.T) {
	for _, tc := range []struct {
		name          string
		unschedulable bool
		wantCommands  string
	}{
		{name: "schedulable", wantCommands: "drain\nuncordon\n"},
		{name: "already cordoned", unschedulable: true, wantCommands: "drain\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			calls := filepath.Join(dir, "calls")
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("POWER_TEST_CALLS", calls)
			t.Setenv("GPU_POWEROFF_SSH_DIR", dir)
			for _, name := range []string{"id_ed25519", "known_hosts"} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte("test fixture"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			kubectl := "#!/bin/sh\nif [ \"$1\" = --kubeconfig ]; then shift 2; fi\nprintf '%s\\n' \"$1\" >> \"$POWER_TEST_CALLS\"\n"
			if err := os.WriteFile(filepath.Join(dir, "kubectl"), []byte(kubectl), 0o700); err != nil {
				t.Fatal(err)
			}
			ssh := `#!/bin/sh
for arg do last=$arg; done
if [ "$last" = check ]; then
  printf '%s\n' 'gpu-server: idle'
  exit 0
fi
printf '%s\n' 'test transport refused' >&2
exit 1
`
			if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(ssh), 0o700); err != nil {
				t.Fatal(err)
			}
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "gpu-server"},
				Spec:       corev1.NodeSpec{Unschedulable: tc.unschedulable},
				Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{
					Type: corev1.NodeReady, Status: corev1.ConditionTrue,
				}}},
			}
			svc := fakeService(&config.ClusterEntry{ComputeNodes: []config.ComputeNodeSpec{{
				Name: node.Name, SSHHost: "vision@192.0.2.4",
			}}}, node)
			resp, err := PowerOffNode(t.Context(), svc, node.Name)
			if err == nil || resp.OK || !strings.Contains(err.Error(), "test transport refused") {
				t.Fatalf("response=%+v, error=%v", resp, err)
			}
			log, err := os.ReadFile(calls)
			if err != nil {
				t.Fatal(err)
			}
			if string(log) != tc.wantCommands {
				t.Fatalf("kubectl calls = %q, want %q", log, tc.wantCommands)
			}
		})
	}
}

func TestGpuPowerOffRefusesWorkloadsBeforeUsingSSH(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: wakeGPUNode},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{
			Type: corev1.NodeReady, Status: corev1.ConditionTrue,
		}}},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "inference", Namespace: "tests"},
		Spec:       corev1.PodSpec{NodeName: node.Name},
	}
	svc := fakeService(&config.ClusterEntry{ComputeNodes: []config.ComputeNodeSpec{{Name: node.Name}}}, node, pod)
	if _, err := PowerOffNode(t.Context(), svc, node.Name); err == nil || !strings.Contains(err.Error(), "non-DaemonSet pods") {
		t.Fatalf("expected refusal before trying SSH, got %v", err)
	}
}
