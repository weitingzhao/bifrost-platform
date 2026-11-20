// Package cards writes the approval cards' life to the bus: created, decided
// (approved, rejected, superseded, expired) and executed (executed, failed,
// unknown). A seat that waits on its card follows match=#<number> and is
// woken when the Owner decides it or the platform finishes it, instead of
// being nudged (2026-11-16: the lanes stopped after creating a card and the
// coordinator or the duty seat pushed them every 30 minutes).
//
// The approval store stays the truth (register: reconcile "approval store
// is the truth"); an event is a pointer to /api/v1/approvals/{id}.
package cards

import (
	"context"
	"log/slog"
	"regexp"
	"strconv"
	"time"

	actors "github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

const (
	TypeCreated  = "approval.card.created"
	TypeDecided  = "approval.card.decided"
	TypeExecuted = "approval.card.executed"
	// TypeNotified is one push about a card to the Owner's phone (created,
	// reminder, result). 2026-11-20, the metrics line: the pushes were kept
	// only in the card's deliveries and the last 500 audit records, so how
	// often the Owner was interrupted could not be counted across a
	// relaxation of the law; the ledger keeps them.
	TypeNotified = "approval.card.notified"
)

// actor writes the events; the platform seeds it.
const actor = "controller"

var (
	idRE     = regexp.MustCompile(`^appr_[0-9a-f]{16}$`)
	statusRE = regexp.MustCompile(`^[a-z_]{1,40}$`)
	actionRE = regexp.MustCompile(`^[a-z0-9_]{1,80}$`)
	wordRE   = regexp.MustCompile(`^[a-z0-9_]{1,40}$`)
	noticeRE = regexp.MustCompile(`(?:^|\s)kind=([a-z0-9_]{1,40})\s+channel=([a-z0-9_]{1,40})(?:\s|$)`)
)

// Card is what the bus keeps of one transition.
type Card struct {
	Type, ID, Action, Status string
	Number                   int
	// Kind and Channel name a push (TypeNotified only): created,
	// reminder_waiting… and ntfy…; empty when the audit detail did not say.
	Kind, Channel string
}

// TypeOf maps an approvals audit action to the bus type, or "" for one the
// bus does not carry (queue, claim).
func TypeOf(auditAction string) string {
	switch auditAction {
	case "approval.create":
		return TypeCreated
	case "approval.approve", "approval.reject", "approval.supersede", "approval.expire":
		return TypeDecided
	case "approval.execute", "approval.run.finish", "approval.late_result", "approval.unknown":
		return TypeExecuted
	case "approval.notify":
		return TypeNotified
	}
	return ""
}

// ParseNotice reads kind and channel from an approval.notify audit detail
// ("kind=created channel=ntfy target=relay"), or "" when it names none (a
// failure to store the deliveries).
func ParseNotice(detail string) (kind, channel string) {
	m := noticeRE.FindStringSubmatch(detail)
	if m == nil {
		return "", ""
	}
	return m[1], m[2]
}

// Subject is "#<number>", the register's subject.
func Subject(n int) string { return "#" + strconv.Itoa(n) }

// Ref is where a reader reads the card.
func Ref(id string) string { return "/api/v1/approvals/" + id }

// Stream is the ledger stream of one card.
func Stream(id string) string { return "card:" + id }

// Body is the event's body: the bus's fixed fields and their facts.
func Body(c Card) map[string]any {
	b := map[string]any{
		"id":      c.ID,
		"card":    Subject(c.Number),
		"number":  c.Number,
		"action":  c.Action,
		"status":  c.Status,
		"subject": Subject(c.Number),
		"ref":     Ref(c.ID),
		"key":     Subject(c.Number),
		"change":  c.Status,
	}
	if c.Type == TypeNotified {
		if c.Kind != "" {
			b["kind"] = c.Kind
		}
		if c.Channel != "" {
			b["channel"] = c.Channel
		}
	}
	return b
}

// Valid says the bus can name c (a card with a number and an id).
func Valid(c Card) bool {
	return c.Number > 0 && idRE.MatchString(c.ID) && statusRE.MatchString(c.Status) && actionRE.MatchString(c.Action) &&
		(c.Kind == "" || wordRE.MatchString(c.Kind)) && (c.Channel == "" || wordRE.MatchString(c.Channel)) &&
		(c.Type == TypeCreated || c.Type == TypeDecided || c.Type == TypeExecuted || c.Type == TypeNotified)
}

// Announce appends one event per card in one transaction. A ledger without
// the controller announces nothing.
func Announce(ctx context.Context, db eventlog.DB, list []Card) error {
	if db == nil || len(list) == 0 {
		return nil
	}
	return eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		if seeded, err := actors.Exists(ctx, tx, actor); err != nil || !seeded {
			return err
		}
		for _, c := range list {
			if _, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: Stream(c.ID), Type: c.Type, Actor: actor, Body: Body(c)}); err != nil {
				return err
			}
		}
		return nil
	})
}

