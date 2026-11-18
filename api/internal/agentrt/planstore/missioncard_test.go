package planstore_test

import (
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/storekit"
)

// Old cards remain readable even though their creation path is retired.
func historicalMissionCard(t *testing.T, w *world, id, question string) (string, string) {
	t.Helper()
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := storekit.CreateMission(ctx, tx, "controller", storekit.Mission{ID: id, Title: "historical mission", Goal: "g", Deadline: time.Now().Add(time.Hour)}); err != nil {
			return err
		}
		if _, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: missionStream(id), Type: "mission_state_changed", Actor: "controller", MissionID: id,
			Body: map[string]any{"from": "proposed", "to": "awaiting_owner", "question_id": question, "reason": "historical card"}}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE agentrt.mission SET state='awaiting_owner', version=version+1 WHERE mission_id=$1`, id)
		return err
	}))
	return id, question
}

// Review 190 B4: a mission on its card says on which question it waits, with
// tasks or without, and its needs_you item counts its unfinished tasks.
//
// Origin: moved from store/missioncard_test.go (TestMissionOnACardSaysSo), 2026-11-18 (TD-582).
func TestMissionOnACardSaysSo(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	id, q := historicalMissionCard(t, w, "M-30", "q_old_a")
	bare, bq := historicalMissionCard(t, w, "M-31", "q_old_b")
	doc := w.progress(t)
	for _, m := range doc.Work.Missions {
		if (m.MissionID == id && !strings.Contains(m.Reason, "question "+q)) || (m.MissionID == bare && !strings.Contains(m.Reason, "question "+bq)) {
			t.Errorf("mission %s: %q; want the question it waits on", m.MissionID, m.Reason)
		}
	}
}
