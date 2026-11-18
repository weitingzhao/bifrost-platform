// Package schemaevents says on the bus that a migration reached the platform's
// database (schema.migration.applied; channel L25, 2026-11-18). The schema
// watch stays where it is and reads the applied migrations every minute; this
// writes an event for each it has not said yet. The first time, with nothing
// said before, it says only the newest: the rest is history, not news.
package schemaevents

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// Type is the event; DB the database it speaks of.
const (
	Type = "schema.migration.applied"
	DB   = "agentrt"
)

const (
	stream   = "schema:" + DB
	busActor = "controller"
	ref      = "/health"
)

var shaRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Migration is one applied migration.
type Migration struct {
	ID     int
	SHA256 string
}

// Subject is <db>:<migration>.
func Subject(id int) string { return DB + ":" + strconv.Itoa(id) }

// Body is the event's body.
func Body(m Migration) map[string]any {
	return map[string]any{"db": DB, "migration": m.ID, "sha256": m.SHA256, "subject": Subject(m.ID), "ref": ref, "change": "applied"}
}

// toSay picks what to write: what is newer than the last said, or, with
// nothing said yet (last < 0), only the newest.
func toSay(applied []Migration, last int) []Migration {
	sorted := slices.Clone(applied)
	slices.SortFunc(sorted, func(a, b Migration) int { return a.ID - b.ID })
	if len(sorted) == 0 {
		return nil
	}
	if last < 0 {
		return sorted[len(sorted)-1:]
	}
	var out []Migration
	for _, m := range sorted {
		if m.ID > last {
			out = append(out, m)
		}
	}
	return out
}

// Announce writes what has not been said, in one transaction, one process at
// a time (the api and the workers both watch). It says how many it wrote.
func Announce(ctx context.Context, db eventlog.DB, applied []Migration) (int, error) {
	n := 0
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		n = 0
		if ok, err := actor.Exists(ctx, tx, busActor); err != nil || !ok {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('bus.schema-events'))`); err != nil {
			return err
		}
		last := -1
		row, found, err := eventlog.Last(ctx, tx, stream)
		if err != nil {
			return err
		}
		if found {
			v, ok := row.Body["migration"].(float64)
			if !ok {
				return fmt.Errorf("schemaevents: the last event has no migration number")
			}
			last = int(v)
		}
		for _, m := range toSay(applied, last) {
			if _, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: stream, Type: Type, Actor: busActor, Body: Body(m)}); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}

func init() {
	eventlog.Declare(eventlog.Vocabulary{
		Bodies: map[string][]eventlog.Field{Type: {
			{Name: "db", Kind: eventlog.Str}, {Name: "migration", Kind: eventlog.Int}, {Name: "sha256", Kind: eventlog.Str},
			{Name: "subject", Kind: eventlog.Str}, {Name: "ref", Kind: eventlog.Str}, {Name: "change", Kind: eventlog.Str},
		}},
		Values: map[string]map[string]eventlog.Value{Type: {"sha256": eventlog.Shaped("a sha256", shaRE)}},
		After: map[string]func(map[string]any) error{Type: func(b map[string]any) error {
			id := 0
			switch v := b["migration"].(type) {
			case int:
				id = v
			case float64:
				id = int(v)
			}
			if id <= 0 || b["db"] != DB || b["subject"] != Subject(id) || b["ref"] != ref || b["change"] != "applied" {
				return eventlog.Invalid("%s: db, subject, ref and change are fixed by the migration", Type)
			}
			return nil
		}},
	})
}
