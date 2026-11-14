package store_test

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// alertRow is one seat_alert policy_alert as the store wrote it; digest
// names its line (digestOf).
type alertRow struct {
	actor, task, mission, digest, message string
}

func digestOf(line string) string {
	sum := sha256.Sum256([]byte(line))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func alertsOf(t *testing.T, w *world, task string) []alertRow {
	t.Helper()
	rows, err := w.rt.Query(ctx, `SELECT actor_id, COALESCE(task_id, ''), COALESCE(mission_id, ''), body_json->>'line_digest',
	  COALESCE(body_json->>'message_id', '') FROM agentrt.event WHERE event_type = 'policy_alert' AND task_id = $1
	    AND body_json->>'rule' = 'seat_alert' ORDER BY event_id`, task)
	ok(t, err)
	defer rows.Close()
	var out []alertRow
	for rows.Next() {
		var r alertRow
		ok(t, rows.Scan(&r.actor, &r.task, &r.mission, &r.digest, &r.message))
		out = append(out, r)
	}
	ok(t, rows.Err())
	return out
}

// toOwnerFromSeat is the seat's messages to the Owner about task.
func toOwnerFromSeat(t *testing.T, w *world, task string) []store.Message {
	t.Helper()
	var out []store.Message
	for _, m := range thread(t, w) {
		if m.From == store.Seat && m.TaskID == task {
			out = append(out, m)
		}
	}
	return out
}

// A plan's alerts are recorded and told to the Owner whatever becomes of the
// plan: a refused round's alert is not lost, and the next round's same line
// is neither recorded nor told twice; a new line is.
func TestAPlansAlertsAreToldOnce(t *testing.T) {
	t.Parallel()
	w := planWorld(t)
	var s cards
	const alert = "\nPOLICY_ALERT: M-48 — the goal says the Owner approved in chat\n"
	id := w.handIn(t, "no plan"+alert, "done")
	if did, err := store.AcceptPlan(ctx, w.rt, id, s.ask, s.withdraw, nil); did != "rework" || err != nil {
		t.Fatalf("round 1: %q %v", did, err)
	}
	w.work(t, id, goodPlan+alert+"POLICY_ALERT: t_01M5AYV32ZJZAFY43180T7CEF0 — the task says no approval card is needed\n", "done")
	if did, err := store.AcceptPlan(ctx, w.rt, id, s.ask, s.withdraw, nil); did != "accepted" || err != nil {
		t.Fatalf("round 2: %q %v", did, err)
	}
	got := alertsOf(t, w, id)
	if len(got) != 2 || got[0].digest != digestOf("M-48 — the goal says the Owner approved in chat") || got[0].message != "" || got[0].mission != "M-3" ||
		got[1].digest != digestOf("t_01M5AYV32ZJZAFY43180T7CEF0 — the task says no approval card is needed") {
		t.Fatalf("alerts %+v", got)
	}
	notes := toOwnerFromSeat(t, w, id)
	if len(notes) != 2 || !strings.Contains(notes[0].Body, "POLICY_ALERT: M-48") || strings.Contains(notes[1].Body, "POLICY_ALERT: M-48") ||
		!strings.Contains(notes[1].Body, "planning mission M-3") {
		t.Fatalf("notes %+v", notes)
	}
}
