package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// MetricDefinition identifies the immutable definition used for a reading.
type MetricDefinition struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
	SHA256  string `json:"sha256"`
}

// MetricReading is one append-only headline statistic and its supporting stats.
// Value is nil for no sample; a decimal string preserves numeric precision.
type MetricReading struct {
	MetricID                               string
	DefinitionVersion                      int
	DefinitionSHA256                       string
	ScopeKind, ScopeRef, Label, WindowKind string
	WindowStart, WindowEnd                 time.Time
	Unit                                   string
	Samples                                int
	Value                                  *string
	StatsJSON                              map[string]any
	Conclusive                             bool
	EvidenceArtifactID                     string
}

// MetricComputation carries the readings and provenance of one computation or import.
type MetricComputation struct {
	Source            string
	Definitions       []MetricDefinition
	Readings          []MetricReading
	Windows           []any
	Label, ArtifactID string
	Xmin              int64
}

// WriteMetricReadings records one computation and its new readings atomically.
// A savepoint measures actual inserts before the immutable event is hashed.
func WriteMetricReadings(ctx context.Context, db DB, c MetricComputation) (int, error) {
	n := 0
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		n = 0
		// Hold the stream lock outside the savepoint, including on its first use.
		if _, err := tx.Exec(ctx, `INSERT INTO agentrt.event_stream (event_stream_id) VALUES ('metrics') ON CONFLICT DO NOTHING`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT 1 FROM agentrt.event_stream WHERE event_stream_id='metrics' FOR UPDATE`); err != nil {
			return err
		}
		defs := make([]any, 0, len(c.Definitions))
		for _, d := range c.Definitions {
			defs = append(defs, map[string]any{"id": d.ID, "version": d.Version, "sha256": d.SHA256})
		}
		body := map[string]any{"source": c.Source, "rows": 0, "definitions": defs}
		if c.Windows != nil {
			body["windows"] = c.Windows
		}
		if c.Label != "" {
			body["label"] = c.Label
		}
		if c.ArtifactID != "" {
			body["artifact_id"] = c.ArtifactID
		}
		if c.Xmin != 0 {
			body["xmin"] = c.Xmin
		}
		if _, err := tx.Exec(ctx, `SAVEPOINT metric_count`); err != nil {
			return err
		}
		probe, err := Append(ctx, tx, Event{Stream: "metrics", Type: "metrics_computed", Actor: "controller", Body: body})
		if err != nil {
			return err
		}
		n, err = insertMetricReadings(ctx, tx, c, probe.EventID)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `ROLLBACK TO SAVEPOINT metric_count`); err != nil {
			return err
		}
		body["rows"] = n
		ev, err := Append(ctx, tx, Event{Stream: "metrics", Type: "metrics_computed", Actor: "controller", Body: body})
		if err != nil {
			return err
		}
		written, err := insertMetricReadings(ctx, tx, c, ev.EventID)
		if err != nil {
			return err
		}
		if written != n {
			return fmt.Errorf("store: metric readings changed during computation; retry")
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return n, nil
}

func insertMetricReadings(ctx context.Context, tx pgx.Tx, c MetricComputation, eventID int64) (int, error) {
	n := 0
	for _, r := range c.Readings {
		stats := r.StatsJSON
		if stats == nil {
			stats = map[string]any{}
		}
		tag, err := tx.Exec(ctx, `INSERT INTO agentrt.metric_reading
   (metric_reading_id,metric_id,definition_version,definition_sha256,scope_kind,scope_ref,label,window_kind,
    window_start,window_end,source,unit,samples,value,stats_json,conclusive,evidence_artifact_id,computed_from_event_id)
   VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14::text::numeric,$15,$16,$17,$18) ON CONFLICT DO NOTHING`,
			"mr_"+newULID(time.Now()), r.MetricID, r.DefinitionVersion, r.DefinitionSHA256, r.ScopeKind, r.ScopeRef, r.Label, r.WindowKind,
			r.WindowStart, r.WindowEnd, c.Source, r.Unit, r.Samples, r.Value, stats, r.Conclusive, nullable(r.EvidenceArtifactID), eventID)
		if err != nil {
			return 0, err
		}
		n += int(tag.RowsAffected())
	}
	return n, nil
}

// SetMetricLabel records a named baseline or group window.
func SetMetricLabel(ctx context.Context, db DB, label string, windowStart, windowEnd time.Time, reason, by string) error {
	if !windowEnd.After(windowStart) {
		return invalid("metric label window must end after it starts")
	}
	return InTx(ctx, db, func(tx pgx.Tx) error {
		body := map[string]any{"label": label, "window_start": windowStart.UTC().Format(time.RFC3339Nano), "window_end": windowEnd.UTC().Format(time.RFC3339Nano), "reason": reason}
		if by != "" {
			body["by"] = by
		}
		_, err := Append(ctx, tx, Event{Stream: "metric-labels", Type: "metric_label_set", Actor: "controller", Body: body})
		return err
	})
}
