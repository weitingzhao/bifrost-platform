package delivery

import "testing"

func TestAggregatePhaseStatus(t *testing.T) {
	tasks := []string{"a", "b", "c"}
	m := map[string]string{"a": "succeeded", "b": "succeeded", "c": "succeeded"}
	st, detail := aggregatePhaseStatus(tasks, m)
	if st != "succeeded" || detail != "3/3 done" {
		t.Fatalf("all succeeded: got %s %s", st, detail)
	}

	m = map[string]string{"a": "succeeded", "b": "running", "c": "pending"}
	st, _ = aggregatePhaseStatus(tasks, m)
	if st != "running" {
		t.Fatalf("partial: got %s", st)
	}

	m = map[string]string{"a": "failed", "b": "succeeded"}
	st, _ = aggregatePhaseStatus([]string{"a", "b"}, m)
	if st != "failed" {
		t.Fatalf("failed: got %s", st)
	}

	m = map[string]string{}
	st, _ = aggregatePhaseStatus(tasks, m)
	if st != "pending" {
		t.Fatalf("pending: got %s", st)
	}
}

func TestAggregateDeliverStgPhasesBuildRunning(t *testing.T) {
	taskStatus := map[string]string{
		"clone-core": "succeeded", "clone-worker": "succeeded", "clone-socket": "succeeded",
		"clone-api": "succeeded", "clone-frontend": "succeeded", "clone-ui": "succeeded", "clone-infra": "succeeded",
		"prepare": "succeeded",
		"build-all-apis": "running",
	}
	phases := aggregateDeliverStgPhases(taskStatus)
	if len(phases) != 6 {
		t.Fatalf("expected 6 phases, got %d", len(phases))
	}
	if phases[0].Status != "succeeded" || phases[0].ID != "clone" {
		t.Fatalf("clone: %+v", phases[0])
	}
	if phases[2].Status != "running" || phases[2].ID != "build" {
		t.Fatalf("build: %+v", phases[2])
	}
	if phases[3].Status != "pending" {
		t.Fatalf("rollout should be pending: %+v", phases[3])
	}
}
