package buildgate

import (
	"path/filepath"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/opscontext"
)

func newTestConfig(t *testing.T, tasks []opscontext.TrackTask) *config.Config {
	t.Helper()
	dir := t.TempDir()
	return &config.Config{
		ConfigPath: filepath.Join(dir, "config", "environments.yaml"),
		OpsContext: &opscontext.File{
			Tracks: &opscontext.Tracks{
				Build: &opscontext.BuildTrack{Label: "Build", Tasks: tasks},
			},
		},
	}
}

func TestGetGateReadyWhenAllTasksDone(t *testing.T) {
	cfg := newTestConfig(t, []opscontext.TrackTask{
		{ID: "p1-1", Label: "Task 1", Status: "done"},
		{ID: "p1-2", Label: "Task 2", Status: "done"},
	})
	svc := NewService(cfg)

	gate := svc.GetGate("P1")
	if !gate.Ready {
		t.Fatalf("gate.Ready = false, want true: %+v", gate)
	}
	if gate.Result != "pass" {
		t.Fatalf("gate.Result = %q, want pass", gate.Result)
	}
	if gate.TotalTasks != 2 || gate.DoneTasks != 2 {
		t.Fatalf("gate tasks = %d/%d, want 2/2", gate.DoneTasks, gate.TotalTasks)
	}
	if len(gate.Blockers) != 0 {
		t.Fatalf("gate.Blockers = %+v, want none", gate.Blockers)
	}
}

func TestGetGateNotReadyWithBlockedTask(t *testing.T) {
	cfg := newTestConfig(t, []opscontext.TrackTask{
		{ID: "p2-1", Label: "Task 1", Status: "done"},
		{ID: "p2-2", Label: "Task 2", Status: "blocked"},
	})
	svc := NewService(cfg)

	gate := svc.GetGate("P2")
	if gate.Ready {
		t.Fatal("gate.Ready = true, want false due to blocked task")
	}
	if gate.Result != "incomplete" {
		t.Fatalf("gate.Result = %q, want incomplete", gate.Result)
	}
	if len(gate.Blockers) != 1 {
		t.Fatalf("gate.Blockers = %+v, want 1 entry", gate.Blockers)
	}
}

func TestGetGateNoTasksIsNoTasksResult(t *testing.T) {
	cfg := newTestConfig(t, nil)
	svc := NewService(cfg)

	gate := svc.GetGate("P1")
	if gate.Ready {
		t.Fatal("gate.Ready = true, want false with no tasks")
	}
	if gate.Result != "no_tasks" {
		t.Fatalf("gate.Result = %q, want no_tasks", gate.Result)
	}
}

func TestGetGateUnknownPhasePrefixHasNoTasks(t *testing.T) {
	cfg := newTestConfig(t, []opscontext.TrackTask{{ID: "p1-1", Label: "Task 1", Status: "done"}})
	svc := NewService(cfg)

	gate := svc.GetGate("P9")
	if gate.TotalTasks != 0 || gate.Result != "no_tasks" {
		t.Fatalf("gate = %+v, want empty/no_tasks for unmapped phase", gate)
	}
}

func TestRunGatePersistsRecordVisibleOnNextGetGate(t *testing.T) {
	cfg := newTestConfig(t, []opscontext.TrackTask{{ID: "p1-1", Label: "Task 1", Status: "done"}})
	svc := NewService(cfg)

	resp, err := svc.RunGate("P1", "tester")
	if err != nil {
		t.Fatalf("RunGate() error = %v", err)
	}
	if !resp.OK {
		t.Fatalf("RunGate().OK = false, want true: %+v", resp)
	}
	if resp.Action != "buildgate.p1-gate" || resp.Target != "build-phase-p1" {
		t.Fatalf("RunGate() action/target = %q/%q", resp.Action, resp.Target)
	}

	gate := svc.GetGate("P1")
	if gate.LastRunAt == nil || gate.LastRunResult != "pass" {
		t.Fatalf("GetGate() after RunGate = %+v, want LastRunAt/LastRunResult populated", gate)
	}
}

func TestSignoffFailsWhenGateNotReady(t *testing.T) {
	cfg := newTestConfig(t, []opscontext.TrackTask{{ID: "p1-1", Label: "Task 1", Status: "blocked"}})
	svc := NewService(cfg)

	if _, err := svc.Signoff("P1", "notes", "owner"); err == nil {
		t.Fatal("Signoff() error = nil, want error when gate not ready")
	}
}

func TestSignoffSucceedsWhenGateReady(t *testing.T) {
	cfg := newTestConfig(t, []opscontext.TrackTask{{ID: "p1-1", Label: "Task 1", Status: "done"}})
	svc := NewService(cfg)

	resp, err := svc.Signoff("P1", "  looks good  ", "owner")
	if err != nil {
		t.Fatalf("Signoff() error = %v", err)
	}
	if !resp.OK {
		t.Fatalf("Signoff().OK = false, want true: %+v", resp)
	}

	gate := svc.GetGate("P1")
	if gate.SignedAt == nil || gate.SignedBy != "owner" {
		t.Fatalf("GetGate() after Signoff = %+v, want SignedAt/SignedBy populated", gate)
	}
}

func TestListPhasesOnlyIncludesPhasesWithTasks(t *testing.T) {
	cfg := newTestConfig(t, []opscontext.TrackTask{
		{ID: "p1-1", Label: "Task 1", Status: "done"},
		{ID: "p3-1", Label: "Task 1", Status: "in_progress"},
	})
	svc := NewService(cfg)

	phases := svc.ListPhases()
	if len(phases) != 2 {
		t.Fatalf("ListPhases() len = %d, want 2 (P1, P3 only), got %+v", len(phases), phases)
	}
	seen := map[string]bool{}
	for _, p := range phases {
		seen[p.Phase] = true
	}
	if !seen["P1"] || !seen["P3"] {
		t.Fatalf("ListPhases() phases = %+v, want P1 and P3", phases)
	}
}

func TestServiceWithNilConfigHasNoTasks(t *testing.T) {
	svc := NewService(nil)
	gate := svc.GetGate("P1")
	if gate.TotalTasks != 0 || gate.Result != "no_tasks" {
		t.Fatalf("gate = %+v, want empty for nil config", gate)
	}
}
