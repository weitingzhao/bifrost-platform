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
	return DrillObservation{Facts: f, Main: M0, Candidate: CIRun{Run: "ci-new", R: R, Status: "Succeeded", CreatedAt: epoch.Add(2 * time.Minute)}}
}

// oldMainRedPark is drillState parked{main_red}, as a stream written
// before main_red was retired (2026-11-06) holds it: it still folds.
func oldMainRedPark(t *testing.T) State {
	s := drillState(t)
	if err := s.Apply(Entry{At: epoch.Add(4 * time.Minute), Record: record("parked", "why", "main_red", "retry_at", epoch.Add(5*time.Minute).Format(time.RFC3339))}); err != nil {
		t.Fatal(err)
	}
	return s
}
func TestDrillMainMovementPrecedesOldMainRed(t *testing.T) {
	s := oldMainRedPark(t)
	obs := observation()
	obs.Main = A
	obs.Candidate.Status = "Failed"
	if a := NextDrill(s, "land:1", obs, epoch.Add(10*time.Minute)); a.Kind != "replay" {
		t.Fatal("stuck waiting on old red main", a)
	}
}

// The landing CI tests main plus the change, so its own result decides
// (Owner 2026-11-06): a Failed run is rerun once, then the operation ends
// failed{ci_red} and frees the slot. Main's CI is no input at all: whether
// it Succeeded, Failed or never ran, the judgement is the same. Nothing is
// parked main_red, and nothing waits on another run.
func TestDrillFailedLandingCIEndsWhateverMainSays(t *testing.T) {
	for _, from := range []string{"branch_pushed", "parked main_red"} {
		s := drillState(t)
		if from != "branch_pushed" {
			s = oldMainRedPark(t)
		}
		obs := observation()
		obs.Candidate.Status = "Failed"
		at := epoch.Add(10 * time.Minute)
		a := NextDrill(s, "land:1", obs, at)
		if from != "branch_pushed" {
			if a.Kind != "unparked" {
				t.Fatal(from, "an old main_red park is not resumed", a)
			}
			if err := s.Apply(Entry{At: at, Record: record("unparked")}); err != nil {
				t.Fatal(err)
			}
			a = NextDrill(s, "land:1", obs, at)
		}
		if a.Kind != "ci_rerun" {
			t.Fatal(from, "a red landing CI is not rerun", a)
		}
		if err := s.Apply(Entry{At: at, Record: record("ci_rerun", "try", 2)}); err != nil {
			t.Fatal(err)
		}
		obs.Candidate = CIRun{Run: CIName("land:1", 2), R: R, Status: "Failed", CreatedAt: at.Add(time.Second)}
		for _, later := range []time.Duration{time.Minute, time.Hour} {
			if a = NextDrill(s, "land:1", obs, at.Add(later)); a.Kind != "failed" || a.Why != "ci_red" {
				t.Fatal(from, "a red rerun does not end the operation", later, a)
			}
		}
	}
}

// A Succeeded landing CI goes ready on a red main, and resumes a park an
// old red main left.
func TestDrillGreenLandingCIOnRedMainGoesReady(t *testing.T) {
	if a := NextDrill(drillState(t), "land:1", observation(), epoch.Add(10*time.Minute)); a.Kind != "ready" {
		t.Fatal("a green landing CI does not go ready", a)
	}
	s := oldMainRedPark(t)
	if a := NextDrill(s, "land:1", observation(), epoch.Add(10*time.Minute)); a.Kind != "unparked" {
		t.Fatal("an old main_red park is not resumed", a)
	}
	if err := s.Apply(Entry{At: epoch.Add(10 * time.Minute), Record: record("unparked")}); err != nil {
		t.Fatal(err)
	}
	if a := NextDrill(s, "land:1", observation(), epoch.Add(10*time.Minute)); a.Kind != "ready" {
		t.Fatal("a green landing CI on a red main does not go ready", a)
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
		a := NextDrill(s, "land:1", obs, epoch.Add(12*time.Minute))
		expected := "wait"
		if status == "Missing" {
			expected = "ci_kick"
		}
		if status == "Failed" {
			expected = "ci_rerun" // never a wait on main's CI
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
