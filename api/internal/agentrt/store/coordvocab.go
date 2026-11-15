package store

import "github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"

// The coordination domain's events: messages between actors and the controls
// that stop, pause or drain. It is the first domain to declare its own
// vocabulary (2026-11-14, biz-align/domains-review.md): its types are not in
// vocab.go's tables, and this file goes with the domain when it becomes a
// package of its own.
var coordBodies = map[string][]field{
	"message_posted":  {{"message_id", kID, false}, {"to", kID, false}, {"kind", kStr, false}},
	"message_acked":   {{"message_id", kID, false}},
	"control_set":     {{"control_id", kID, false}, {"scope", kStr, false}, {"kind", kStr, false}, {"reason", kStr, false}, {"scope_ref", kID, true}, {"token", kStr, true}},
	"control_cleared": {{"control_id", kID, false}},
}

var coordValues = map[string]map[string]value{
	"message_posted": {"message_id": messageID, "kind": oneOf("ask", "answer", "help", "handover", "note", "wake")},
	"message_acked":  {"message_id": messageID},
	"control_set":    {"scope": oneOf(keysOf(controlKinds)...), "kind": oneOf("stop", "pause", "drain")},
}

func init() {
	eventlog.Declare(eventlog.Vocabulary{Bodies: declaredBodies(coordBodies), Values: coordValues})
}
