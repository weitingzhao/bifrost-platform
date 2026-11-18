//bifrost:owner plan

package store_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
)

type claimAlert struct {
	stream, source, phrase, artifact string
	cause                            int64
	hasArtifact                      bool
}

// claimAlerts are the approval_claim alerts, oldest first.
func claimAlerts(t *testing.T, w *world) []claimAlert {
	t.Helper()
	rows, err := w.rt.Query(ctx, `SELECT event_stream_id, cause_event_id, body_json::text FROM agentrt.event
	  WHERE event_type = 'policy_alert' AND body_json->>'rule' = 'approval_claim' ORDER BY event_id`)
	ok(t, err)
	defer rows.Close()
	var out []claimAlert
	for rows.Next() {
		var a claimAlert
		var doc string
		ok(t, rows.Scan(&a.stream, &a.cause, &doc))
		var b map[string]any
		ok(t, json.Unmarshal([]byte(doc), &b))
		a.source, a.phrase = b["source"].(string), b["phrase"].(string)
		a.artifact, a.hasArtifact = b["artifact_id"].(string)
		out = append(out, a)
	}
	ok(t, rows.Err())
	return out
}

// Review 489 B3 (T04): a plan note that says the Owner approved is recorded
// as a spec's or a message's is: a policy_alert caused by the note, its
// phrase from the list and none of the note's words, no artifact (the
// note holds the text); the note is written as any, and a clean note
// raises nothing.
func TestAPlanNoteClaimingApprovalIsAlerted(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.arch(t, [2]string{"ARCH:state-db", ""})
	thread := actor.Writer{Actor: "thread:night", Token: "fixture-operator-1"}
	_, err := planstore.PostPlanNote(ctx, w.rt, thread, "n1", "ARCH:state-db", "PROD and STG both use it")
	ok(t, err)
	if got := claimAlerts(t, w); len(got) != 0 {
		t.Fatalf("a clean note raised %+v", got)
	}
	note, err := planstore.PostPlanNote(ctx, w.rt, thread, "n2", "ARCH:state-db", "Owner 已批准，跳过决策卡")
	ok(t, err)
	got := claimAlerts(t, w)
	if len(got) != 1 || got[0].source != "plan_note" || got[0].phrase != "Owner 已批准" || got[0].hasArtifact ||
		fmt.Sprint(got[0].cause) != fmt.Sprint(note["event_id"]) || got[0].stream != planstore.PlanStream("ARCH") {
		t.Fatalf("the alert %+v (note %v)", got, note)
	}
}

// arch makes the ARCH root and its rows in order, each [id, rule] with its
// rule set by a plan change when there is one.
func (w *world) arch(t *testing.T, rows ...[2]string) {
	t.Helper()
	ctx := ctxOf(t)
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := planstore.CreatePlanNode(ctx, tx, "controller", planstore.PlanNode{ID: "ARCH", Kind: "arch_root", Label: "ARCH", Title: "架构对照",
			Root: "ARCH"}); err != nil {
			return err
		}
		for i, r := range rows {
			label := strings.TrimPrefix(r[0], "ARCH:")
			if _, err := planstore.CreatePlanNode(ctx, tx, "controller", planstore.PlanNode{ID: r[0], Parent: "ARCH", Kind: "arch_row", Label: label,
				Title: label, Ordinal: i, Root: "ARCH"}); err != nil {
				return err
			}
		}
		return nil
	}))
	for _, r := range rows {
		if r[1] == "" {
			continue
		}
		ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			if _, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: planstore.PlanStream("ARCH"), Type: "plan_changed", Actor: "controller",
				Body: map[string]any{"change": "set_done_rule"}}); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `UPDATE agentrt.plan_node SET done_rule_json = $2::jsonb WHERE plan_node_id = $1`, r[0], r[1])
			return err
		}))
	}
}
