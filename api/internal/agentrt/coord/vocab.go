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
			"message_posted": {{Name: "message_id", Kind: eventlog.ID}, {Name: "to", Kind: eventlog.ID}, {Name: "kind", Kind: eventlog.Str},
				{Name: "matter", Kind: eventlog.Str, Optional: true}, {Name: "reply_to", Kind: eventlog.ID, Optional: true}},
			"message_acked": {{Name: "message_id", Kind: eventlog.ID}},
			"control_set": {{Name: "control_id", Kind: eventlog.ID}, {Name: "scope", Kind: eventlog.Str}, {Name: "kind", Kind: eventlog.Str},
				{Name: "reason", Kind: eventlog.Str}, {Name: "scope_ref", Kind: eventlog.ID, Optional: true}, {Name: "token", Kind: eventlog.Str, Optional: true}},
			"control_cleared": {{Name: "control_id", Kind: eventlog.ID}},
			// policy_alert: a text that claims the Owner's approval (a letter,
			// or a plan note through the plan's port): the rule, where the text
			// came in, the closed phrase it holds, the text's artifact (a
			// letter's) and the letter. Coordination's since 2026-11-17 (one
			// owning domain per event type, TD-583): the store declared it with
			// every rule other domains once wrote, each now its own type or
			// retired; their old rows stay and are read as before.
			"policy_alert": {{Name: "rule", Kind: eventlog.Str}, {Name: "source", Kind: eventlog.Str}, {Name: "phrase", Kind: eventlog.Str},
				{Name: "artifact_id", Kind: eventlog.ID, Optional: true}, {Name: "message_id", Kind: eventlog.ID, Optional: true}},
		},
		Values: map[string]map[string]eventlog.Value{
			"message_posted": {"message_id": messageID, "kind": eventlog.OneOf("ask", "answer", "help", "handover", "note", "wake"),
				"matter": eventlog.Shaped("a domain id or unknown", matterRE), "reply_to": messageID},
			"message_acked": {"message_id": messageID},
			"control_set":   {"scope": eventlog.OneOf(scopes...), "kind": eventlog.OneOf("stop", "pause", "drain")},
			"policy_alert": {"rule": eventlog.OneOf("approval_claim"), "source": eventlog.OneOf("message", "plan_note"),
				"phrase": eventlog.OneOf(ApprovalClaims...), "message_id": messageID},
		},
		After: map[string]func(map[string]any) error{"policy_alert": checkClaimAlert},
	})
}

// checkClaimAlert: a letter's claim names the letter's artifact; a plan
// note's text is in the note the alert is caused by, so it names none.
func checkClaimAlert(body map[string]any) error {
	if _, named := body["artifact_id"]; !named && body["source"] != "plan_note" {
		return eventlog.Invalid("policy_alert needs \"artifact_id\"")
	}
	return nil
}
