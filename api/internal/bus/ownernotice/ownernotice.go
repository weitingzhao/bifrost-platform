// Package ownernotice says on the bus the things the Owner's phone is told
// about: a release policy running out (release.policy.expiring; channels L17,
// N04) and a seat or host gone quiet (seat.liveness.changed; L18, N03). The
// code that decides stays where it is and pushes as before; each decision it
// has pushed is also written here, so the notifier can take the push over
// after a side-by-side run (2026-11-18).
package ownernotice

import (
	"context"
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// The events.
const (
	TypePolicy   = "release.policy.expiring"
	TypeLiveness = "seat.liveness.changed"
)

// What changed.
const (
	ChangeExpiring = "expiring"
	ChangeExpired  = "expired"
	ChangeSilent   = "silent"
	ChangeLost     = "lost"
)

const (
	busActor    = "controller"
	policyRef   = "/api/v1/release-policy"
	livenessRef = "/api/v1/agent/threads"
)

var subjectRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:@/-]{0,199}$`)

// Notice is one decision the phone was told.
type Notice struct{ Type, Subject, Change string }

// Policy is the notice of a policy (id; "none" when there is no policy).
func Policy(id string, valid bool) Notice {
	if id == "" {
		id = "none"
	}
	change := ChangeExpired
	if valid {
		change = ChangeExpiring
	}
	return Notice{TypePolicy, id, change}
}

// Liveness is the notice of a quiet thread or a lost host, or false for
// another kind.
func Liveness(kind, id string) (Notice, bool) {
	switch kind {
	case "thread":
		return Notice{TypeLiveness, id, ChangeSilent}, true
	case "host":
		return Notice{TypeLiveness, id, ChangeLost}, true
	}
	return Notice{}, false
}

func refOf(typ string) string {
	if typ == TypePolicy {
		return policyRef
	}
	return livenessRef
}

// Body is the event's body.
func Body(n Notice) map[string]any {
	return map[string]any{"subject": n.Subject, "ref": refOf(n.Type), "change": n.Change}
}

// Valid says the bus can name n.
func Valid(n Notice) bool {
	if !subjectRE.MatchString(n.Subject) {
		return false
	}
	switch n.Type {
	case TypePolicy:
		return n.Change == ChangeExpiring || n.Change == ChangeExpired
	case TypeLiveness:
		return n.Change == ChangeSilent || n.Change == ChangeLost
	}
	return false
}

// Announce writes the notice. Before the bus's actor exists it writes nothing.
func Announce(ctx context.Context, db eventlog.DB, n Notice) error {
	if !Valid(n) {
		return fmt.Errorf("ownernotice: %s cannot be named on the bus", n.Type)
	}
	return eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		if ok, err := actor.Exists(ctx, tx, busActor); err != nil || !ok {
			return err
		}
		_, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: "notice:" + n.Type + ":" + n.Subject, Type: n.Type, Actor: busActor, Body: Body(n)})
		return err
	})
}

func init() {
	fields := []eventlog.Field{{Name: "subject", Kind: eventlog.Str}, {Name: "ref", Kind: eventlog.Str}, {Name: "change", Kind: eventlog.Str}}
	check := func(typ string) func(map[string]any) error {
		return func(b map[string]any) error {
			subject, _ := b["subject"].(string)
			change, _ := b["change"].(string)
			if !Valid(Notice{typ, subject, change}) || b["ref"] != refOf(typ) {
				return eventlog.Invalid("%s: subject, ref and change are fixed by the notice", typ)
			}
			return nil
		}
	}
	eventlog.Declare(eventlog.Vocabulary{
		Bodies: map[string][]eventlog.Field{TypePolicy: fields, TypeLiveness: fields},
		After:  map[string]func(map[string]any) error{TypePolicy: check(TypePolicy), TypeLiveness: check(TypeLiveness)},
	})
}
