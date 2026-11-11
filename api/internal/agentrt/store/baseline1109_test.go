package store_test

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"testing"
)

func TestBaseline1109Import(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	n, err := store.ImportBaseline1109(ctx, w.rt)
	ok(t, err)
	if n != 3 {
		t.Fatalf("imported %d rows", n)
	}
	var events, artifacts int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event`).Scan(&events))
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.artifact`).Scan(&artifacts))
	n, err = store.ImportBaseline1109(ctx, w.rt)
	ok(t, err)
	if n != 0 {
		t.Fatalf("second import wrote %d rows", n)
	}
	var after int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event`).Scan(&after))
	if after != events {
		t.Fatal("second import wrote events")
	}
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.artifact`).Scan(&after))
	if after != artifacts {
		t.Fatal("second import wrote artifacts")
	}
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.metric_reading r JOIN agentrt.artifact_blob b ON b.artifact_id=r.evidence_artifact_id WHERE r.label='baseline:pre-group-0' AND r.source='imported' AND NOT r.conclusive AND convert_from(b.body,'UTF8') LIKE '%Raw data is gone%'`).Scan(&after))
	if after != 3 {
		t.Fatalf("evidenced imports: %d", after)
	}
}
