package landing

import (
	"testing"
	"time"
)

func drillState(t *testing.T) State {
	rs := records()[:4]
	rs[1]["dry_run"] = true
	ref := "refs/heads/land/stg/" + task1 + "/1"
	rs[2]["ref"], rs[3]["ref"] = ref, ref
	return mustFold(t, rs)
}
func observation() DrillObservation {
	f := eligibleFacts()
	f.Environment = "stg"
	f.Mode = "dry_run"
	return DrillObservation{Facts: f, Main: M0, Candidate: CIRun{Run: "ci-new", R: R, Status: "Succeeded", CreatedAt: epoch.Add(2 * time.Minute)}, MainCI: CIRun{R: M0, Status: "Succeeded"}}
}
func TestDrillMainMovementPrecedesOldMainRed(t *testing.T) {
	s := drillState(t)
	if err := s.Apply(Entry{At: epoch.Add(4 * time.Minute), Record: record("parked", "why", "main_red", "retry_at", epoch.Add(5*time.Minute).Format(time.RFC3339))}); err != nil {
		t.Fatal(err)
	}
	obs := observation()
	obs.Main = A
	obs.Candidate.Status = "Failed"
	obs.MainCI.Status = "Failed"
	if a := NextDrill(s, "land:1", obs, epoch.Add(10*time.Minute)); a.Kind != "replay" {
		t.Fatal("stuck waiting on old red main", a)
	}
}
func TestDrillLatestCIFailureCannotRelease(t *testing.T) {
	s := drillState(t)
	obs := observation()
	obs.Candidate = LatestCI([]CIRun{
		{Run: "old-green", R: R, Status: "Succeeded", CreatedAt: epoch.Add(2 * time.Minute)},
		{Run: "new-red", R: R, Status: "Failed", CreatedAt: epoch.Add(3 * time.Minute)},
		{Run: "other-result", R: A, Status: "Succeeded", CreatedAt: epoch.Add(4 * time.Minute)},
	}, R, epoch.Add(time.Minute))
	if obs.Candidate.Run != "new-red" {
		t.Fatal(obs.Candidate)
	}
	a := NextDrill(s, "land:1", obs, epoch.Add(5*time.Minute))
	if a.Kind != "ci_rerun" {
		t.Fatal("old green admitted", a)
	}
	o := s.Operations["land:1"]
	o.Rerun = true
	s.Operations["land:1"] = o
	a = NextDrill(s, "land:1", obs, epoch.Add(5*time.Minute))
	if a.Kind != "failed" || a.Why != "ci_red" {
		t.Fatal(a)
	}
}
func TestDrillCIStatesAndWindowBudget(t *testing.T) {
	for _, status := range []string{"Missing", "Unknown", "Running", "Failed", "Succeeded"} {
		s := drillState(t)
		obs := observation()
		obs.Candidate.Status = status
		obs.MainCI.Status = "Unknown"
		a := NextDrill(s, "land:1", obs, epoch.Add(12*time.Minute))
		expected := "wait"
		if status == "Missing" {
			expected = "ci_kick"
		}
		if status == "Succeeded" {
			expected = "ready"
		}
		if a.Kind != expected {
			t.Fatal(status, a)
		}
	}
	s := drillState(t)
	obs := observation()
	obs.WindowBlocked = true
	if a := NextDrill(s, "land:1", obs, epoch.Add(time.Hour)); a.Kind != "parked" || a.Why != "window" {
		t.Fatal(a)
	}
	if a := NextDrill(s, "land:1", obs, epoch.Add(6*time.Hour)); a.Kind != "failed" || a.Why != "bound" {
		t.Fatal(a)
	}
	obs.Candidate.Status = "Running"
	if a := NextDrill(s, "land:1", obs, epoch.Add(40*time.Minute)); a.Kind != "parked" || a.Why != "ci_stuck" {
		t.Fatal(a)
	}
	o := s.Operations["land:1"]
	o.Kicked = true
	s.Operations["land:1"] = o
	obs.Candidate.Status = "Missing"
	if a := NextDrill(s, "land:1", obs, epoch.Add(time.Hour)); a.Kind != "wait" {
		t.Fatal("missing kicked twice", a)
	}
}
func TestLatestCIFences(t *testing.T) {
	for _, status := range []string{"Running", "Unknown", "Failed"} {
		r := LatestCI([]CIRun{{Run: "old", R: R, Status: "Succeeded", CreatedAt: epoch}, {Run: "new", R: R, Status: status, CreatedAt: epoch.Add(time.Minute)}}, R, time.Time{})
		if r.Status != status {
			t.Fatal(r)
		}
	}
	if r := LatestCI([]CIRun{{R: R, Status: "Succeeded", CreatedAt: epoch}}, R, epoch); r.Status != "Missing" {
		t.Fatal("preparation fence ignored", r)
	}
	tied := LatestCI([]CIRun{{Run: "a", R: R, Status: "Succeeded", CreatedAt: epoch}, {Run: "b", R: R, Status: "Failed", CreatedAt: epoch}}, R, time.Time{})
	if tied.Status != "Unknown" {
		t.Fatal("ambiguous ordering admitted", tied)
	}
}

func TestDrillUnknownWithoutEvidenceDoesNotKick(t *testing.T) {
	s := drillState(t)
	obs := observation()
	obs.Candidate = CIRun{Status: "Unknown"}
	if a := NextDrill(s, "land:1", obs, epoch.Add(time.Hour)); a.Kind != "wait" {
		t.Fatal("unknown became missing", a)
	}
	o := s.Operations["land:1"]
	o.Rerun = true
	o.RerunAt = epoch.Add(10 * time.Minute)
	s.Operations["land:1"] = o
	obs.Candidate = CIRun{R: R, Run: "old", Status: "Failed", CreatedAt: epoch.Add(2 * time.Minute)}
	if a := NextDrill(s, "land:1", obs, epoch.Add(time.Hour)); a.Kind != "wait" {
		t.Fatal("old failure consumed new retry", a)
	}
	obs.Candidate.Status = "Succeeded"
	if a := NextDrill(s, "land:1", obs, epoch.Add(time.Hour)); a.Kind != "wait" {
		t.Fatal("old success consumed new retry", a)
	}
}
