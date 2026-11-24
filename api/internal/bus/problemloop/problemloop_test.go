package problemloop

import (
	"encoding/json"
	"strings"
	"testing"
)

// 2026-11-18, OWNER_LAW #93: the text a letter ends with must be what the
// runtime reads back. Its tail is the envelope's JSON; with the two
// placeholders filled it has the fields the verdict reader requires, no more.
func TestAskIsTheEnvelopeTheReaderWants(t *testing.T) {
	ask := Ask("ev-123")
	i := strings.Index(ask, "body ")
	if i < 0 || !strings.HasPrefix(ask, "\nJudge it: send_message to controller, kind note, body {") {
		t.Fatalf("ask = %q", ask)
	}
	var env struct {
		V       int    `json:"v"`
		Type    string `json:"type"`
		Summary string `json:"summary"`
		Text    string `json:"text"`
	}
	if err := json.Unmarshal([]byte(ask[i+len("body "):]), &env); err != nil {
		t.Fatalf("the ask is not JSON: %v: %q", err, ask)
	}
	if env.V != 1 || env.Type != VerdictType || env.Summary != "<one line>" {
		t.Errorf("envelope = %+v", env)
	}
	var text map[string]string
	if err := json.Unmarshal([]byte(env.Text), &text); err != nil {
		t.Fatal(err)
	}
	if len(text) != 4 || text["event"] != "ev-123" || text["verdict"] != "real|false" || text["outcome"] != "handled|source_fixed|open" {
		t.Errorf("text = %v", text)
	}
	// Positive control for the kinds: both rows' words are known, a typo is not.
	if !KnownKind(KindAlert) || !KnownKind(KindDelivery) || KnownKind("alerts") {
		t.Error("KnownKind")
	}
}
