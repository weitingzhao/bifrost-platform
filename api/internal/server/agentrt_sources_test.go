package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/ownercard"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/questionbridge"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
	"github.com/weitingzhao/bifrost-platform/api/internal/releases"
)

type fakeApprovals map[string]approvals.Approval

func (f fakeApprovals) Lookup(ref string) (approvals.Approval, bool, error) {
	if ref == "13" {
		return approvals.Approval{}, false, errors.New("store unreadable")
	}
	a, ok := f[ref]
	return a, ok, nil
}

func (f fakeApprovals) Observations() ([]approvals.Approval, error) {
	out := make([]approvals.Approval, 0, len(f))
	for _, a := range f {
		out = append(out, a)
	}
	return out, nil
}

type downApprovals struct{ fakeApprovals }

func (downApprovals) Observations() ([]approvals.Approval, error) { return nil, errors.New("down") }

// 2026-11-23, the closed loop (Owner: only a change of law opens a ticket):
// the plan reads only executed law_amend cards that read, with their
// standards; a pending one, another action and an unreadable one are not
// laws.
func TestLawCardsReadsOnlyExecutedLawAmendCards(t *testing.T) {
	std := []any{map[string]any{"id": "s1", "standard": "0 segments by hand", "measure": "Audit counts"}}
	f := fakeApprovals{
		"1": {ID: "appr_1", Number: 1, Action: "law_amend", Status: approvals.StatusExecuted,
			Params: map[string]any{"op": "add", "after": "A new law", "revert": "repeal it", "standards": std}},
		"2": {ID: "appr_2", Number: 2, Action: "law_amend", Status: approvals.StatusPending,
			Params: map[string]any{"op": "add", "after": "Pending", "revert": "r", "standards": std}},
		"3": {ID: "appr_3", Number: 3, Action: "scale_deployment", Status: approvals.StatusExecuted},
		"4": {ID: "appr_4", Number: 4, Action: "law_amend", Status: approvals.StatusExecuted, Params: map[string]any{"op": "bogus"}},
	}
	cards, ok := agentRuntimeSources(f, nil, nil, nil).Laws(context.Background())
	if !ok || len(cards) != 1 {
		t.Fatalf("cards = %+v, ok %v; want only appr_1", cards, ok)
	}
	c := cards[0]
	if c.ApprovalID != "appr_1" || c.Op != "add" || c.After != "A new law" || len(c.Standards) != 1 || c.Standards[0].Measure != "Audit counts" {
		t.Fatalf("card = %+v", c)
	}
	if _, ok := agentRuntimeSources(downApprovals{}, nil, nil, nil).Laws(context.Background()); ok {
		t.Fatal("an unreadable store reads as ok")
	}
}

// question answers as the questionbridge reader does's reader does, from plain fields.
func question(_ context.Context, id string) (seenfacts.Question, bool) {
	// What questioncards.Fact makes of each card (observation_test.go there
	// holds the conversion); here the sources only have to hand it on.
	at := "2026-10-14T17:00:00Z"
	switch id {
	case "q_open":
		return seenfacts.Question{ID: id, Status: "open"}, true
	case "q_done":
		return seenfacts.Question{ID: id, Status: "answered", Option: "a", Text: "go", AnsweredBy: "owner", AnsweredAt: at}, true
	case "q_textual":
		return seenfacts.Question{ID: id, Status: "answered", Option: "none", Text: "just text", AnsweredBy: "owner", AnsweredAt: at}, true
	}
	return seenfacts.Question{}, false
}

// fakeCI answers as the ledger's reader does: a verdict, none, or an error.
func fakeCI(_ context.Context, repo, commit string) (string, string, error) {
	switch repo + "@" + commit {
	case "bifrost-platform@ok":
		return "Succeeded", "ci-platform-1", nil
	case "bifrost-platform@red":
		return "Failed", "ci-platform-2", nil
	case "bifrost-platform@unread":
		// An error is no verdict, whatever came with it.
		return "Succeeded", "ci-platform-3", errors.New("the evidence ledger is not connected")
	}
	return "", "", nil
}

type fakeCurrent map[string]releases.Record

