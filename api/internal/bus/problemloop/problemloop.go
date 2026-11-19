// Package problemloop is what the bus's problem letters say about judging
// them (2026-11-18, OWNER_LAW #36: a problem is sent, seen, judged true or
// false, then a real one handled and a false one fixed at the source). A
// letter of a row with params.ask_verdict ends with the exact message the
// receiver sends to give its verdict; the runtime reads those messages back
// (agentrt/coord, ProblemLoop). Nothing here reads or writes a store.
package problemloop

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// ParamAskVerdict is the row parameter that makes a row's letter ask for a
// verdict; its value is the kind of problem the row's letters are.
const ParamAskVerdict = "ask_verdict"

// The kinds of problem a letter can be.
const (
	KindAlert    = "alert"
	KindDelivery = "delivery"
)

// KnownKind says whether k is a kind a row may name.
func KnownKind(k string) bool { return k == KindAlert || k == KindDelivery }

// VerdictType is the type of the envelope a verdict is sent in.
const VerdictType = "problem_verdict"

// The words a verdict uses.
const (
	VerdictReal  = "real"
	VerdictFalse = "false"

	OutcomeHandled     = "handled"
	OutcomeSourceFixed = "source_fixed"
	OutcomeOpen        = "open"
)

// Ask is the end of a letter for event: the exact message the receiver sends
// (send_message to controller, kind note) so nobody has to look it up. The
// placeholders are the only parts the receiver fills.
func Ask(event string) string {
	inner, _ := marshal(struct {
		Event   string `json:"event"`
		Verdict string `json:"verdict"`
		Outcome string `json:"outcome"`
		Ref     string `json:"ref"`
	}{event, VerdictReal + "|" + VerdictFalse, OutcomeHandled + "|" + OutcomeSourceFixed + "|" + OutcomeOpen, ""})
	outer, _ := marshal(struct {
		V       int    `json:"v"`
		Type    string `json:"type"`
		Summary string `json:"summary"`
		Text    string `json:"text"`
	}{1, VerdictType, "<one line>", string(inner)})
	return fmt.Sprintf("\nJudge it: send_message to controller, kind note, body %s", outer)
}

// marshal is json.Marshal without HTML escaping, so the placeholders stay readable.
func marshal(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}
