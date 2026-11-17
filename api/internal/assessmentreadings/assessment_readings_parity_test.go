package assessmentreadings_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/assessmentreadings"
	"testing"
)

func TestAssessmentRecordedQueryParityPG(t *testing.T) {
	t.Parallel()
	db := agentrttest.MigratedWith(t, "seeded", func(ctx context.Context, owner *pgx.Conn) error {
		seeded, err := store.Seed(ctx, owner)
		if err == nil && !seeded {
			err = errors.New("seed did nothing on an empty database")
		}
		return err
	})
	pool := db.Pool(t, db.Runtime)
	ctx := context.Background()
	for _, kind := range []string{assessmentreadings.AssessCard, assessmentreadings.AssessApproval} {
		ok(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
			for _, text := range []string{"recorded first", "recorded second"} {
				_, err := store.Append(ctx, tx, store.Event{Stream: assessmentreadings.AssessStream(kind, "parity"), Type: "note", Actor: "controller", Body: map[string]any{"text": text}})
				if err != nil {
					return err
				}
			}
			return nil
		}))
	}

	rows, err := pool.Query(ctx, "SELECT DISTINCT event_stream_id FROM agentrt.event ORDER BY event_stream_id LIMIT 12")
	ok(t, err)
	streams := []string{"assess:card:missing", "assess:approval:missing", "assess:card:parity", "assess:approval:parity"}
	for rows.Next() {
		var stream string
		ok(t, rows.Scan(&stream))
		streams = append(streams, stream)
	}
	ok(t, rows.Err())
	rows.Close()
	if len(streams) == 4 {
		t.Fatal("seed supplied no recorded streams")
	}
	for _, stream := range streams {
		var want json.RawMessage
		ok(t, pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_object(
		  'event_type', event_type, 'body_json', body_json, 'at', at) ORDER BY seq), '[]')
		  FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note'`, stream).Scan(&want))
		got, err := assessmentreadings.Events(ctx, pool, stream)
		ok(t, err)
		if !bytes.Equal(want, got) {
			t.Fatalf("stream %q: %s != %s", stream, want, got)
		}
	}
	for _, kind := range []string{assessmentreadings.AssessCard, assessmentreadings.AssessApproval} {
		raw, err := assessmentreadings.ReadAssessment(ctx, pool, kind, "missing")
		ok(t, err)
		if string(raw) != "[]" {
			t.Fatalf("missing: %s", raw)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	var want json.RawMessage
	oldErr := pool.QueryRow(cancelled, `SELECT COALESCE(jsonb_agg(jsonb_build_object(
		  'event_type', event_type, 'body_json', body_json, 'at', at) ORDER BY seq), '[]')
		  FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note'`, streams[0]).Scan(&want)
	_, newErr := assessmentreadings.Events(cancelled, pool, streams[0])
	if oldErr == nil || newErr == nil || oldErr.Error() != newErr.Error() {
		t.Fatalf("cancel errors %v != %v", oldErr, newErr)
	}
}

func ok(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
