package landingci

import (
	"context"
	"errors"
	"testing"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

// Origin: 2026-11-18, decoupling item 5: this moved out of api/internal/server,
// where nothing tested it; the log comes through the client handed in, and a client that cannot
// be had is the read's error.
func TestPodLogsReadsThroughTheClient(t *testing.T) {
	logs := PodLogs(func() (kubernetes.Interface, error) { return fake.NewSimpleClientset(), nil }, DefaultNamespace)
	if got, err := logs(context.Background(), "pod", "step"); err != nil || got == "" {
		t.Fatalf("got %q, %v", got, err)
	}
	boom := errors.New("no cluster")
	logs = PodLogs(func() (kubernetes.Interface, error) { return nil, boom }, DefaultNamespace)
	if _, err := logs(context.Background(), "pod", "step"); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}
