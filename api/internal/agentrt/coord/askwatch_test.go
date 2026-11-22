package coord_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/coord"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	_ "github.com/weitingzhao/bifrost-platform/api/internal/bus/registry" // declares seat.ask.overdue
)

// Origin: 2026-11-21 05:5xZ, the Bus page's "waiting for an answer" panel had
// 8 waiting links, 7 past 60 minutes, the oldest 605 minutes, and nothing told
// the coordinator, whose job is chasing. An open ask past 4 hours is overdue;
// two seats with open asks to each other are a cycle at once; a fresh ask is
// neither.
func TestAnOpenAskIsOverdueAfterFourHoursOrInACycle(t *testing.T) {
	now := time.Date(2026, 11, 21, 6, 0, 0, 0, time.UTC)
	got := coord.Overdue([]coord.OpenAsk{
		{ID: "m_1", From: "thread:a", To: "thread:b", At: now.Add(-5 * time.Hour)},
		{ID: "m_2", From: "thread:c", To: "thread:d", At: now.Add(-10 * time.Minute)},
		{ID: "m_3", From: "thread:d", To: "thread:c", At: now.Add(-5 * time.Minute)},
		{ID: "m_4", From: "thread:e", To: "thread:f", At: now.Add(-time.Hour)},
	}, now, coord.AskOverdueAfter)
	var s []string
	for _, o := range got {
		s = append(s, o.ID+":"+o.Reason)
	}
	if len(s) != 3 || s[0] != "m_1:overdue" || s[1] != "m_2:cycle" || s[2] != "m_3:cycle" {
		t.Fatalf("got %v, want m_1:overdue m_2:cycle m_3:cycle", s)
	}
	if err := eventlog.CheckBody(coord.AskOverdueType, coord.AskOverdueBody(got[0])); err == nil {
		t.Fatal("a body with an id that is no message id passed the register (positive control below)")
	}
	ok := coord.OverdueAsk{OpenAsk: coord.OpenAsk{ID: "m_01M80X4KH09N8MR3T2QM1GQN0D", From: "thread:local_a", To: "thread:local_b", At: now}, Reason: "overdue"}
	if err := eventlog.CheckBody(coord.AskOverdueType, coord.AskOverdueBody(ok)); err != nil {
		t.Fatalf("a real overdue ask refused by the register: %v", err)
	}
}

// Same origin: the watch says each overdue ask once, however often it reads,
// and an ask answered by any later letter back is not said.
func TestAnOverdueAskIsSaidOncePG(t *testing.T) {
	t.Parallel()
	_, pool, a := conclusionDB(t)
	asked, err := coord.SendMessage(ctx, pool, a, "owner", "ask", []byte("may I"), "")
	ok2(t, err)
	answered, err := coord.SendMessage(ctx, pool, a, "owner", "ask", []byte("and this"), "")
	ok2(t, err)
	ok2(t, eventlog.InTx(ctx, pool, func(tx pgx.Tx) error {
		_, err := coord.PostMessage(ctx, tx, "owner", a.Actor, "answer", []byte("yes"))
		return err
	}))
	later := time.Now().Add(5 * time.Hour)
	n1, err := coord.FlagOverdueAsks(ctx, pool, later)
	ok2(t, err)
	n2, err := coord.FlagOverdueAsks(ctx, pool, later.Add(time.Hour))
	ok2(t, err)
	if n1 != 0 || n2 != 0 {
		// Both asks were answered by the one letter back (any later letter
		// from the recipient answers every earlier ask to it).
		t.Fatalf("said %d then %d, want 0 and 0: the owner wrote back after both asks (%s, %s)", n1, n2, asked, answered)
	}
	third, err := coord.SendMessage(ctx, pool, a, "owner", "ask", []byte("one more"), "")
	ok2(t, err)
	n3, err := coord.FlagOverdueAsks(ctx, pool, later)
	ok2(t, err)
	n4, err := coord.FlagOverdueAsks(ctx, pool, later.Add(time.Hour))
	ok2(t, err)
	if n3 != 1 || n4 != 0 {
		t.Fatalf("said %d then %d for %s, want 1 then 0", n3, n4, third)
	}
}
