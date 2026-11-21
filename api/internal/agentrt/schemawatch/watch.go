// Package schemawatch is what platform-api reads of the agentrt schema: which
// migrations the database applied, said in /health and the log, and on the bus.
package schemawatch

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/migrate"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/readerseen"
)

// What platform-api reads of the agentrt schema (TD-411: STG ran a platform
// that needs 0007 and 0008 on a database at 0006, its controller failed every
// 15 seconds with a warning, and health said ok). Once the database is
// connected it reads which migrations the database applied, then again every
// schemaEvery, so it says current once the agentrt-migrate Job has run. It
// logs each change: an ERROR when the database is behind or an applied
// migration was edited, a WARN when it is ahead (an older binary), INFO when
// it is current again. Health says the state it last read; it never reads the
// database itself, so a probe never waits on it. Nothing is stopped: the
// loops that do not need the new structure go on.

// schemaEvery is how often the schema is read again.
const schemaEvery = time.Minute

// Watch is the schema last read; the zero value has read nothing.
type Watch struct {
	mu     sync.Mutex
	status *migrate.SchemaStatus
	err    string
	// log writes a line at a level; nil writes it with slog.
	log func(level slog.Level, msg string, args ...any)
	// Announce, when set, says the applied migrations on the bus; said is the
	// newest this process has seen said.
	Announce func(ctx context.Context, applied []migrate.Applied) error
	said     int
}

// Run reads the schema now and every schemaEvery until ctx ends.
func (w *Watch) Run(ctx context.Context, q migrate.Querier) {
	t := time.NewTicker(schemaEvery)
	defer t.Stop()
	for {
		w.check(ctx, q)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// check reads the schema once and logs a change from the last read.
func (w *Watch) check(ctx context.Context, q migrate.Querier) {
	rctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	st, err := migrate.Check(rctx, q)
	cancel()
	if ctx.Err() != nil {
		return
	}
	w.mu.Lock()
	prev, prevErr := w.status, w.err
	if err != nil {
		w.err = err.Error()
	} else {
		w.status, w.err = &st, ""
	}
	w.mu.Unlock()
	if err == nil {
		w.say(ctx, st.Applied)
	}
	switch {
	case err != nil && prevErr == "":
		w.logf(slog.LevelWarn, "agentrt: the database's schema could not be read", "err", err)
	case err != nil:
	case prev != nil && prevErr == "" && sameSchema(*prev, st): // after a failed read, the state is said again (review 466 C-1)
	case st.State == migrate.SchemaBehind || st.State == migrate.SchemaEdited:
		w.logf(slog.LevelError, "agentrt: the database's schema is "+st.State+" this binary's migrations; the controller "+
			"fails where it needs them. Run the agentrt-migrate Job for this environment first",
			"missing", st.Missing, "edited", st.Edited, "unknown", st.Unknown)
	case st.State == migrate.SchemaAhead:
		w.logf(slog.LevelWarn, "agentrt: the database has migrations this binary does not carry (an older binary)", "unknown", st.Unknown)
	default:
		w.logf(slog.LevelInfo, "agentrt: the database's schema is current")
	}
}

// say hands the applied migrations to the bus when a newer one is there; a
// failure is said again at the next read.
func (w *Watch) say(ctx context.Context, applied []migrate.Applied) {
	if w.Announce == nil {
		return
	}
	readerseen.Saw("schema-watch")
	newest := 0
	for _, a := range applied {
		newest = max(newest, a.ID)
	}
	if newest <= w.said {
		return
	}
	actx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := w.Announce(actx, applied); err != nil {
		w.logf(slog.LevelWarn, "agentrt: the applied migrations were not said on the bus", "err", err)
		return
	}
	w.said = newest
}

func sameSchema(a, b migrate.SchemaStatus) bool {
	return a.State == b.State && slices.Equal(a.Missing, b.Missing) && slices.Equal(a.Edited, b.Edited) && slices.Equal(a.Unknown, b.Unknown)
}

func (w *Watch) logf(level slog.Level, msg string, args ...any) {
	if w.log != nil {
		w.log(level, msg, args...)
		return
	}
	slog.Log(context.Background(), level, msg, args...)
}

// Health is what /health says of the schema: agentrt_schema is off without
// the runtime, unknown until it is read (or when the read failed), else the
// state, with the migrations missing, edited or unknown named, and every
// migration the database has applied (agentrt_schema_applied, number and
// sha256), which the PROD deliver pipeline checks the next build's
// migrations against before it rolls out (TD-411).
func (w *Watch) Health() map[string]any {
	if w == nil {
		return map[string]any{"agentrt_schema": "off"}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.status == nil || w.err != "" {
		return map[string]any{"agentrt_schema": "unknown"}
	}
	out := map[string]any{"agentrt_schema": w.status.State, "agentrt_schema_applied": w.status.Applied}
	if len(w.status.Missing) > 0 {
		out["agentrt_schema_missing"] = w.status.Missing
	}
	if len(w.status.Edited) > 0 {
		out["agentrt_schema_edited"] = w.status.Edited
	}
	if len(w.status.Unknown) > 0 {
		out["agentrt_schema_unknown"] = w.status.Unknown
	}
	return out
}

// Applied is the migrations the database had applied at the last read
// (number -> sha256); ok is false when no read has succeeded or the last
// one failed.
func (w *Watch) Applied() (map[int]string, bool) {
	if w == nil {
		return nil, false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.status == nil || w.err != "" {
		return nil, false
	}
	out := make(map[int]string, len(w.status.Applied))
	for _, a := range w.status.Applied {
		out[a.ID] = a.SHA256
	}
	return out, true
}

// AnnounceMigrations says the applied migrations through db (Announce,
// schemaevents.go).
func AnnounceMigrations(db eventlog.DB) func(context.Context, []migrate.Applied) error {
	return func(ctx context.Context, applied []migrate.Applied) error {
		ms := make([]Migration, 0, len(applied))
		for _, a := range applied {
			ms = append(ms, Migration{ID: a.ID, SHA256: a.SHA256})
		}
		_, err := Announce(ctx, db, ms)
		return err
	}
}
