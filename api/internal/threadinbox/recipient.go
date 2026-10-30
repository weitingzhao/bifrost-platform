package threadinbox

import (
	"regexp"
	"strings"
)

// Accept a primary session/actor, optionally followed by a parenthesized note.
// A note cannot contain another address or a URL/path. Never search prose for
// a recipient; registration alone cannot prove which thread requested a card.
var session = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
var annotated = regexp.MustCompile(`^([A-Za-z0-9._:-]+) \(([^()]*)\)$`)

func Recipient(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return ""
	}
	// The 128-character bound belongs to the session, not the actor with
	// its "thread:" prefix. Use the same validation for every input form.
	if to := actor(label); to != "" {
		return to
	}
	parts := annotated.FindStringSubmatch(label)
	if parts == nil || strings.ContainsAny(parts[2], "/\\?#=@:\r\n") ||
		strings.Contains(parts[2], "local_") {
		return ""
	}
	return actor(parts[1])
}

func actor(value string) string {
	id := strings.TrimPrefix(value, "thread:")
	if !session.MatchString(id) || strings.HasPrefix(id, "thread:") {
		return ""
	}
	// An explicit thread actor may have a session such as "owner" or
	// "agent:review"; ThreadActor permits these. Only a bare label naming
	// another kind of actor is excluded here. Delivery checks registration.
	if strings.HasPrefix(value, "thread:") {
		return value
	}
	for _, prefix := range []string{"seat:", "agent:", "host:", "exec:"} {
		if strings.HasPrefix(id, prefix) {
			return ""
		}
	}
	if id == "owner" || id == "controller" {
		return ""
	}
	return "thread:" + id
}
