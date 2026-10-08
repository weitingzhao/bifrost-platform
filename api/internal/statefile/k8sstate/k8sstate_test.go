package k8sstate_test

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/statefile"
	"github.com/weitingzhao/bifrost-platform/api/internal/statefile/k8sstate"
)

const ns = "bifrost-platform-prod"

func newBackend(t *testing.T) (*k8sstate.Backend, *fake.Clientset) {
	t.Helper()
	cs := fake.NewSimpleClientset()
	return k8sstate.New(ns, func() (kubernetes.Interface, error) { return cs, nil }), cs
}

func TestReadWriteOneConfigMapPerKey(t *testing.T) {
	b, cs := newBackend(t)
	ctx := context.Background()
	if _, err := b.Read(ctx, "checklist/signals.json"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing key: %v, want fs.ErrNotExist", err)
	}
	if err := b.Write(ctx, "checklist/signals.json", []byte("v1")); err != nil {
		t.Fatal(err)
	}
	if err := b.Write(ctx, "checklist/signals.json", []byte("v2")); err != nil {
		t.Fatal(err)
	}
	got, err := b.Read(ctx, "checklist/signals.json")
	if err != nil || string(got) != "v2" {
		t.Fatalf("read %q %v", got, err)
	}
	cm, err := cs.CoreV1().ConfigMaps(ns).Get(ctx, "platform-state-checklist-signals-json", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("ConfigMap name: %v", err)
	}
	if cm.Labels["bifrost.io/platform-state"] != "true" || cm.Annotations["bifrost.io/state-key"] != "checklist/signals.json" {
		t.Fatalf("labels %v annotations %v", cm.Labels, cm.Annotations)
	}
	if err := b.Write(ctx, "big.json", []byte(strings.Repeat("x", k8sstate.MaxBytes+1))); err == nil {
		t.Fatal("a value over the ConfigMap budget must be refused, not truncated")
	}
}

// Two pods (api, workers) and a rollout share state through the backend.
func withBackend(t *testing.T) string {
	t.Helper()
	b, _ := newBackend(t)
	dataDir := filepath.Join(t.TempDir(), "data")
	t.Setenv("PLATFORM_DATA_DIR", dataDir)
	statefile.Use(b, dataDir)
	t.Cleanup(func() { statefile.Use(nil, "") })
	return dataDir
}

func TestDataCloneScheduleSetOnAPIReachesWorkers(t *testing.T) {
	withBackend(t)
	api := cluster.NewDataCloneScheduleStore()
	workers := cluster.NewDataCloneScheduleStore() // built first, then the api pod writes
	cfg := api.Get()
	cfg.Enabled, cfg.Interval = true, "daily"
	api.Put(cfg)
	if got := workers.Get(); !got.Enabled || got.Interval != "daily" {
		t.Fatalf("workers see %+v, want the schedule the api pod stored", got)
	}
}

func TestAuditSurvivesRestartAndMergesTheWorkersRecords(t *testing.T) {
	dataDir := withBackend(t)
	apiPath := filepath.Join(dataDir, "audit", "audit-api.json")
	workersPath := filepath.Join(dataDir, "audit", "audit-workers.json")

	actuation.NewAuditLog(apiPath).RecordDirect("operator", actuation.RoleOperator, "delivery.pipeline.run", "run-1", "ok", "")
	actuation.NewAuditLog(workersPath).RecordDirect("platform-auto-repair", actuation.RoleOperator, "ib-gateway.auto_reconnect", "data/ib-gateway", "ok", "")

	api := actuation.NewAuditLog(apiPath) // the api pod after a rollout
	api.AlsoList(workersPath)
	rec := httptest.NewRecorder()
	api.HandleList(rec, httptest.NewRequest(http.MethodGet, "/api/v1/audit", nil))
	var body struct {
		Records []actuation.AuditRecord `json:"records"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	actions := map[string]bool{}
	for _, r := range body.Records {
		actions[r.Action] = true
	}
	if !actions["delivery.pipeline.run"] || !actions["ib-gateway.auto_reconnect"] {
		t.Fatalf("audit after rollout lists %v, want the api record and the workers record", actions)
	}
}
