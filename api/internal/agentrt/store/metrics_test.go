package store_test

import (
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func metricFixture() store.MetricComputation {
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	sha := strings.Repeat("a", 64)
	value := "12.25"
	return store.MetricComputation{Source: "computed", Definitions: []store.MetricDefinition{{ID: "lead_time", Version: 1, SHA256: sha}}, Readings: []store.MetricReading{{MetricID: "lead_time", DefinitionVersion: 1, DefinitionSHA256: sha, ScopeKind: "all", WindowKind: "day", WindowStart: start, WindowEnd: start.Add(24 * time.Hour), Unit: "seconds", Samples: 1, Value: &value, Conclusive: true}}}
}

func TestMetricReadings(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	c := metricFixture()
	// Repeated keys inside a batch and across transactions are skipped.
	c.Readings = append(c.Readings, c.Readings[0])
	for _, want := range []int{1, 0} {
		n, err := store.WriteMetricReadings(ctx, w.rt, c)
		ok(t, err)
		if n != want {
			t.Fatalf("wrote %d, want %d", n, want)
		}
		var count int
		ok(t, w.rt.QueryRow(ctx, `SELECT (body_json->>'rows')::int FROM agentrt.event WHERE event_type='metrics_computed' ORDER BY event_id DESC LIMIT 1`).Scan(&count))
		if count != n {
			t.Fatalf("event rows %d, inserted %d", count, n)
		}
	}
	reader := w.db.Connect(t, w.db.Reader)
	var count int
	ok(t, reader.QueryRow(ctx, `SELECT count(*) FROM agentrt.metric_reading`).Scan(&count))
	if count != 1 {
		t.Fatalf("reader saw %d readings", count)
	}
	// Use the owner to prove the append-only triggers, rather than ACL denial.
	owner := w.db.Connect(t, w.db.Owner)
	for _, sql := range []string{`UPDATE agentrt.metric_reading SET samples=1`, `DELETE FROM agentrt.metric_reading`, `TRUNCATE agentrt.metric_reading`} {
		if _, err := owner.Exec(ctx, sql); err == nil {
			t.Fatalf("allowed %s", sql)
		}
	}
	ok(t, store.SetMetricLabel(ctx, w.rt, "baseline:before", c.Readings[0].WindowStart, c.Readings[0].WindowEnd, "comparison", "owner"))
}

func TestMetricReadingGuards(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	c := metricFixture()
	_, err := store.WriteMetricReadings(ctx, w.rt, c)
	ok(t, err)
	var old int64
	ok(t, w.rt.QueryRow(ctx, `SELECT max(event_id) FROM agentrt.event WHERE event_type='metrics_computed'`).Scan(&old))
	for _, mode := range []string{"missing", "wrong_type", "old_transaction", "scope", "label", "imported"} {
		t.Run(mode, func(t *testing.T) {
			err := store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
				id := old
				typ := "metrics_computed"
				body := map[string]any{"source": "computed", "rows": 1, "definitions": []any{}}
				if mode == "wrong_type" {
					typ = "note"
					body = map[string]any{"text": "guard test"}
				}
				if mode != "old_transaction" && mode != "missing" {
					ev, e := store.Append(ctx, tx, store.Event{Stream: "metrics", Type: typ, Actor: "controller", Body: body})
					if e != nil {
						return e
					}
					id = ev.EventID
				}
				if mode == "missing" {
					id = -1
				}
				scope, label, source := "", "", "computed"
				if mode == "scope" {
					scope = "repo"
				}
				if mode == "label" {
					label = "baseline:test"
				}
				if mode == "imported" {
					source = "imported"
				}
				_, e := tx.Exec(ctx, `INSERT INTO agentrt.metric_reading
     (metric_reading_id,metric_id,definition_version,definition_sha256,scope_kind,scope_ref,label,window_kind,window_start,window_end,source,unit,samples,conclusive,computed_from_event_id)
     VALUES ('mr_01M59KGZ56VX9YCCCN9KCR9264','guard',1,$1,'all',$2,$3,'day',$4,$5,$6,'count',0,false,$7)`, strings.Repeat("a", 64), scope, label, c.Readings[0].WindowStart, c.Readings[0].WindowEnd, source, id)
				return e
			})
			if err == nil {
				t.Fatalf("allowed %s", mode)
			}
			var pg *pgconn.PgError
			if !errors.As(err, &pg) || pg.Code != "23514" {
				t.Fatalf("expected check violation, got %v", err)
			}
		})
	}
}

func TestMetricImportedReading(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	c := metricFixture()
	c.Source = "imported"
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		id, err := store.PutArtifact(ctx, tx, "controller", "report", "text/plain", []byte("metric evidence"))
		c.ArtifactID = id
		c.Readings[0].EvidenceArtifactID = id
		return err
	}))
	n, err := store.WriteMetricReadings(ctx, w.rt, c)
	ok(t, err)
	if n != 1 {
		t.Fatalf("import wrote %d", n)
	}
}

func TestMetricChecksRollbackComputation(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	for _, change := range []func(*store.MetricReading){
		func(r *store.MetricReading) { r.WindowEnd = r.WindowStart },
		func(r *store.MetricReading) { r.ScopeKind = "repo" },
		func(r *store.MetricReading) { r.Label = "unshaped" },
		func(r *store.MetricReading) { r.Value = nil },
		func(r *store.MetricReading) { r.Samples = 0 },
		func(r *store.MetricReading) { r.Samples = -1 },
		func(r *store.MetricReading) { r.DefinitionVersion = 0 },
		func(r *store.MetricReading) { r.DefinitionSHA256 = "bad" },
	} {
		c := metricFixture()
		change(&c.Readings[0])
		n, err := store.WriteMetricReadings(ctx, w.rt, c)
		if err == nil || n != 0 {
			t.Fatalf("invalid reading wrote %d: %v", n, err)
		}
	}
	var n int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type='metrics_computed'`).Scan(&n))
	if n != 0 {
		t.Fatalf("failed computations left %d events", n)
	}
}
