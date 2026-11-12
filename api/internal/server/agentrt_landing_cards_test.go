package server

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// S3 B3 (review 668 B-04): every landing card kind's real request is taken
// by the real question store (two options, 72 hours, free text), found by
// its key without side effects, read, withdrawn, and a second withdrawal
// says what it became.
func TestLandingCardRequestsFitTheQuestionStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := "t_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	at := time.Now().UTC()
	cases := []store.CardTrigger{
		{Kind: store.CardRootFailed, Seq: 1, At: at, Root: root, Op: "land:x:1", Why: "stale", Choices: []string{"try_again", "do_not_merge"}},
		{Kind: store.CardRootFailed, Seq: 2, At: at, Root: root, Op: "land:x:1", Why: "conflict", Choices: []string{"do_not_merge", "wait"}},
		{Kind: store.CardFloor, Seq: 3, At: at, Root: root, Op: "land:x:1", Why: "floor", Choices: []string{"do_not_merge", "wait"}},
		{Kind: store.CardOkMissing, Seq: 4, At: at, Root: root, Op: "land:x:1", Choices: []string{"confirm_main_lost", "wait"}},
		{Kind: store.CardLateLanding, Seq: 5, At: at, Root: root, Op: "land:x:1", Choices: []string{"ack", "remind_later"}},
		{Kind: store.CardRepoStopped, Seq: 6, At: at, StopGeneration: 1, Choices: []string{"repo_resume", "keep_stopped"}},
		{Kind: store.CardCIStuck, Seq: 7, At: at, Root: root, Op: "land:x:1", Why: "ci_stuck", Choices: []string{"ack", "remind_later"}},
		{Kind: store.CardParkedLong, Seq: 8, At: at, Root: root, Op: "land:x:1", Why: "window", Choices: []string{"ack", "remind_later"}},
		{Kind: store.CardLandedUnverified, Seq: 9, At: at, Root: root, Op: "land:x:1", Why: "unknown", Choices: []string{"confirm_not_delivered", "wait"}},
		{Kind: store.CardInMain, Seq: 10, At: at, Root: root, Op: "land:x:1", Why: "in_main", Choices: []string{"count_as_delivered", "confirm_not_delivered"}},
		{Family: "stg", Kind: store.CardSTGFailed, Seq: 11, At: at, Block: "0123456789abcdef", Choices: []string{"retry_release", "do_not_release"}},
		{Family: "stg", Kind: store.CardSTGBlocked, Seq: 12, At: at, Block: "1123456789abcdef", Choices: []string{"ack", "remind_later"}},
		{Family: "stg", Kind: store.CardSTGBlocked, Seq: 13, At: at, Batch: "stg:bifrost-platform:1", Choices: []string{"ack", "remind_later", "accept_unreconciled"}},
		{Family: "stg", Kind: store.CardSTGIneligible, Seq: 14, At: at, Block: "2123456789abcdef", Choices: []string{"ack", "remind_later"}},
	}
	for i, tr := range cases {
		// A store of its own per card.
		rt := newQuestionRuntime(filepath.Join(t.TempDir(), "store.json"), nil)
		q := rt.landingCardQuestions()
		key := "landingcardkey" + string(rune('a'+i)) + "0123456789abcdef0123456789abcdef"
		req := store.LandingCardRequest("bifrost-platform", tr, "Landing: a title")
		id, created, err := q.AskRaw(ctx, key, req)
		if err != nil || id == "" || created.IsZero() {
			t.Fatalf("%s: ask %q %v %v", tr.Kind, id, created, err)
		}
		again, _, err := q.AskRaw(ctx, key, req)
		if err != nil || again != id {
			t.Fatalf("%s: the same request asked again made %q (%v)", tr.Kind, again, err)
		}
		found, _, ok, err := q.LookupKey(ctx, key, req)
		if err != nil || !ok || found.ID != id {
			t.Fatalf("%s: lookup by key %+v %v %v", tr.Kind, found, ok, err)
		}
		// The same key holding another request is a mismatch (review 670 B-01).
		other := store.LandingCardRequest("bifrost-platform", tr, "Landing: another title")
		if _, _, ok, err := q.LookupKey(ctx, key, other); ok || !errors.Is(err, store.ErrCardMismatch) {
			t.Fatalf("%s: another request under the key: %v %v", tr.Kind, ok, err)
		}
		read, ok, err := q.Lookup(ctx, id)
		if err != nil || !ok || read.Status != "open" || read.Options != len(tr.Choices) || read.ExpiresAt.Sub(created) != 72*time.Hour {
			t.Fatalf("%s: read %+v %v %v", tr.Kind, read, ok, err)
		}
		if st, err := q.Withdraw(ctx, id, "moved on"); err != nil || st != "withdrawn" {
			t.Fatalf("%s: withdraw %s %v", tr.Kind, st, err)
		}
		if st, err := q.Withdraw(ctx, id, "moved on"); err != nil || st != "withdrawn" {
			t.Fatalf("%s: a second withdraw %s %v", tr.Kind, st, err)
		}
	}
	rt := newQuestionRuntime(filepath.Join(t.TempDir(), "store.json"), nil)
	q := rt.landingCardQuestions()
	for i := 0; i < 20; i++ { // the store holds 20 open questions
		if _, _, err := q.AskRaw(ctx, fmt.Sprintf("landingcardquota%02d0123456789abcdef0123456789abcdef", i), store.LandingCardRequest("bifrost-platform", cases[i%len(cases)], fmt.Sprintf("Landing: title %d", i))); err != nil {
			t.Fatal(i, err)
		}
	}
	if _, _, err := q.AskRaw(ctx, "landingcardquotazz0123456789abcdef0123456789abcdef", store.LandingCardRequest("bifrost-platform", cases[3], "Landing: one more")); !errors.Is(err, store.ErrCardQuota) {
		t.Fatal("a 21st open card:", err)
	}
	if _, ok, err := q.Lookup(ctx, "q-none"); ok || err != nil {
		t.Fatal("a missing question", ok, err)
	}
	if st, err := q.Withdraw(ctx, "q-none", "x"); st != "gone" || err != nil {
		t.Fatal("withdrawing a missing question", st, err)
	}
}
