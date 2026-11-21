package cluster

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

func TestScaleDaemonFromZeroBlockedByD10(t *testing.T) {
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "daemon", Namespace: "bifrost-stg"},
		Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(0)},
	}
	clientset := fake.NewSimpleClientset(deploy)
	svc := NewService(nil)
	svc.clientFactory = func() (kubernetes.Interface, string, error) {
		return clientset, "fake", nil
	}

	resp, err := svc.Scale(t.Context(), ScaleRequest{
		Namespace: "bifrost-stg",
		Kind:      "Deployment",
		Name:      "daemon",
		Replicas:  2,
	})
	if err == nil {
		t.Fatal("expected D10 error")
	}
	if resp.OK {
		t.Fatalf("expected ok=false, got %+v", resp)
	}
	if !strings.Contains(resp.Message, "BLOCKED (D10)") {
		t.Fatalf("message=%q", resp.Message)
	}
}

func TestScaleDaemonDownAllowed(t *testing.T) {
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "daemon", Namespace: "bifrost-prod"},
		Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(2)},
	}
	clientset := fake.NewSimpleClientset(deploy)
	svc := NewService(nil)
	svc.clientFactory = func() (kubernetes.Interface, string, error) {
		return clientset, "fake", nil
	}

	resp, err := svc.Scale(t.Context(), ScaleRequest{
		Namespace: "bifrost-prod",
		Kind:      "Deployment",
		Name:      "daemon",
		Replicas:  0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK || !resp.Changed {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

// TestScaleDaemonUpFromNonZeroBlocked is the TD-222 ratchet: PROD daemon
// already runs at 2, and 2→3 must be refused while D10 is not UNLOCKED.
func TestScaleDaemonUpFromNonZeroBlocked(t *testing.T) {
	resp, err := scaleDaemon(t, "bifrost-prod", 2, 3, "")
	if err == nil || resp.OK || !strings.Contains(resp.Message, "BLOCKED (D10)") {
		t.Fatalf("2→3 in bifrost-prod must be blocked, err=%v resp=%+v", err, resp)
	}
}

// TestScaleDaemonReplicaChanges covers 0→1, 1→2 and 2→3 in every Trade
// namespace, and allows scale-down. The spine is unread (fail closed).
func TestScaleDaemonReplicaChanges(t *testing.T) {
	cases := []struct {
		ns      string
		from    int32
		to      int32
		wantErr bool
	}{
		{"bifrost-dev", 0, 1, true},
		{"bifrost-stg", 0, 1, true},
		{"bifrost-prod", 0, 1, true},
		{"bifrost-dev", 1, 2, true},
		{"bifrost-stg", 1, 2, true},
		{"bifrost-prod", 1, 2, true},
		{"bifrost-dev", 2, 3, true},
		{"bifrost-stg", 2, 3, true},
		{"bifrost-prod", 2, 3, true},
		{"bifrost-dev", 1, 0, false},
		{"bifrost-stg", 2, 0, false},
		{"bifrost-prod", 2, 1, false},
		{"bifrost-prod", 3, 2, false},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s_%d_to_%d", tc.ns, tc.from, tc.to), func(t *testing.T) {
			resp, err := scaleDaemon(t, tc.ns, tc.from, tc.to, "")
			if tc.wantErr {
				if err == nil || resp.OK || !strings.Contains(resp.Message, "BLOCKED (D10)") {
					t.Fatalf("expected D10 block, err=%v resp=%+v", err, resp)
				}
				return
			}
			if err != nil || !resp.OK || !resp.Changed {
				t.Fatalf("scale-down must be allowed, err=%v resp=%+v", err, resp)
			}
		})
	}
}

func TestScaleDaemonUpAllowedWhenD10Unlocked(t *testing.T) {
	path := writeSpine(t, "UNLOCKED")
	for _, tc := range []struct{ from, to int32 }{{0, 1}, {1, 2}, {2, 3}} {
		resp, err := scaleDaemon(t, "bifrost-prod", tc.from, tc.to, path)
		if err != nil || !resp.OK || !resp.Changed {
			t.Fatalf("%d→%d with D10 UNLOCKED: err=%v resp=%+v", tc.from, tc.to, err, resp)
		}
	}
}

func TestScaleDaemonUpBlockedWhenSpineMissingD10(t *testing.T) {
	path := writeSpine(t, "")
	resp, err := scaleDaemon(t, "bifrost-dev", 1, 2, path)
	if err == nil || resp.OK {
		t.Fatalf("missing D10 must fail closed, resp=%+v", resp)
	}
}

func TestScaleDaemonDownAllowedWhenSpineUnreadable(t *testing.T) {
	resp, err := scaleDaemon(t, "bifrost-prod", 2, 0, filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil || !resp.OK || !resp.Changed {
		t.Fatalf("scale-down must not depend on the spine, err=%v resp=%+v", err, resp)
	}
}

func scaleDaemon(t *testing.T, namespace string, from, to int32, spinePath string) (ActuationResponse, error) {
	t.Helper()
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "daemon", Namespace: namespace},
		Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(from)},
	}
	clientset := fake.NewSimpleClientset(deploy)
	svc := NewService(nil)
	svc.clientFactory = func() (kubernetes.Interface, string, error) {
		return clientset, "fake", nil
	}
	if spinePath != "" {
		svc.SetOpsContextPath(spinePath)
	}
	return svc.Scale(t.Context(), ScaleRequest{
		Namespace: namespace,
		Kind:      "Deployment",
		Name:      "daemon",
		Replicas:  to,
	})
}

// writeSpine writes a minimal ops-context.yaml. An empty d10Status omits D10.
func writeSpine(t *testing.T, d10Status string) string {
	t.Helper()
	decisions := ""
	if d10Status != "" {
		decisions = fmt.Sprintf("  - id: D10\n    status: %s\n    conclusion: test\n", d10Status)
	}
	body := fmt.Sprintf(`meta:
  version: "1"
  catalog_version: "test"
deployment:
  phase: test
focus:
  headline: test focus
milestones:
  - id: m
    status: OPEN
decisions:
%s`, decisions)
	path := filepath.Join(t.TempDir(), "ops-context.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestScaleAccountSyncAllowedFromZero(t *testing.T) {
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "account-sync", Namespace: "bifrost-prod"},
		Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(0)},
	}
	clientset := fake.NewSimpleClientset(deploy)
	svc := NewService(nil)
	svc.clientFactory = func() (kubernetes.Interface, string, error) {
		return clientset, "fake", nil
	}

	resp, err := svc.Scale(t.Context(), ScaleRequest{
		Namespace: "bifrost-prod",
		Kind:      "Deployment",
		Name:      "account-sync",
		Replicas:  1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK || !resp.Changed {
		t.Fatalf("unexpected response: %+v", resp)
	}
}