func (f fakeCurrent) Current(_ context.Context, lane, env string) (releases.Record, error) {
	if lane != "platform" {
		return releases.Record{}, fmt.Errorf("lane %s", lane)
	}
	if env == "qa" {
		return releases.Record{}, releases.ErrUnsettled
	}
	r, ok := f[env]
	if !ok {
		return releases.Record{}, releases.ErrNoDeployment
	}
	return r, nil
}

// PodDigests: the pods run what the env's record says, but in "drift",
// where the workers pulled another image, and "podless", where no pod reads.
func (f fakeCurrent) PodDigests(_ context.Context, namespace, deployment, container string) ([]string, error) {
	env := strings.TrimPrefix(namespace, "bifrost-platform-")
	switch {
	case env == "drift" && deployment == "platform-workers":
		return []string{digestA, digestOther}, nil
	case env == "podless":
		return nil, errors.New("no pod")
	}
	return []string{f[env].Images[container]}, nil
}

var (
	sha1        = "1111111111111111111111111111111111111111"
	sha2        = "2222222222222222222222222222222222222222"
	digestA     = "sha256:" + strings.Repeat("a", 64)
	digestC     = "sha256:" + strings.Repeat("c", 64)
	digestOther = "sha256:" + strings.Repeat("e", 64)
)

func platformRec(run, sha string) releases.Record {
	r := releases.Record{Run: run, Lane: "platform", Deploys: true, Repos: map[string]releases.RepoBuild{},
		Images: map[string]string{"platform-api": digestA, "platform-console": digestC}}
	if sha != "" {
		r.Repos["bifrost-platform"] = releases.RepoBuild{SHA: sha}
	}
	return r
}

// The controller's sources read the platform's stores (design v5 §5.2 外部事实).
func TestAgentRuntimeSources(t *testing.T) {
	src := agentRuntimeSources(
		fakeApprovals{"7": {Number: 7, Status: approvals.StatusExecuted}, "8": {Number: 8}},
		question,
		fakeCI,
		fakeCurrent{"stg": platformRec("deliver-platform-2", sha2), "prod": platformRec("deliver-platform-prod-1", sha1),
			"short": platformRec("deliver-short", "abc"), "norepo": platformRec("deliver-norepo", ""), "norun": platformRec("", sha1),
			"drift": platformRec("deliver-drift", sha1), "podless": platformRec("deliver-podless", sha1),
			"noimages": {Run: "deliver-old", Lane: "platform", Deploys: true, Repos: map[string]releases.RepoBuild{"bifrost-platform": {SHA: sha1}}}},
	)
	ctx := context.Background()

	if s, ok := src.Approval(ctx, 7); !ok || s.Status != "executed" {
		t.Errorf("approval 7 = %v %v", s, ok)
	}
	for _, n := range []int64{8, 9, 13} { // no status, missing, store unreadable
		if s, ok := src.Approval(ctx, n); ok {
			t.Errorf("approval %d = %v, read", n, s)
		}
	}

	if q, ok := src.Question(ctx, "q_open"); !ok || q.Status != "open" || q.Option != "" {
		t.Errorf("open question = %+v %v", q, ok)
	}
	want := seenfacts.Question{ID: "q_done", Status: "answered", Option: "a", Text: "go", AnsweredBy: "owner", AnsweredAt: "2026-10-14T17:00:00Z"}
	if q, ok := src.Question(ctx, "q_done"); !ok || q != want {
		t.Errorf("answered question = %+v %v", q, ok)
	}
	if q, ok := src.Question(ctx, "q_textual"); !ok || q.Option != "none" || q.Text != "just text" {
		t.Errorf("a text-only answer = %+v %v", q, ok)
	}
	for _, id := range []string{"q_empty", "q_nobody", "q_untimed", "q_missing"} {
		if q, ok := src.Question(ctx, id); ok {
			t.Errorf("%s = %+v, read", id, q)
		}
	}

	if s, run, ok := src.CI(ctx, "bifrost-platform", "ok"); !ok || s != "Succeeded" || run != "ci-platform-1" {
		t.Errorf("ci = %q %q %v", s, run, ok)
	}
	if s, run, ok := src.CI(ctx, "bifrost-platform", "red"); !ok || s != "Failed" || run != "ci-platform-2" {
		t.Errorf("a failed ci = %q %q %v", s, run, ok)
	}
	// No verdict, and a read that failed, are both not read: never a pass.
	for _, commit := range []string{"unknown", "unread"} {
		if s, run, ok := src.CI(ctx, "bifrost-platform", commit); ok || s != "" || run != "" {
			t.Errorf("ci of %s = %q %q %v, read", commit, s, run, ok)
		}
	}

	images := " images:map[platform-api:" + digestA + " platform-console:" + digestC + "]]"
	for env, want := range map[string]string{"stg": sha2 + " map[delivery_run:deliver-platform-2" + images, "prod": sha1 + " map[delivery_run:deliver-platform-prod-1" + images} {
		commit, ref, ok := src.Deployed(ctx, env)
		if got := commit + " " + fmt.Sprint(ref); !ok || got != want {
			t.Errorf("deployed %s = %s %v", env, got, ok)
		}
	}
	// TD-343: a pod running another digest than the record's, pods that
	// cannot be read, and a record without digests are all not known.
	for _, env := range []string{"dev", "qa", "short", "norepo", "norun", "drift", "podless", "noimages"} {
		if commit, _, ok := src.Deployed(ctx, env); ok {
			t.Errorf("deployed %s = %s, read", env, commit)
		}
	}

	none := agentRuntimeSources(nil, nil, nil, nil)
	if _, ok := none.Approval(ctx, 7); ok {
		t.Error("no approvals store, read")
	}
	if _, ok := none.Question(ctx, "q_done"); ok {
		t.Error("no question store, read")
	}
	if _, _, ok := none.CI(ctx, "bifrost-platform", "ok"); ok {
		t.Error("no CI reader, read")
	}
	if _, _, ok := none.Deployed(ctx, "stg"); ok {
		t.Error("no release records, read")
	}
	empty := (&Server{}).agentRuntimeSources() // no store: a nil reader, never a typed nil
	if _, ok := empty.Approval(ctx, 7); ok {
		t.Error("a server with no stores read an approval")
	}
	if _, _, ok := empty.Deployed(ctx, "stg"); ok {
		t.Error("a server with no stores read a deployment")
	}
}

