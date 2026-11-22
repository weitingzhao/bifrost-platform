package headrelease

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
)

// Origin: 2026-11-21, CI line. The PROD head-release loop's reasons for
// holding a release (which of ProdConditions failed) lived in the workers'
// log and own audit file only; the ledger's audit event kept "wait" and no
// reason, so the CI line's list of what the system cannot see had it wrong
// as archived. Each judgement now goes to the ledger with its record, and
// reads back newest first; a detail that is not JSON is kept as text.
func TestProdJudgementsGoToTheLedgerWithTheirReasons(t *testing.T) {
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	if _, err := actor.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	pool := db.Pool(t, db.Runtime)
	first := `{"would_release":false,"verdict":"wait: soaked","failing":["soaked"],"reasons":{"soaked":"stg has run cc59bf9f for 17s of 600s"}}`
	if err := NoteProdJudged(ctx, pool, "wait", first); err != nil {
		t.Fatal(err)
	}
	if err := NoteProdJudged(ctx, pool, "error", "not json"); err != nil {
		t.Fatal(err)
	}
	got, err := ProdJudgements(ctx, pool, time.Now().Add(-time.Hour), 10)
	if err != nil || len(got) != 2 {
		t.Fatalf("judgements %+v, %v", got, err)
	}
	var text map[string]string
	if got[0].Result != "error" || json.Unmarshal(got[0].Record, &text) != nil || text["text"] != "not json" {
		t.Fatalf("newest %+v", got[0])
	}
	var rec struct {
		Failing []string          `json:"failing"`
		Reasons map[string]string `json:"reasons"`
	}
	if got[1].Result != "wait" || json.Unmarshal(got[1].Record, &rec) != nil || len(rec.Failing) != 1 || rec.Reasons["soaked"] == "" {
		t.Fatalf("oldest %+v", got[1])
	}
}
