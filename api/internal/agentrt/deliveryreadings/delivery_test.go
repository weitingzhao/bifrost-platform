package deliveryreadings_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/deliveryreadings"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/deliveryview"
)

const adapterCommit = "9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d9d"

func adapterStages(t *testing.T, list []deliveryview.DeliveryStage) map[string]deliveryview.DeliveryStage {
	t.Helper()
	if len(list) != len(deliveryview.DeliveryStageOrder) {
		t.Fatalf("stages %+v", list)
	}
	out := map[string]deliveryview.DeliveryStage{}
	for i, s := range list {
		if s.Stage != deliveryview.DeliveryStageOrder[i] {
			t.Fatalf("stage %d is %s", i, s.Stage)
		}
		out[s.Stage] = s
	}
	return out
}

// The adapter must preserve unread versus empty, and every time and name the
// read model uses. The SQL-backed reads are exercised in the store's tests
// (delivery_external_test.go), where the migrated database is.
//
// Origin: moved with the code from store/delivery_external_test.go
// TestDeliveryViewAdapter (2026-11-17, the delivery read leaves the store).
func TestDeliveryViewAdapter(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	in := deliveryreadings.ExternalDelivery{Registrant: "thread:reader", Repo: "bifrost-platform", Commit: adapterCommit, NamedAt: at}
	unread := adapterStages(t, deliveryreadings.FoldExternalDelivery(in))
	if !strings.Contains(unread[deliveryview.StageLandingCI].Reason, "were not read") {
		t.Fatal(unread)
	}
	in.Runs = []deliveryreadings.Run{}
	empty := adapterStages(t, deliveryreadings.FoldExternalDelivery(in))
	if !strings.Contains(empty[deliveryview.StageLandingCI].Reason, "no completed CI") {
		t.Fatal(empty)
	}
	start := at.Add(time.Minute)
	in.Runs = []deliveryreadings.Run{{Run: "ci-adapter", Pipeline: "bifrost-ci-platform", Kind: "ci", Result: "Succeeded", Repo: "bifrost-platform",
		Revision: adapterCommit, CreatedAt: start, StartedAt: start, CompletedAt: start.Add(time.Minute)}}
	in.Production = &deliveryview.DeliveryProduction{Commit: adapterCommit, StartedAt: at.Add(3 * time.Minute), CompletedAt: at.Add(4 * time.Minute)}
	stages := adapterStages(t, deliveryreadings.FoldExternalDelivery(in))
	ci := stages[deliveryview.StageLandingCI]
	if ci.State != deliveryview.StageDone || !ci.EnteredAt.Equal(in.Runs[0].StartedAt) || !ci.LeftAt.Equal(in.Runs[0].CompletedAt) {
		t.Fatal(ci)
	}
	if h := stages[deliveryview.StageHandedIn]; h.Actor != in.Registrant || !h.EnteredAt.Equal(at) {
		t.Fatal(h)
	}
	if p := stages[deliveryview.StageOnProd]; p.State != deliveryview.StageDone || !p.EnteredAt.Equal(in.Production.CompletedAt) {
		t.Fatal(p)
	}
}

type noDB struct{ called bool }

func (d *noDB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	d.called = true
	return nil, errors.New("no database in this test")
}

// Unbound, the read refuses before it opens a transaction (fail safe: the
// lineage port is how it learns which work named the commit).
//
// Origin: 2026-11-17, the port declared when the delivery read left the store.
func TestReadDeliveryRefusesUnbound(t *testing.T) {
	db := &noDB{}
	_, err := deliveryreadings.ReadDelivery(context.Background(), db, deliveryreadings.DeliveryQuery{Commit: adapterCommit})
	if !errors.Is(err, deliveryreadings.ErrPortUnbound) || db.called {
		t.Fatalf("err %v, opened a transaction %v", err, db.called)
	}
}
