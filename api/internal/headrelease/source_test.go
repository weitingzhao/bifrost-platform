package headrelease

import (
	"context"
	"testing"
)

// Origin: 2026-11-17, the PROD batch shadow (in server/prod_batch_shadow_test.go
// until 2026-11-23, when building the shadow moved here with Source). Guards
// the switch where it is read: unset and every value but "shadow" build no shadow at all (so nothing is read or
// recorded), "on" included. Positive control: "shadow" is on.
func TestProdShadowSwitchDefaultsOff(t *testing.T) {
	table, _, err := LoadTable()
	if err != nil {
		t.Fatal(err)
	}
	// 2026-11-21: "on" builds the loop too (it releases).
	for _, value := range []string{"", "off", "prod", "1"} {
		t.Setenv(prodShadowEnv, value)
		if ProdShadowOn() || NewProdShadow(table, ProdShadowHands{}) != nil {
			t.Errorf("%s=%q built a shadow", prodShadowEnv, value)
		}
	}
	t.Setenv(prodShadowEnv, "shadow")
	if !ProdShadowOn() {
		t.Error("positive control: shadow is not on")
	}
}

// Origin: 2026-11-23, recording a decision moved here from the server
// (recordHeadDecision); server's action trace test now reaches only the hand.
// Guards that one outcome goes to the audit log under the head-release
// principal and to the action trace with the deliverable and the run.
// Positive control: without the hands nothing is written and nothing panics.
func TestHandsRecordAuditsAndTraces(t *testing.T) {
	var audited, traced []string
	h := Hands{
		Audit: func(principal, action, target, result, detail string) {
			audited = append(audited, principal+"|"+action+"|"+target+"|"+result)
		},
		Trace: func(_ context.Context, action, mode, principal, why string, params map[string]any, decision, result any) {
			traced = append(traced, action+"|"+mode+"|"+principal+"|"+why+"|"+params["deliverable"].(string))
		},
	}
	h.Record(context.Background(), Deliverable{}, Outcome{Deliverable: "platform", Action: "waiting", Why: "CI pending"})
	if len(audited) != 1 || audited[0] != Principal+"|head_release|deliverable/platform|waiting" {
		t.Fatalf("audit: %v", audited)
	}
	if len(traced) != 1 || traced[0] != "start_pipeline_run|system_head|"+Principal+"|CI pending|platform" {
		t.Fatalf("trace: %v", traced)
	}
	Hands{}.Record(context.Background(), Deliverable{}, Outcome{Deliverable: "platform", Action: "waiting"})
}