// questions_wire.go's reader on a real question store: open, answered by
// option, answered by text only, missing.
func TestQuestionReaderOnTheStore(t *testing.T) {
	rt := questionbridge.New(filepath.Join(t.TempDir(), "questions"), nil)
	ctx := context.Background()
	ask := func(key string, allowText bool) {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"idempotency_key": key, "title": "Which way?", "urgency": "Before tonight",
			"meanwhile": "The line waits", "options": []any{map[string]any{"label": "Ship"}, map[string]any{"label": "Wait"}}, "allow_text": allowText})
		if res := rt.Svc.Create(ctx, "agentrt", "reporter", "reporter", body); res.Code >= 300 {
			t.Fatalf("create = %d %v", res.Code, res.Body)
		}
	}
	ask("0123456789abcdef", false)
	ask("fedcba9876543210", true)
	read := rt.Question()
	q1, ok := read(ctx, "1")
	// TD-401 (review 460 B1): an open one says when it expires, which the
	// store marks only at its next sweep.
	if !ok || q1.Status != "open" || q1.Option != "" || !q1.ExpiresAt.After(time.Now()) {
		t.Fatalf("open = %+v %v", q1, ok)
	}
	answer := func(ref string, body map[string]any) {
		t.Helper()
		raw, _ := json.Marshal(body)
		if res := rt.Svc.Answer(ctx, "owner-console", "admin", "admin", ref, raw); res.Code >= 300 {
			t.Fatalf("answer %s = %d %v", ref, res.Code, res.Body)
		}
	}
	answer("1", map[string]any{"option": "b", "channel": "console"})
	answer("2", map[string]any{"text": "neither, wait for the data", "channel": "console"})
	q, ok := read(ctx, "1")
	at, err := time.Parse(time.RFC3339, q.AnsweredAt)
	if !ok || q.Status != "answered" || q.Option != "b" || q.AnsweredBy != "admin" || err != nil || time.Since(at) > time.Minute || at.Location() != time.UTC {
		t.Fatalf("answered = %+v %v %v", q, ok, err)
	}
	if q, ok := read(ctx, "2"); !ok || q.Option != "none" || q.Text != "neither, wait for the data" {
		t.Fatalf("answered by text = %+v %v", q, ok)
	}
	if _, ok := read(ctx, "9"); ok {
		t.Fatal("a missing question read")
	}
	var nilRuntime *questionbridge.Runtime
	if nilRuntime.Question() != nil {
		t.Fatal("no question store gave a reader")
	}
}

