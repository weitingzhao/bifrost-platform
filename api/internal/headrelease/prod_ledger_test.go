package headrelease

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// Origin: 2026-11-21, CI line. The PROD head-release loop's reasons for
// holding a release (which of ProdConditions failed) lived in the workers'
// log and own audit file only; the ledger's audit event kept "wait" and no
// reason, so the CI line's list of what the system cannot see had it wrong
// as archived. Each judgement now goes to the ledger with its record, and
// reads back newest first; a detail that is not JSON is kept as text.
// 2026-11-22: the first version decoded the record with json.Unmarshal, so
// "commits": 15 became a float the ledger refuses, and every judgement that
// had a number in it was lost (PROD log: "canon: only integers are allowed");
// this test's record had no number. It has one now, and one with a fraction,
// which is kept as text.
func TestProdJudgementsGoToTheLedgerWithTheirReasons(t *testing.T) {
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	if _, err := actor.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	pool := db.Pool(t, db.Runtime)
	first := `{"would_release":false,"verdict":"wait: soaked","commits":15,"failing":["soaked"],"reasons":{"soaked":"stg has run cc59bf9f for 17s of 600s"}}`
	if err := NoteProdJudged(ctx, pool, "wait", first); err != nil {
		t.Fatal(err)
	}
	if err := NoteProdJudged(ctx, pool, "wait", `{"verdict":"wait","ratio":0.5}`); err != nil {
		t.Fatal(err)
	}
	if err := NoteProdJudged(ctx, pool, "error", "not json"); err != nil {
		t.Fatal(err)
	}
	got, err := ProdJudgements(ctx, pool, time.Now().Add(-time.Hour), 10)
	if err != nil || len(got) != 3 {
		t.Fatalf("judgements %+v, %v", got, err)
	}
	var text map[string]string
	if got[0].Result != "error" || json.Unmarshal(got[0].Record, &text) != nil || text["text"] != "not json" {
		t.Fatalf("newest %+v", got[0])
	}
	if json.Unmarshal(got[1].Record, &text) != nil || text["text"] != `{"verdict":"wait","ratio":0.5}` {
		t.Fatalf("a fraction is kept as text: %+v", got[1])
	}
	var rec struct {
		Commits int               `json:"commits"`
		Failing []string          `json:"failing"`
		Reasons map[string]string `json:"reasons"`
	}
	if got[2].Result != "wait" || json.Unmarshal(got[2].Record, &rec) != nil || rec.Commits != 15 || len(rec.Failing) != 1 || rec.Reasons["soaked"] == "" {
		t.Fatalf("oldest %+v", got[2])
	}
}

// Origin: 2026-11-22, law 93 (seen and felt): the judgements were in the ledger
// and nothing showed them; the route is the read. A limit out of range is a
// 400, and with no database it says so (503), never an empty list.
func TestProdJudgementsRouteRefusesWhatItCannotAnswer(t *testing.T) {
	rt := ProdJudgementsRoute{DB: func() eventlog.DB { return nil }}
	for _, c := range []struct {
		query string
		code  int
	}{{"?limit=0", http.StatusBadRequest}, {"?limit=201", http.StatusBadRequest}, {"?limit=x", http.StatusBadRequest}, {"", http.StatusServiceUnavailable}} {
		w := httptest.NewRecorder()
		rt.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/delivery/prod-judgements"+c.query, nil))
		if w.Code != c.code {
			t.Fatalf("%q: %d %s", c.query, w.Code, w.Body.String())
		}
	}
}
