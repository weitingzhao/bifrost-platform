package landing

import (
	"testing"
	"time"
)

// Grant CI is kicked once; failed evidence ends the operation immediately.
func TestC5PinnedCIComesFirst(t *testing.T) {
	t.Parallel()
	f := eligibleFacts()
	f.GrantEnabled = true
	f.Changes[0].OldPath = "api/internal/agentrt/store/example.go"
	f.Changes[0].NewPath = f.Changes[0].OldPath
	s := mustFold(t, phaseRecords("green", false))
	o := s.Operations["land:1"]
	at := epoch.Add(time.Hour)
	for _, status := range []string{"Succeeded", "Failed"} {
		ci := CIRun{R: o.R, Run: "ci-platform-webhook", Status: status, CreatedAt: o.PreparedAt.Add(time.Second)}
		obs := DrillObservation{Facts: f, Main: o.M0, Candidate: ci, PinCI: true}
		if d := NextDrill(s, "land:1", obs, at); d.Kind != map[string]string{"Succeeded": "ci_kick", "Failed": "failed"}[status] {
			t.Errorf("webhook %s, not kicked: %+v", status, d)
		}
		kicked := s
		k := o
		k.Kicked = true
		kicked.Operations = map[string]Operation{"land:1": k}
		if d := NextDrill(kicked, "land:1", obs, at); d.Kind != map[string]string{"Succeeded": "wait", "Failed": "failed"}[status] {
			t.Errorf("webhook %s, kick used: %+v", status, d)
		}
		obs.PinCI = false
		want := map[string]string{"Succeeded": "ready", "Failed": "failed"}[status]
		if d := NextDrill(s, "land:1", obs, at); d.Kind != want {
			t.Errorf("control %s: %+v, want %s", status, d, want)
		}
	}
}
