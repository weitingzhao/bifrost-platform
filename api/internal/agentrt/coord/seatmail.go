package coord

import (
	"regexp"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// SeatMailType is the bus's announcement that a message reached an inbox
// (bus/registry/rocket.yaml, seat.mail.arrived). A seat that stops while it
// waits arms one doorbell on its own address; a card decided, a sub-task
// handed back or another seat's reply all arrive as mail (2026-11-16: the
// lanes were pushed every 30 minutes because nothing woke them).
const SeatMailType = "seat.mail.arrived"

var mailToRE = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,160}$`)

// SeatMailRef is where the receiver reads its mail.
func SeatMailRef(to string) string { return "/api/v1/agentrt/messages?to=" + to }

// SeatMailBody is the event's body: subject and key are the receiver, change
// is the message's kind.
func SeatMailBody(to, messageID, kind string) map[string]any {
	return map[string]any{
		"to":         to,
		"seat":       to,
		"message_id": messageID,
		"kind":       kind,
		"subject":    to,
		"ref":        SeatMailRef(to),
		"key":        to,
		"change":     kind,
	}
}

func init() {
	eventlog.Declare(eventlog.Vocabulary{
		Bodies: map[string][]eventlog.Field{
			SeatMailType: {
				{Name: "to", Kind: eventlog.Str},
				{Name: "seat", Kind: eventlog.Str},
				{Name: "message_id", Kind: eventlog.Str},
				{Name: "kind", Kind: eventlog.Str},
				{Name: "subject", Kind: eventlog.Str},
				{Name: "ref", Kind: eventlog.Str},
				{Name: "key", Kind: eventlog.Str},
				{Name: "change", Kind: eventlog.Str},
			},
		},
		Values: map[string]map[string]eventlog.Value{
			SeatMailType: {"to": eventlog.Shaped("an actor address", mailToRE)},
		},
		After: map[string]func(body map[string]any) error{
			SeatMailType: func(body map[string]any) error {
				s := func(k string) string { v, _ := body[k].(string); return v }
				if body["subject"] != s("to") || body["seat"] != s("to") || body["key"] != s("to") || body["ref"] != SeatMailRef(s("to")) || body["change"] != s("kind") {
					return eventlog.Invalid("%s: subject, seat, key, ref and change are made of to and kind", SeatMailType)
				}
				return nil
			},
		},
	})
}
