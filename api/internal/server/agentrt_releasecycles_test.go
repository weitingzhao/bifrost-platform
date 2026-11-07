package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/promote"
)

func TestReleaseCyclesForAgentRuntime(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PLATFORM_RELEASE_CYCLES_DIR", dir)
	cycles := promote.NewCycleStore(dir)
	got, err := releaseCyclesForAgentRuntime(cycles)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("empty store: got %#v, err %v", got, err)
	}
	start := time.Date(2026, 11, 1, 1, 0, 0, 0, time.UTC)
	completed := start.Add(time.Minute)
	gate := completed.Add(time.Minute)
	records := []promote.ReleaseCycleRecord{
		{Lane: promote.ReleaseCycleLanePlatform, Revision: "released", TriggeredBy: "owner", StartedAt: start.Add(-time.Hour), Steps: []promote.CycleStepRecord{
			{Kind: promote.CycleStepStgGate, CompletedAt: &start, Result: promote.CycleStepResultPass},
			{Kind: promote.CycleStepProdDeploy, StartedAt: &start, CompletedAt: &completed, Result: promote.CycleStepResultSuccess},
			{Kind: promote.CycleStepProdGate, CompletedAt: &gate, Result: promote.CycleStepResultPass},
		}},
		{Lane: promote.ReleaseCycleLanePlatform, Revision: "pending"},
		{Lane: promote.ReleaseCycleLanePlatform, Revision: "failed-gate", TriggeredBy: "agent", Steps: []promote.CycleStepRecord{
			{Kind: promote.CycleStepProdDeploy, StartedAt: &start, CompletedAt: &completed, Result: promote.CycleStepResultSuccess},
			{Kind: promote.CycleStepProdGate, CompletedAt: &gate, Result: promote.CycleStepResultFail},
		}},
	}
	write := func(name string, records []promote.ReleaseCycleRecord) {
		t.Helper()
		data, err := json.Marshal(records)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("release_cycles_platform.json", records)
	write("release_cycles_trade.json", []promote.ReleaseCycleRecord{{Lane: promote.ReleaseCycleLaneTrade, Revision: "trade"}})
	got, err = releaseCyclesForAgentRuntime(cycles)
	if err != nil {
		t.Fatal(err)
	}
	want := []store.ReleaseCycle{
		{Revision: "released", TriggeredBy: "owner", ProdDeployStartedAt: start, ProdDeployCompletedAt: completed, ProdDeployResult: promote.CycleStepResultSuccess, ProdGatePassedAt: gate},
		{Revision: "pending"},
		{Revision: "failed-gate", TriggeredBy: "agent", ProdDeployStartedAt: start, ProdDeployCompletedAt: completed, ProdDeployResult: promote.CycleStepResultSuccess},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}
