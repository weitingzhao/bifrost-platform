// Package homelayout keeps the Owner's arrangement of the Ops home's city
// (2026-11-18): the land, his roads and his buildings, as a Tiled map. A save
// is an artifact holding the map and a home_layout_saved event on the stream
// home:layout naming it; the event's seq is the version, and the stream is the
// history.
//
// Contract, version 1:
//
//	GET  /api/v1/home/layout   {version, layout, saved_by, saved_at}; version 0 and no layout before the first save
//	PUT  /api/v1/home/layout   {base_version, layout}: saved as base_version+1
//	                           409 {error, version} when another save came first
//
// A layout is his preference: no reading of the system goes in, and nothing
// acts on it.
package homelayout

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/artifact"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// The ledger's names for a save.
const (
	Stream    = "home:layout"
	EventType = "home_layout_saved"
)

// The limits of a layout: its encoded size, the side of the land, its buildings.
const (
	MaxBytes = 256 << 10
	MaxSide  = 96
	MaxParts = 2000
)

var (
	// ErrStale: another save came after the version this one started from.
	ErrStale = errors.New("the layout was saved elsewhere since this one was opened")
	// ErrNotMigrated: the database does not have the table yet.
	ErrNotMigrated = errors.New("this database has not applied the home layout migration yet")
)

// Saved is the current layout; Version 0 means none was saved.
type Saved struct {
	Version int             `json:"version"`
	Layout  json.RawMessage `json:"layout"`
	SavedBy string          `json:"saved_by,omitempty"`
	SavedAt time.Time       `json:"saved_at,omitzero"`
}

type layer struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Data    []int  `json:"data"`
	Objects []struct {
		Type   string  `json:"type"`
		X      float64 `json:"x"`
		Y      float64 `json:"y"`
		Width  float64 `json:"width"`
		Height float64 `json:"height"`
	} `json:"objects"`
}

// Check says what is wrong with a layout, before anything is written.
func Check(raw []byte) error {
	if len(raw) > MaxBytes {
		return fmt.Errorf("a layout is at most %d bytes", MaxBytes)
	}
	var m struct {
		Type        string  `json:"type"`
		Orientation string  `json:"orientation"`
		Width       int     `json:"width"`
		Height      int     `json:"height"`
		TileHeight  float64 `json:"tileheight"`
		Layers      []layer `json:"layers"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("a layout is a Tiled map: %w", err)
	}
	switch {
	case m.Type != "map" || m.Orientation != "isometric":
		return errors.New("a layout is an isometric Tiled map")
	case m.Width < 1 || m.Height < 1 || m.Width > MaxSide || m.Height > MaxSide:
		return fmt.Errorf("the land is 1 to %d cells a side", MaxSide)
	case m.TileHeight <= 0:
		return errors.New("a layout names its tile height")
	}
	var roads, parts *layer
	for i := range m.Layers {
		l := &m.Layers[i]
		switch {
		case l.Type == "tilelayer" && l.Name == "roads":
			roads = l
		case l.Type == "objectgroup" && l.Name == "parts":
			parts = l
		}
	}
	if roads == nil || parts == nil {
		return errors.New("a layout has a roads tile layer and a parts object group")
	}
	if len(roads.Data) != m.Width*m.Height {
		return errors.New("the roads layer has one value a cell")
	}
	if len(parts.Objects) > MaxParts {
		return fmt.Errorf("a layout has at most %d buildings", MaxParts)
	}
	for _, o := range parts.Objects {
		size := o.Width / m.TileHeight
		if o.Type == "" || o.Width != o.Height || (size != 1 && size != 2 && size != 4 && size != 8) {
			return errors.New("a building has a kind and a square footprint of 1, 2, 4 or 8 cells")
		}
		if o.X < 0 || o.Y < 0 || o.X+o.Width > float64(m.Width)*m.TileHeight || o.Y+o.Height > float64(m.Height)*m.TileHeight {
			return errors.New("a building stands on the land")
		}
	}
	return nil
}

func init() {
	eventlog.Declare(eventlog.Vocabulary{
		Types: []string{EventType},
		Bodies: map[string][]eventlog.Field{EventType: {
			{Name: "artifact_id", Kind: eventlog.ID}, {Name: "size_bytes", Kind: eventlog.Int},
		}},
	})
}

// migrated turns the refusal of a database without migration 0034 into ErrNotMigrated.
func migrated(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23514" && pg.ConstraintName == "event_type_closed" {
		return ErrNotMigrated
	}
	return err
}

// Latest reads the current layout.
func Latest(ctx context.Context, db eventlog.DB) (Saved, error) {
	var s Saved
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		last, ok, err := eventlog.Last(ctx, tx, Stream)
		if err != nil || !ok {
			return err
		}
		id, _ := last.Body["artifact_id"].(string)
		var body []byte
		if err := tx.QueryRow(ctx, `SELECT body FROM agentrt.artifact_blob WHERE artifact_id = $1`, id).Scan(&body); err != nil {
			return fmt.Errorf("the layout of version %d: %w", last.Seq, err)
		}
		s = Saved{Version: int(last.Seq), Layout: body, SavedBy: last.Actor, SavedAt: last.At}
		return nil
	})
	return s, err
}

// Save stores layout as the version after base, which must be the current one.
// On ErrStale the returned Saved holds the version that came first.
func Save(ctx context.Context, db eventlog.DB, w actor.Writer, base int, layout []byte) (Saved, error) {
	if base < 0 {
		return Saved{}, errors.New("base_version is the version this layout started from, 0 for none")
	}
	if err := Check(layout); err != nil {
		return Saved{}, err
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, layout); err != nil {
		return Saved{}, err
	}
	body := compact.Bytes()
	sum := sha256.Sum256(body)
	id := "sha256:" + hex.EncodeToString(sum[:])
	var out Saved
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		if err := actor.Ensure(ctx, tx, w, false); err != nil {
			return err
		}
		// Held to the commit: two saves from one version cannot both pass.
		if err := eventlog.LockStream(ctx, tx, Stream); err != nil {
			return err
		}
		last, ok, err := eventlog.Last(ctx, tx, Stream)
		if err != nil {
			return err
		}
		current := 0
		if ok {
			current = int(last.Seq)
		}
		if current != base {
			out = Saved{Version: current}
			return ErrStale
		}
		// The event first: the database refuses a state write without one.
		ev, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: Stream, Type: EventType, Actor: w.Actor,
			Body: map[string]any{"artifact_id": id, "size_bytes": len(body)}})
		if err != nil {
			return err
		}
		stored, err := artifact.PutArtifact(ctx, tx, w.Actor, "other", "application/json", body)
		if err != nil {
			return err
		}
		if stored != id {
			return fmt.Errorf("the layout was stored as %s, not %s", stored, id)
		}
		out = Saved{Version: int(ev.Seq), Layout: body, SavedBy: w.Actor, SavedAt: ev.At}
		return nil
	})
	return out, migrated(err)
}