func init() {
	fields := []eventlog.Field{
		{Name: "id", Kind: eventlog.Str},
		{Name: "card", Kind: eventlog.Str},
		{Name: "number", Kind: eventlog.Int},
		{Name: "action", Kind: eventlog.Str},
		{Name: "status", Kind: eventlog.Str},
		{Name: "subject", Kind: eventlog.Str},
		{Name: "ref", Kind: eventlog.Str},
		{Name: "key", Kind: eventlog.Str},
		{Name: "change", Kind: eventlog.Str},
	}
	values := map[string]eventlog.Value{
		"id":     eventlog.Shaped("an approval id", idRE),
		"status": eventlog.Shaped("an approval status", statusRE),
		"action": eventlog.Shaped("a catalog action id", actionRE),
	}
	after := func(body map[string]any) error {
		s := func(k string) string { v, _ := body[k].(string); return v }
		n := 0
		switch v := body["number"].(type) {
		case int:
			n = v
		case float64:
			n = int(v)
		}
		if n <= 0 || body["subject"] != Subject(n) || body["card"] != Subject(n) || body["key"] != Subject(n) || body["ref"] != Ref(s("id")) || body["change"] != s("status") {
			return eventlog.Invalid("approval.card: subject, key, ref and change are made of number, id and status")
		}
		return nil
	}
	v := eventlog.Vocabulary{Bodies: map[string][]eventlog.Field{}, Values: map[string]map[string]eventlog.Value{}, After: map[string]func(map[string]any) error{}}
	for _, t := range []string{TypeCreated, TypeDecided, TypeExecuted} {
		v.Bodies[t] = fields
		v.Values[t] = values
		v.After[t] = after
	}
	notified := append(append([]eventlog.Field{}, fields...),
		eventlog.Field{Name: "kind", Kind: eventlog.Str, Optional: true},
		eventlog.Field{Name: "channel", Kind: eventlog.Str, Optional: true})
	notifiedValues := map[string]eventlog.Value{"kind": eventlog.Shaped("a push kind", wordRE), "channel": eventlog.Shaped("a push channel", wordRE)}
	for k, f := range values {
		notifiedValues[k] = f
	}
	v.Bodies[TypeNotified] = notified
	v.Values[TypeNotified] = notifiedValues
	v.After[TypeNotified] = after
	eventlog.Declare(v)
}

// AnnounceInBackground writes the valid cards of list to the bus without
// holding up the approvals flow. A failed write is logged; the store stays
// the truth.
func AnnounceInBackground(db eventlog.DB, list []Card) {
	valid := make([]Card, 0, len(list))
	for _, c := range list {
		if Valid(c) {
			valid = append(valid, c)
		}
	}
	if len(valid) == 0 {
		return
	}
	safego.Go("bus.cards", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := Announce(ctx, db, valid); err != nil {
			slog.Warn("bus: approval cards not announced", "error", err, "count", len(valid))
		}
	})
}
