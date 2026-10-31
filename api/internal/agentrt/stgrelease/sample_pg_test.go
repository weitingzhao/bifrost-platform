package stgrelease

import (
	"context"
	"slices"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"k8s.io/apimachinery/pkg/types"
)

// Serial: one migrated PostgreSQL database. The production ledger end to end
// with the real adapter on a fake cluster: the batch is the allocation's
// stored event id, the archive is the committed record the finalizer removal
// reads, and a second sample is refused until the first is released.
func TestSamplePGCreateAndFinalize(t *testing.T) {
	db := agentrttest.Migrated(t)
	if _, err := store.Seed(context.Background(), db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	pg := PostgresSampleLedger{DB: db.Connect(t, db.Runtime), Tenant: SampleTenant}
	w := newSampleWorld(t)
	w.sampler.Ledger = pg
	w.sampler.Runs.(*delivery.STGReleaseAdapter).Archive = pg
	out, err := w.create("approval-pg-create")
	if err != nil {
		t.Fatal(err)
	}
	conn := db.Connect(t, db.Runtime)
	var eventID int64
	if err := conn.QueryRow(context.Background(), `SELECT event_id FROM agentrt.event WHERE event_stream_id=$1 AND seq=1`, store.StgSampleStream).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	s, err := pg.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	x := s.Samples["approval-pg-create"]
	if x.UID != out.UID || out.Batch != eventID || x.Allocation != eventID || out.IntentSeq != 1 || w.creates != 1 {
		t.Fatalf("recorded %+v result %+v", x, out)
	}
	if _, err := w.create("approval-pg-second"); err == nil {
		t.Fatal("second sample while the first is open")
	}
	if archived, err := pg.HasRunEvidence(context.Background(), delivery.STGRunIdentity{Namespace: delivery.STGReleaseNamespace, Name: out.Run, UID: types.UID(sampleUIDOf(1))}); err != nil || archived {
		t.Fatal("archive receipt before finalize", archived, err)
	}
	w.finish(out.Run, "Succeeded")
	done, err := w.finalize("approval-pg-finalize", out.Run, out.UID)
	if err != nil || !done.Released || slices.Contains(w.run(out.Run).GetFinalizers(), delivery.STGReleaseFinalizer) {
		t.Fatal("finalize", done, err)
	}
	s, err = pg.Read(context.Background())
	if err != nil || s.Open() != nil || s.Samples["approval-pg-create"].ArchivedSeq >= s.Samples["approval-pg-create"].ReleasedSeq {
		t.Fatalf("closed sample %+v %v", s.Samples["approval-pg-create"], err)
	}
}
