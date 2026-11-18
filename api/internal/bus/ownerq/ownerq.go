// Package ownerq writes the Owner's questions to the bus: asked, and answered
// (answered, withdrawn or expired). A seat waiting on its question follows
// match=<id> and reads the answer when the event comes, instead of reading the
// question every two seconds (channel W03, 2026-11-18). The question store
// stays the truth; an event is a pointer to /api/v1/questions/{id}.
package ownerq

import (
	"context"
	"regexp"

	"github.com/jackc/pgx/v5"

	actors "github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

const (
	TypeAsked    = "owner.question.asked"
	TypeAnswered = "owner.question.answered"
)

// actor writes the events; the platform seeds it.
const actor = "controller"

var (
	idRE     = regexp.MustCompile(`^q_[0-9a-f]{16}$`)
	statusRE = regexp.MustCompile(`^[a-z_]{1,40}$`)
)

// Question is what the bus keeps of one transition.
type Question struct{ Type, ID, Status string }

// TypeOf maps a question's audit action to the bus type, or "" for one the bus
// does not carry (notify, ack, a store error).
func TypeOf(auditAction string) string {
	switch auditAction {
	case "question.create":
		return TypeAsked
	case "question.answer", "question.withdraw", "question.expire":
		return TypeAnswered
	}
	return ""
}

// Ref is where a reader reads the question.
func Ref(id string) string { return "/api/v1/questions/" + id }

// Stream is the ledger stream of one question.
func Stream(id string) string { return "question:" + id }

// Valid says the bus can name q.
func Valid(q Question) bool {
	return idRE.MatchString(q.ID) && statusRE.MatchString(q.Status) && (q.Type == TypeAsked || q.Type == TypeAnswered)
}

// Body is the event's body.
func Body(q Question) map[string]any {
	return map[string]any{"id": q.ID, "status": q.Status, "subject": q.ID, "ref": Ref(q.ID), "change": q.Status}
}

// Announce appends one event per question in one transaction. A ledger
// without the controller announces nothing.
func Announce(ctx context.Context, db eventlog.DB, list []Question) error {
	if db == nil || len(list) == 0 {
		return nil
	}
	return eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		if seeded, err := actors.Exists(ctx, tx, actor); err != nil || !seeded {
			return err
		}
		for _, q := range list {
			if _, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: Stream(q.ID), Type: q.Type, Actor: actor, Body: Body(q)}); err != nil {
				return err
			}
		}
		return nil
	})
}

func init() {
	fields := []eventlog.Field{
		{Name: "id", Kind: eventlog.Str}, {Name: "status", Kind: eventlog.Str}, {Name: "subject", Kind: eventlog.Str},
		{Name: "ref", Kind: eventlog.Str}, {Name: "change", Kind: eventlog.Str},
	}
	values := map[string]eventlog.Value{"id": eventlog.Shaped("a question id", idRE), "status": eventlog.Shaped("a question status", statusRE)}
	after := func(b map[string]any) error {
		id, _ := b["id"].(string)
		if b["subject"] != id || b["ref"] != Ref(id) || b["change"] != b["status"] {
			return eventlog.Invalid("owner.question: subject, ref and change are made of id and status")
		}
		return nil
	}
	v := eventlog.Vocabulary{Bodies: map[string][]eventlog.Field{}, Values: map[string]map[string]eventlog.Value{}, After: map[string]func(map[string]any) error{}}
	for _, t := range []string{TypeAsked, TypeAnswered} {
		v.Bodies[t], v.Values[t], v.After[t] = fields, values, after
	}
	eventlog.Declare(v)
}
