// Package catalogusers writes catalog.users.changed to the ledger
// (2026-11-21, Foundation's ask, the topic as the communication line set it):
// after each scan round the map compares every package's users with the last
// event of the package's stream and writes one event where they changed. The
// event carries how many users of each kind (prod, test, event) and a digest
// of the lists, never the lists: ref is GET /api/v1/catalog/users, which
// serves them (?package=<subject> for one). The body's shape is declared in the register
// (rocket.yaml).
package catalogusers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/catalog"
)

// Type is the registered event type; Actor who writes it.
// Ref is the register's ref, compared word for word with the body's: the
// reader adds ?package=<subject> itself.
const (
	Type  = "catalog.users.changed"
	Actor = "service:catalog"
	Ref   = "/api/v1/catalog/users"
)

// Stream is one package's stream: its events are its users over time.
func Stream(pkg string) string { return "catalog-users:" + pkg }

// Change is one package's users now.
type Change struct {
	Package           string
	Prod, Test, Event []string
}

// Body is the event body of a change; the register checks its values.
func Body(c Change) map[string]any {
	return map[string]any{"subject": c.Package, "package": c.Package,
		"prod": len(c.Prod), "test": len(c.Test), "event": len(c.Event), "digest": Digest(c),
		"ref": Ref, "change": "changed"}
}

// Digest tells two lists of users apart without carrying them: the first 16
// hex of the SHA-256 of the three sorted lists.
func Digest(c Change) string {
	raw, _ := json.Marshal([3][]string{c.Prod, c.Test, c.Event})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])[:16]
}

// Writer appends to the ledger; DB is nil while it is not connected.
type Writer struct {
	DB func() eventlog.DB
}

var errNoLedger = errors.New("bus catalogusers: the ledger is not connected")

// Write appends an event for each package whose users differ from its
// stream's last event (or whose stream is empty), one transaction per
// package under the stream's lock; it returns how many it wrote.
func (w Writer) Write(ctx context.Context, changes []Change) (int, error) {
	var db eventlog.DB
	if w.DB != nil {
		db = w.DB()
	}
	if db == nil {
		return 0, errNoLedger
	}
	if err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		return actor.RegisterController(ctx, tx, Actor, "Map scan", "the map's scan, registered at its first write of package users")
	}); err != nil {
		return 0, err
	}
	n := 0
	for _, c := range changes {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		body := Body(c)
		stream := Stream(c.Package)
		wrote := false
		err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
			wrote = false
			if err := eventlog.LockStream(ctx, tx, stream); err != nil {
				return err
			}
			last, ok, err := eventlog.Last(ctx, tx, stream)
			if err != nil {
				return err
			}
			if ok && last.Type == Type && Same(last.Body, body) {
				return nil
			}
			if _, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: stream, Type: Type, Actor: Actor, Body: body}); err != nil {
				return err
			}
			wrote = true
			return nil
		})
		if err != nil {
			return n, err
		}
		if wrote {
			n++
		}
	}
	return n, nil
}

// Same is whether a kept body says what body says: the digest and the three
// counts (a JSON number reads back as float64).
func Same(kept, body map[string]any) bool {
	if kept == nil || strings.TrimSpace(asString(kept["digest"])) != body["digest"] {
		return false
	}
	for _, k := range []string{"prod", "test", "event"} {
		if asInt(kept[k]) != body[k].(int) {
			return false
		}
	}
	return true
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func asInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	}
	return -1
}

// Kept writes the changes of one round's kept users (catalog.Scanner's
// UsersChanged): the scan does not wait on a ledger that is not connected.
func (w Writer) Kept(ctx context.Context, doc catalog.UsersDoc) {
	changes := make([]Change, 0, len(doc.Packages))
	for _, p := range doc.Packages {
		changes = append(changes, Change{Package: p.Package, Prod: p.Users.Prod, Test: p.Users.Test, Event: p.Users.Event})
	}
	if n, err := w.Write(ctx, changes); err != nil {
		slog.Warn("catalogusers: package users to the ledger", "written", n, "err", err)
	}
}
