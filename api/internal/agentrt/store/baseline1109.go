package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// Frozen evidence supplied by the Owner's AM-6 brief, sealed 2026-11-09.
// This is an aggregate transcription, not a reconstruction of missing events.
const baseline1109Evidence = `AM-6 baseline sealed 2026-11-09, baseline:pre-group-0.
Window: 2026-11-03 through 2026-11-09 inclusive (UTC date boundaries).
Task start to PROD median: 2.7 hours. Waiting on the Owner: 38%.
Owner cards: 147 in 7 days (approval/question split unavailable).
CI runs per commit reaching main: approximately 1.4; reuse count unavailable.
Raw data is gone. The original task/commit sample counts and stage breakdown
are unavailable. One aggregate observation is stored per metric; these imports
are not conclusive. Definitions and hashes are frozen at import version 1;
a later definition cannot recompute this baseline.
Source: Owner AM-5/AM-6 R6 group 0 brief, 2026-11-10.
`

// ImportBaseline1109 imports the sealed aggregates once, atomically with their
// evidence. The metrics stream serializes concurrent invocations. It deliberately
// does not consult the current definition registry: raw data no longer exists.
func ImportBaseline1109(ctx context.Context, db DB) (int, error) {
	n := 0
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO agentrt.event_stream(event_stream_id) VALUES ('metrics') ON CONFLICT DO NOTHING`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT 1 FROM agentrt.event_stream WHERE event_stream_id='metrics' FOR UPDATE`); err != nil {
			return err
		}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agentrt.metric_reading WHERE label='baseline:pre-group-0' AND source='imported')`).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return nil
		}
		artifact, err := PutArtifact(ctx, tx, "controller", "report", "text/plain", []byte(baseline1109Evidence))
		if err != nil {
			return err
		}
		start := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
		c := MetricComputation{Source: "imported", Label: "baseline:pre-group-0", ArtifactID: artifact}
		// Hashes of the original v1 definitions, not whatever version is current.
		for _, v := range []struct{ id, value, unit, hash string }{
			{"lead_time_s", "9720", "seconds", "1e5d1190093878c437b874f099b74c41d13415d4a43c9a541a5138ff0894af9f"},
			{"owner_wait_share", "0.38", "ratio", "749a86dc164c0721c02f00d3413cd21dfb85e39f45fe2019a173045c8767be9e"},
			{"ci_runs_per_commit", "1.4", "count", "dd318ac034438458d51f17cc2b2d712651cfb3eb6985590dc0f0303b83900d7a"},
		} {
			value := v.value
			c.Definitions = append(c.Definitions, MetricDefinition{ID: v.id, Version: 1, SHA256: v.hash})
			c.Readings = append(c.Readings, MetricReading{MetricID: v.id, DefinitionVersion: 1, DefinitionSHA256: v.hash, ScopeKind: "all", Label: c.Label, WindowKind: "span", WindowStart: start, WindowEnd: start.AddDate(0, 0, 7), Unit: v.unit, Samples: 1, Value: &value, Conclusive: false, EvidenceArtifactID: artifact, StatsJSON: map[string]any{"sample_basis": "one sealed aggregate; underlying sample count unknown", "raw_data_available": false, "evidence_artifact_id": artifact, "owner_cards": 147, "days": 7, "approximate": v.id == "ci_runs_per_commit"}})
		}
		defs := make([]any, 0, len(c.Definitions))
		for _, d := range c.Definitions {
			defs = append(defs, map[string]any{"id": d.ID, "version": d.Version, "sha256": d.SHA256})
		}
		ev, err := Append(ctx, tx, Event{Stream: "metrics", Type: "metrics_computed", Actor: "controller", Body: map[string]any{"source": "imported", "rows": len(c.Readings), "definitions": defs, "label": c.Label, "artifact_id": artifact}})
		if err != nil {
			return err
		}
		n, err = insertMetricReadings(ctx, tx, c, ev.EventID)
		return err
	})
	if err != nil {
		return 0, err
	}
	return n, nil
}
