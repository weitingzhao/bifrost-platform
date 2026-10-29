package landing

import (
	"testing"
	"time"
)

// Review 698 B-01: an operation that needs a grant runs its pinned CI
// first, whatever another run of R says: the push webhook's run already
// green (or red) does not make it ready; the kick comes first, or the
// rerun when the kick was used. Without PinCI the v1 decisions stand.
// (On 082125dd NextDrill returned ready / ci_rerun with no pinned run.)
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
		obs := DrillObservation{Facts: f, Main: o.M0, Candidate: ci, MainCI: CIRun{Status: "Succeeded"}, PinCI: true}
		if d := NextDrill(s, "land:1", obs, at); d.Kind != "ci_kick" {
			t.Errorf("webhook %s, not kicked: %+v", status, d)
		}
		kicked := s
		k := o
		k.Kicked = true
		kicked.Operations = map[string]Operation{"land:1": k}
		if d := NextDrill(kicked, "land:1", obs, at); d.Kind != "ci_rerun" {
			t.Errorf("webhook %s, kick used: %+v", status, d)
		}
		obs.PinCI = false
		want := map[string]string{"Succeeded": "ready", "Failed": "ci_rerun"}[status]
		if d := NextDrill(s, "land:1", obs, at); d.Kind != want {
			t.Errorf("control %s: %+v, want %s", status, d, want)
		}
	}
}
