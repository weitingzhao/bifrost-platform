package landing

import (
	"testing"
	"time"
)

// missingObservation: a platform operation that sees no run of R yet.
func missingObservation() DrillObservation {
	obs := observation()
	obs.Candidate = CIRun{Status: "Missing"}
	return obs
}

// infraMissingObservation: the same for an infra operation.
func infraMissingObservation() DrillObservation {
	obs := missingObservation()
	f := infraFacts()
	f.Environment, f.Mode = obs.Facts.Environment, obs.Facts.Mode
	f.DeployChanges = f.Changes
	obs.Facts = f
	return obs
}

// R5:5.7-5: the platform trigger leaves refs/heads/land/ out, so a platform
// operation that sees no run of R makes its own 30 seconds after PreparedAt,
// not 10 minutes. Real event order, PreparedAt on a whole second or not, a
// restart after the kick's intent: the run it makes (creation time in whole
// seconds) is read, makes the operation ready, and the protocol takes its
// ci_seen and rehearsed (outside review of R5:5.7-5, rounds 2 and 3).
func TestDrillMissingCIPlatformKicksAfter30sAndItsRunCounts(t *testing.T) {
	for _, fractional := range []time.Duration{0, 100 * time.Millisecond, 999 * time.Millisecond} {
		for _, restart := range []bool{false, true} {
			prepared := epoch.Add(time.Minute + fractional)
			rs := records()[:4]
			rs[1]["dry_run"] = true
			ref := "refs/heads/land/stg/" + task1 + "/1"
			rs[2]["ref"], rs[3]["ref"] = ref, ref
			es := []Entry{
				{At: prepared.Add(-time.Second), Record: rs[0]},
				{At: prepared, Record: rs[1]},
				{At: prepared.Add(10 * time.Millisecond), Record: rs[2]},
				{At: prepared.Add(20 * time.Millisecond), Record: rs[3]},
			}
			s, err := Fold(es)
			if err != nil {
				t.Fatal(err)
			}
			for _, delay := range []time.Duration{100 * time.Millisecond, time.Second, 30*time.Second - time.Nanosecond} {
				if a := NextDrill(s, "land:1", missingObservation(), prepared.Add(delay)); a.Kind != "wait" {
					t.Fatalf("fractional=%s at=%s before 30s: %+v", fractional, delay, a)
				}
			}
			kickAt := prepared.Add(30 * time.Second)
			if a := NextDrill(s, "land:1", missingObservation(), kickAt); a.Kind != "ci_kick" {
				t.Fatalf("fractional=%s exact 30s: %+v", fractional, a)
			}
			kick := Entry{At: kickAt, Record: v11("ci_kick", "try", 1)}
			if err = s.Apply(kick); err != nil {
				t.Fatal(err)
			}
			es = append(es, kick)
			created := kickAt.Add(100 * time.Millisecond).Truncate(time.Second)
			if restart {
				s, err = Fold(es)
				if err != nil {
					t.Fatal(err)
				}
				recoveryAt := kickAt.Add(5 * time.Second)
				if a := NextDrill(s, "land:1", missingObservation(), recoveryAt); a.Kind != "wait" {
					t.Fatalf("recovered intent kicked again: %+v", a)
				}
				// The caller ensures the persisted CIName(op, 1) after a crash.
				created = recoveryAt.Add(100 * time.Millisecond).Truncate(time.Second)
			}
			run := CIRun{Run: CIName("land:1", 1), R: R, Status: "Succeeded", CreatedAt: created}
			obs := observation()
			obs.Candidate = LatestCI([]CIRun{run}, R, prepared)
			if obs.Candidate.Run != run.Run || obs.Candidate.Status != "Succeeded" {
				t.Fatalf("fractional=%s restart=%t unreadable: %+v", fractional, restart, obs.Candidate)
			}
			doneAt := kickAt.Add(5 * time.Minute)
			if a := NextDrill(s, "land:1", obs, doneAt); a.Kind != "ready" {
				t.Fatalf("fractional=%s restart=%t not ready: %+v", fractional, restart, a)
			}
			if err = s.Apply(Entry{At: doneAt, Record: Record(CIRecord("land:1", obs.Candidate))}); err != nil {
				t.Fatalf("ci_seen rejected: %v", err)
			}
			if err = s.Apply(Entry{At: doneAt, Record: v11("rehearsed", "R", R)}); err != nil {
				t.Fatalf("rehearsed rejected: %v", err)
			}
		}
	}
}

// Infra keeps its 10 minutes, and does not kick twice.
func TestDrillMissingCIInfraKeepsTenMinutes(t *testing.T) {
	s := drillState(t)
	prepared := s.Operations["land:1"].PreparedAt
	for _, delay := range []time.Duration{30 * time.Second, 10*time.Minute - time.Nanosecond} {
		if a := NextDrill(s, "land:1", infraMissingObservation(), prepared.Add(delay)); a.Kind != "wait" {
			t.Fatalf("infra before ten minutes at=%s: %+v", delay, a)
		}
	}
	at := prepared.Add(10 * time.Minute)
	if a := NextDrill(s, "land:1", infraMissingObservation(), at); a.Kind != "ci_kick" {
		t.Fatalf("infra ten-minute boundary: %+v", a)
	}
	if err := s.Apply(Entry{At: at, Record: v11("ci_kick", "try", 1)}); err != nil {
		t.Fatal(err)
	}
	if a := NextDrill(s, "land:1", infraMissingObservation(), at.Add(time.Minute)); a.Kind != "wait" {
		t.Fatalf("infra kicked twice: %+v", a)
	}
}

// An operation that already kicked does not kick again; one that reran
// without a kick (Kicked false, Rerun true, a legal folded state) waits for
// its try 2 and never makes a later try 1 that would hide it (review of
// R5:5.7-5 round 1).
func TestDrillMissingCINeverKicksAfterAKickOrARerun(t *testing.T) {
	for _, kind := range []string{"ci_kick", "ci_rerun"} {
		t.Run(kind, func(t *testing.T) {
			s := drillState(t)
			at := s.Operations["land:1"].PreparedAt.Add(time.Second)
			try := 1
			if kind == "ci_rerun" {
				try = 2
			}
			if err := s.Apply(Entry{At: at, Record: v11(kind, "try", try)}); err != nil {
				t.Fatal(err)
			}
			o := s.Operations["land:1"]
			if kind == "ci_rerun" && (o.Kicked || !o.Rerun) {
				t.Fatal("fold", o.Kicked, o.Rerun)
			}
			if a := NextDrill(s, "land:1", missingObservation(), at.Add(time.Hour)); a.Kind == "ci_kick" {
				t.Fatal("kicked again", a)
			}
		})
	}
}
