package store

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// SQL transport only; effectiveRule, canonical replay and trigger selection
// run unchanged. A disabled read cannot even reach the release ledger.
type stgCardReadTx struct {
	stgGateReadTx
	entries []StgEntry
	reads   int
}

func (tx *stgCardReadTx) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	if !strings.Contains(sql, "FROM agentrt.event") || len(args) != 1 || args[0] != StgReleaseStream {
		tx.t.Fatalf("unexpected query %s", sql)
	}
	tx.reads++
	return &stgCardReadRows{entries: tx.entries}, nil
}

type stgCardReadRows struct {
	pgx.Rows
	entries []StgEntry
	n       int
}

func (r *stgCardReadRows) Next() bool { r.n++; return r.n <= len(r.entries) }
func (*stgCardReadRows) Close()       {}
func (*stgCardReadRows) Err() error   { return nil }
func (r *stgCardReadRows) Scan(dest ...any) error {
	e := r.entries[r.n-1]
	body, err := canon.Encode(e.Event.Body)
	if err != nil {
		return err
	}
	return grantIdentityAssign(dest, e.Seq, e.At, e.Event.Type, e.Event.Actor, body, e.Event.TaskID, e.Event.MissionID, e.Event.PlanNode, e.Event.AttemptID)
}

// Serial: FreezeEventClock; paired none/enabled through the same SQL entry.
func TestStgCardsNoneDoesNotReadLedgerOrEmitTriggers(t *testing.T) {
	h := stgExhausted(t)
	block := h.block("budget_exhausted", stgTestMember())
	defer FreezeEventClock(h.at)()
	for _, mode := range []string{"none", "bifrost-deliver-platform"} {
		tx := &stgCardReadTx{stgGateReadTx: stgGateReadTx{t: t, at: h.at}, entries: h.entries}
		tx.text = fmt.Sprintf("rule_id: landing:dev\nrule_version: 1\ndirection: initial\neffective_at: %s\nexpires_at: %s\ntenant: dev\nrepos:\n  - repo: bifrost-platform\n    allow: [\"console/src/components/**/*.tsx\"]\n    deny: []\n    stg_release: %s\nlimits: {max_files: 10, max_changed_lines: 300, max_landings_per_day: 5, max_in_flight_per_repo: 1}\n", h.at.Add(-time.Hour).Format(time.RFC3339), h.at.Add(time.Hour).Format(time.RFC3339), mode)
		var err error
		tx.rule, err = rules.Parse([]byte(tx.text))
		if err != nil {
			t.Fatal(err)
		}
		tx.body, err = tx.rule.BodyJSON()
		if err != nil {
			t.Fatal(err)
		}
		triggers, err := stgCardTriggers(context.Background(), tx, "bifrost-platform")
		if err != nil {
			t.Fatal(err)
		}
		if mode == "none" {
			if len(triggers) != 0 || tx.reads != 0 {
				t.Fatal("none read/emitted", triggers, tx.reads)
			}
		} else if len(triggers) != 1 || triggers[0].Seq != block.Seq || tx.reads != 1 {
			t.Fatal("enabled trigger missing", triggers, tx.reads)
		}
	}
}
