package coord

import (
	"regexp"
	"slices"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// The coordination domain's events, declared to the event log from here: the
// domain brought its vocabulary with it when it left the store (2026-11-14).
func init() {
	messageID := eventlog.Shaped("a message id", regexp.MustCompile(`^m_[0-9A-HJKMNP-TV-Z]{26}$`))
	scopes := make([]string, 0, len(controlKinds))
	for scope := range controlKinds {
		scopes = append(scopes, scope)
	}
	slices.Sort(scopes)
	eventlog.Declare(eventlog.Vocabulary{
		Bodies: map[string][]eventlog.Field{
			"message_posted": {{Name: "message_id", Kind: eventlog.ID}, {Name: "to", Kind: eventlog.ID}, {Name: "kind", Kind: eventlog.Str}},
			"message_acked":  {{Name: "message_id", Kind: eventlog.ID}},
			"control_set": {{Name: "control_id", Kind: eventlog.ID}, {Name: "scope", Kind: eventlog.Str}, {Name: "kind", Kind: eventlog.Str},
				{Name: "reason", Kind: eventlog.Str}, {Name: "scope_ref", Kind: eventlog.ID, Optional: true}, {Name: "token", Kind: eventlog.Str, Optional: true}},
			"control_cleared": {{Name: "control_id", Kind: eventlog.ID}},
		},
		Values: map[string]map[string]eventlog.Value{
			"message_posted": {"message_id": messageID, "kind": eventlog.OneOf("ask", "answer", "help", "handover", "note", "wake")},
			"message_acked":  {"message_id": messageID},
			"control_set":    {"scope": eventlog.OneOf(scopes...), "kind": eventlog.OneOf("stop", "pause", "drain")},
		},
	})
}
