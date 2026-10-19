package server

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// encoded is a body's size as the question store counts it: JSON with its
// quotes, HTML not escaped; the store takes at most 3000.
func encoded(t *testing.T, s string) int {
	t.Helper()
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		t.Fatal(err)
	}
	return len(strings.TrimSuffix(b.String(), "\n"))
}

// A mission's decision card on the real question store and a real database
// (Owner 2026-10-15 「决策卡 A」; reviews 192 A1, A4, 193 A5 and its B): the
// card names its two options, the goal duty, no default and how soon; a
// retry after the card was made and the proposal failed gets the same card
// although the mission's number moved; two proposals with the same words get
// two cards; the answer read back starts the mission and is acknowledged;
// the longest card a proposal within the bounds makes fits the store, and
// one that would not is refused before the store is asked.
func TestMissionCardOnTheStore(t *testing.T) {
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	if _, err := store.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	conn := db.Connect(t, db.Runtime)
	rt := newQuestionRuntime(filepath.Join(t.TempDir(), "questions"), nil)
	ask, read, ack := rt.agentRuntimeAsk(), rt.agentRuntimeQuestion(), rt.agentRuntimeAck()
	thread := store.Writer{Actor: "thread:s1", Token: "operator"}
	p := store.MissionProposal{Title: "Review the cards", Goal: "核对任务卡片并确认授权范围。", Success: []string{"done"},
		Repos: []string{"bifrost-platform"}, WorkID: "W-31"}

	var made string
	failing := func(ctx context.Context, c store.Card) (string, error) {
		id, err := ask(ctx, c)
		if err != nil {
			return "", err
		}
		made = id
		return "", errors.New("the database went away after the card was made")
	}
	if _, err := store.ProposeMission(ctx, conn, thread, "k1", p, failing); !errors.Is(err, store.ErrAsk) || made == "" {
		t.Fatalf("the first try: %v (card %q)", err, made)
	}
	// Another mission takes the number the first try had.
	if err := store.InTx(ctx, conn, func(tx pgx.Tx) error {
		_, err := store.CreateMission(ctx, tx, "controller", store.Mission{ID: "M-1", Title: "x", Goal: "g", Deadline: time.Now().Add(time.Hour)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	out, err := store.ProposeMission(ctx, conn, thread, "k1", p, ask)
	if err != nil || out["question_id"] != made || out["mission_id"] != "M-2" || out["state"] != "awaiting_owner" {
		t.Fatalf("the retry: %v %v; want M-2 on card %s", out, err, made)
	}
	got, ok, err := rt.svc.Lookup(made)
	if err != nil || !ok || len(got.Options) != 2 || got.Options[0].Label != store.MissionStart || got.Options[1].Label != store.MissionNotStart ||
		got.OwnerDuty != "goal" || got.Recommended != "" || got.TaskID != "" || !strings.Contains(got.Urgency, "only this mission") ||
		got.Meanwhile != "the mission does not start" || !strings.Contains(got.Body, "核对任务卡片") || got.Title != "Mission: Review the cards" {
		t.Fatalf("the card %+v %v %v", got, ok, err)
	}
	other, err := store.ProposeMission(ctx, conn, thread, "k2", p, ask)
	if err != nil || other["state"] != "awaiting_owner" || other["question_id"] == made {
		t.Fatalf("a second proposal with the same words: %v %v", other, err)
	}
	raw, _ := json.Marshal(map[string]any{"option": "a", "channel": "console"})
	if res := rt.svc.Answer(ctx, "owner-console", "admin", "admin", made, raw); res.Code >= 300 {
		t.Fatalf("answer = %d %v", res.Code, res.Body)
	}
	n, err := store.DecideMissions(ctx, conn, read, ack)
	if err != nil || n != 1 {
		t.Fatalf("decided %d: %v", n, err)
	}
	var state string
	if err = conn.QueryRow(ctx, `SELECT state FROM agentrt.mission WHERE mission_id = 'M-2'`).Scan(&state); err != nil || state != "active" {
		t.Fatalf("M-2 is %s (%v); want active", state, err)
	}
	if got, ok, err = rt.svc.Lookup(made); err != nil || !ok || got.Answer == nil || got.Answer.Read == nil {
		t.Fatalf("the answer was not acknowledged: %+v %v %v", got.Answer, ok, err)
	}

	// The longest card: quotes cost two bytes each once encoded.
	fresh := newQuestionRuntime(filepath.Join(t.TempDir(), "long"), nil)
	asked := 0
	counting := func(ctx context.Context, c store.Card) (string, error) {
		asked++
		return fresh.agentRuntimeAsk()(ctx, c)
	}
	long := store.MissionProposal{Title: "m", Goal: strings.Repeat("\"", 1150), Success: []string{"done"}}
	out, err = store.ProposeMission(ctx, conn, thread, "long", long, counting)
	if err != nil || out["state"] != "awaiting_owner" {
		t.Fatalf("the longest card: %v %v", out, err)
	}
	if card, found, lerr := fresh.svc.Lookup(out["question_id"].(string)); lerr != nil || !found || encoded(t, card.Body) < 2700 || encoded(t, card.Body) > 3000 {
		t.Fatalf("the longest card is %d encoded bytes (%v %v); want close to and within 3000", encoded(t, card.Body), found, lerr)
	}
	long.Success = append(long.Success, strings.Repeat("\\", store.MaxMissionSuccessBytes))
	_, err = store.ProposeMission(ctx, conn, thread, "longer", long, counting)
	if !errors.Is(err, store.ErrInvalid) || !strings.Contains(err.Error(), "would be") || asked != 1 {
		t.Fatalf("a card too long: %v, the store asked %d times; want refused before asking", err, asked)
	}
}