// Review 180 A1, B2, B3: the runtime's decision card on the real question
// store. An answer carries the option's key, which is what the runtime
// compares; the runtime acknowledges as the requester; a card the store
// cannot take for its size is refused as invalid, not as unreachable.
func TestPlanChangeCardOnTheStore(t *testing.T) {
	rt := questionbridge.New(filepath.Join(t.TempDir(), "questions"), nil)
	ctx := context.Background()
	ask, read, ack := rt.Ask(), rt.Question(), rt.Ack()
	id, err := ask(ctx, ownercard.Card{Key: strings.Repeat("ab", 16), Title: "Plan change to R2: a new item", Body: "A thread proposes …"})
	if err != nil {
		t.Fatal(err)
	}
	again, err := ask(ctx, ownercard.Card{Key: strings.Repeat("ab", 16), Title: "Plan change to R2: a new item", Body: "A thread proposes …"})
	if err != nil || again != id {
		t.Fatalf("a retry of the card: %s %v; want %s", again, err, id)
	}
	if err = ack(ctx, id); err == nil {
		t.Fatal("acknowledged a card nobody answered")
	}
	raw, _ := json.Marshal(map[string]any{"option": "a", "channel": "console"})
	if res := rt.Svc.Answer(ctx, "owner-console", "admin", "admin", id, raw); res.Code >= 300 {
		t.Fatalf("answer = %d %v", res.Code, res.Body)
	}
	q, ok := read(ctx, id)
	if !ok || q.Status != seenfacts.QuestionAnswered || q.Option != ownercard.CardApplyKey {
		t.Fatalf("the answer reads %+v; want option %q", q, ownercard.CardApplyKey)
	}
	for range 2 {
		if err = ack(ctx, id); err != nil {
			t.Fatalf("acknowledge: %v", err)
		}
	}
	got, ok, err := rt.Svc.Lookup(id)
	if err != nil || !ok || got.Answer == nil || got.Answer.Read == nil || got.Answer.Read.Session != "agentrt" {
		t.Fatalf("the read receipt: %+v %v %v", got.Answer, ok, err)
	}
	// Review 182 B2: the store's own limits, counted its way (JSON with the
	// quotes, HTML not escaped): a body of 2998 characters fits, 2999 does
	// not; a title is cut to fit, whatever it holds.
	if _, err = ask(ctx, ownercard.Card{Key: strings.Repeat("cd", 16), Title: "Plan change", Body: strings.Repeat("<", 2998)}); err != nil {
		t.Fatalf("a body of 3000 encoded bytes: %v", err)
	}
	_, err = ask(ctx, ownercard.Card{Key: strings.Repeat("ef", 16), Title: "Plan change", Body: strings.Repeat("x", 2999)})
	if !errors.Is(err, eventlog.ErrInvalid) {
		t.Fatalf("a body of 3001 encoded bytes: %v", err)
	}
	// A fresh store for the titles: the runtime may have three cards open.
	fresh := questionbridge.New(filepath.Join(t.TempDir(), "titles"), nil).Ask()
	for i, title := range []string{strings.Repeat("&", 298), strings.Repeat("a", 299), strings.Repeat("计", 150) + "\u0007tail"} {
		if _, err = fresh(ctx, ownercard.Card{Key: strings.Repeat(string(rune('g'+i)), 32), Title: title, Body: "b"}); err != nil {
			t.Errorf("title %d: %v", i, err)
		}
	}
	if err = ack(ctx, "q_not_there"); !errors.Is(err, ownercard.ErrQuestionGone) {
		t.Errorf("acknowledging a card the store dropped: %v", err)
	}
}

