package store

import (
	"context"
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
