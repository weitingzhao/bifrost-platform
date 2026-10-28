package store_test

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// S3 B3 (CUT §11.6 v3): the card records' order, uniqueness and shapes.

var cardStream = store.CardsStream("bifrost-platform")

func putCard(w *world, actor, stream, name string, body map[string]any) error {
	body["v"] = "1"
	return store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := store.Append(ctx, tx, store.Event{Stream: stream, Type: "note", Actor: actor, Body: map[string]any{"text": "card", name: body}})
		return err
	})
}

func preparedCard(root, kind string, seq int64, n int, options map[string]any) map[string]any {
	digest := strings.Repeat("a", 64)
	b := map[string]any{"prep": store.CardPrepID(kind, seq, n), "repo": "bifrost-platform", "kind": kind, "trigger_seq": seq, "n": n,
		"template_version": 1, "key": digest, "request_fp": digest, "request": map[string]any{"title": "x"}, "options": options}
	if kind == store.CardRepoStopped {
		b["stop_generation"] = 1
	} else {
		b["root"], b["op"] = root, "land:"+root+":1"
	}
	return b
}

func TestLandingCardRecordsFollowTheirOrder(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	root := w.ready(t, store.Task{OffPlanReason: "card records"})
	opts := map[string]any{"a": "try_again", "b": "do_not_merge"}
	prep := store.CardPrepID(store.CardRootFailed, 7, 1)
	ok(t, putCard(w, "controller", cardStream, "landing_card_prepared", preparedCard(root, store.CardRootFailed, 7, 1, opts)))
	refused := func(name string, body map[string]any, why string) {
		t.Helper()
		if err := putCard(w, "controller", cardStream, name, body); err == nil {
			t.Fatalf("%s accepted: %s", name, why)
		}
	}
	refused("landing_card_prepared", preparedCard(root, store.CardRootFailed, 7, 1, opts), "prepared twice")
	refused("landing_card", map[string]any{"prep": prep, "question_id": "q1"}, "bound before its ask started")
	refused("landing_card_acked", map[string]any{"prep": prep, "question_id": "q1"}, "acked before bound")
	ok(t, putCard(w, "controller", cardStream, "landing_card_ask_started", map[string]any{"prep": prep}))
	refused("landing_card_ask_started", map[string]any{"prep": prep}, "ask started twice")
	ok(t, putCard(w, "controller", cardStream, "landing_card", map[string]any{"prep": prep, "question_id": "q1"}))
	refused("landing_card", map[string]any{"prep": prep, "question_id": "q2"}, "bound twice")
	// Another card cannot bind the same question.
	other := store.CardPrepID(store.CardRootFailed, 9, 1)
	ok(t, putCard(w, "controller", cardStream, "landing_card_prepared", preparedCard(root, store.CardRootFailed, 9, 1, opts)))
	ok(t, putCard(w, "controller", cardStream, "landing_card_ask_started", map[string]any{"prep": other}))
	refused("landing_card", map[string]any{"prep": other, "question_id": "q1"}, "a question bound to two cards")
	refused("landing_card_acked", map[string]any{"prep": prep, "question_id": "q2"}, "acked another question")
	ok(t, putCard(w, "controller", cardStream, "landing_card_acked", map[string]any{"prep": prep, "question_id": "q1"}))
	refused("landing_card_closed", map[string]any{"prep": prep, "why": "expired"}, "closed after it ended")
	var cards map[string]store.CardRecord
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		var err error
		cards, err = store.LandingCards(ctx, tx, cardStream)
		return err
	}))
	c := cards[prep]
	if !c.AskStarted || !c.Bound || !c.Acked || !c.Ended() || c.QuestionID != "q1" || c.Kind != store.CardRootFailed || c.Root != root ||
		c.TriggerSeq != 7 || c.N != 1 || c.Options["a"] != "try_again" || c.Options["b"] != "do_not_merge" {
		t.Fatalf("%+v", c)
	}
	if o := cards[other]; o.Bound || !o.AskStarted || o.Ended() {
		t.Fatalf("%+v", o)
	}
}

func TestLandingCardRecordShapes(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	root := w.ready(t, store.Task{OffPlanReason: "card shapes"})
	good := map[string]any{"a": "do_not_merge", "b": "wait"}
	for name, c := range map[string]struct {
		actor, stream string
		body          map[string]any
	}{
		"not the controller":   {"owner", cardStream, preparedCard(root, store.CardFloor, 3, 1, good)},
		"repository stream":    {"controller", "landing:bifrost-platform", preparedCard(root, store.CardFloor, 3, 1, good)},
		"another repo's":       {"controller", store.CardsStream("bifrost-trade-api"), preparedCard(root, store.CardFloor, 3, 1, good)},
		"options not the kind": {"controller", cardStream, preparedCard(root, store.CardFloor, 3, 1, map[string]any{"a": "try_again", "b": "do_not_merge"})},
		"one option":           {"controller", cardStream, preparedCard(root, store.CardFloor, 3, 1, map[string]any{"a": "do_not_merge"})},
		"prep not its id": {"controller", cardStream, func() map[string]any {
			b := preparedCard(root, store.CardFloor, 3, 1, good)
			b["prep"] = "card:3:2:floor"
			return b
		}()},
		"repo card with a root": {"controller", cardStream, func() map[string]any {
			b := preparedCard(root, store.CardRepoStopped, 4, 1, map[string]any{"a": "repo_resume", "b": "keep_stopped"})
			b["root"] = root
			return b
		}()},
		"root card without a root": {"controller", cardStream, func() map[string]any {
			b := preparedCard(root, store.CardFloor, 3, 1, good)
			delete(b, "root")
			return b
		}()},
		"not a digest": {"controller", cardStream, func() map[string]any {
			b := preparedCard(root, store.CardFloor, 3, 1, good)
			b["key"] = "landing/root/op"
			return b
		}()},
	} {
		if err := putCard(w, c.actor, c.stream, "landing_card_prepared", c.body); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	ok(t, putCard(w, "controller", cardStream, "landing_card_prepared", preparedCard(root, store.CardFloor, 3, 1, good)))
	ok(t, putCard(w, "controller", cardStream, "landing_card_prepared", preparedCard(root, store.CardRepoStopped, 4, 1, map[string]any{"a": "repo_resume", "b": "keep_stopped"})))
	if err := putCard(w, "controller", cardStream, "landing_card_closed", map[string]any{"prep": store.CardPrepID(store.CardFloor, 3, 1), "why": "bored"}); err == nil {
		t.Error("a closing reason outside the list")
	}
	ok(t, putCard(w, "controller", cardStream, "landing_card_closed", map[string]any{"prep": store.CardPrepID(store.CardFloor, 3, 1), "why": "unconfirmed"}))
}