// A planner's card on the real question store (W-31 2.2b, review 217 B2):
// its own options and default; withdrawn while open; the same question under
// another key, open or answered, comes back as asked before, not as an error
// a plan would wait on forever. A plan change's card keeps its old answer.
func TestPlannerCardOnTheStore(t *testing.T) {
	rt := questionbridge.New(filepath.Join(t.TempDir(), "questions"), nil)
	ctx := context.Background()
	ask, read, withdraw := rt.Ask(), rt.Question(), rt.Withdraw()
	card := func(key, question string) ownercard.Card {
		return ownercard.Card{Key: key, Title: question, Body: "why it matters", Options: []string{"Not now", "Yes"}, Recommended: 1,
			Meanwhile: "the work goes on without it", OwnerDuty: "goal", TaskID: "t_01M5192P0JPMJAP63ZKRPXJ2VV"}
	}
	id, err := ask(ctx, card(strings.Repeat("a1", 16), "Document the Console too?"))
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := rt.Svc.Lookup(id)
	if err != nil || !ok || len(got.Options) != 2 || got.Recommended != "a" || got.OwnerDuty != "goal" || got.TaskID == "" {
		t.Fatalf("the card %+v %v %v", got, ok, err)
	}
	// Open, under another key: asked before.
	if _, err = ask(ctx, card(strings.Repeat("b2", 16), "Document the Console too?")); !errors.Is(err, ownercard.ErrAsked) || !strings.Contains(err.Error(), "duplicate of") {
		t.Fatalf("an open duplicate: %v", err)
	}
	raw, _ := json.Marshal(map[string]any{"option": "b", "channel": "console"})
	if res := rt.Svc.Answer(ctx, "owner-console", "admin", "admin", id, raw); res.Code >= 300 {
		t.Fatalf("answer = %d %v", res.Code, res.Body)
	}
	if q, ok := read(ctx, id); !ok || q.Status != seenfacts.QuestionAnswered {
		t.Fatalf("the answer %+v", q)
	}
	// Answered: not withdrawn (that is not an error), asked before.
	if err = withdraw(ctx, id, "the plan was not used"); err != nil {
		t.Fatalf("withdrawing an answered card: %v", err)
	}
	if _, err = ask(ctx, card(strings.Repeat("c3", 16), "Document the Console too?")); !errors.Is(err, ownercard.ErrAsked) || !strings.Contains(err.Error(), "already answered") {
		t.Fatalf("an answered duplicate: %v", err)
	}
	// An open card withdrawn; one that is gone needs nothing.
	other, err := ask(ctx, card(strings.Repeat("d4", 16), "Name the planner in the comments?"))
	if err != nil {
		t.Fatal(err)
	}
	if err = withdraw(ctx, other, "the plan was not used"); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	if got, ok, _ = rt.Svc.Lookup(other); !ok || got.Status != "withdrawn" {
		t.Fatalf("after the withdrawal: %+v", got)
	}
	if err = withdraw(ctx, "q_not_there", "gone"); err != nil {
		t.Fatalf("withdrawing a card the store dropped: %v", err)
	}
	// A plan change's card, the same question twice under two keys: as before,
	// an error, not asked before.
	pc := ownercard.Card{Key: strings.Repeat("e5", 16), Title: "Plan change to R2: a new item", Body: "A thread proposes …"}
	if _, err = ask(ctx, pc); err != nil {
		t.Fatal(err)
	}
	pc.Key = strings.Repeat("f6", 16)
	if _, err = ask(ctx, pc); err == nil || errors.Is(err, ownercard.ErrAsked) {
		t.Fatalf("a plan change's duplicate: %v", err)
	}
}
func TestExternalQuestionDropsOtherSessionText(t *testing.T) {
	t.Parallel()
	rt := questionbridge.New(filepath.Join(t.TempDir(), "questions"), nil)
	ctx := context.Background()
	raw, _ := json.Marshal(map[string]any{"idempotency_key": "abcdef0123456789", "title": "Which way?", "urgency": "Before tonight",
		"meanwhile": "The line waits", "options": []any{map[string]any{"label": "Ship"}, map[string]any{"label": "Wait"}}, "allow_text": true})
	if res := rt.Svc.Create(ctx, "other-session", "reporter", "reporter", raw); res.Code >= 300 {
		t.Fatalf("create = %d %v", res.Code, res.Body)
	}
	raw, _ = json.Marshal(map[string]any{"text": "private answer", "channel": "console"})
	if res := rt.Svc.Answer(ctx, "owner-console", "admin", "admin", "1", raw); res.Code >= 300 {
		t.Fatalf("answer = %d %v", res.Code, res.Body)
	}
	q, ok := rt.Question()(ctx, "1")
	if !ok || q.Text != "" || q.Option != "none" || q.Number != 1 || q.CreatedAt == "" || q.Channel != "console" {
		t.Fatalf("fact = %+v, %v", q, ok)
	}
}
