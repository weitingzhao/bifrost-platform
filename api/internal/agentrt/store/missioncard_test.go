package store_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/questions"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

func proposeOutside(t *testing.T, w *world, ask store.Ask, key string) (string, string) {
	t.Helper()
	out, err := store.ProposeMission(ctx, w.rt, wr("thread:s1"), key,
		store.MissionProposal{Title: "m " + key, Goal: "g", Success: []string{"done"}}, ask)
	ok(t, err)
	id, _ := out["mission_id"].(string)
	q, _ := out["question_id"].(string)
	if out["state"] != "awaiting_owner" || out["waiting"] != store.EnvelopePending || q == "" || w.state(t, "mission", id) != "awaiting_owner" {
		t.Fatalf("proposal %s: %v", key, out)
	}
	return id, q
}

// A mission outside the standing envelope waits on a decision card (proposal
// §2): the first option starts it; the second, or "none of these" with the
// Owner's words, stops it; a card that expired stops it; an open card leaves
// it waiting. Each answer is kept whole (question_seen) before the mission
// moves and acknowledged once; a card with no answer is not acknowledged;
// the open card is in needs_you, and the decided ones are not.
func TestMissionCard(t *testing.T) {
	w := newKernelWorld(t)
	a := &asker{}
	question := map[string]string{}
	var start, refuse, none, expire, open string
	for key, id := range map[string]*string{"a": &start, "b": &refuse, "c": &none, "d": &expire, "e": &open} {
		m, q := proposeOutside(t, w, a.ask, key)
		*id, question[m] = m, q
	}
	c := a.cards[0]
	if len(a.cards) != 5 || len(c.Options) != 2 || c.Options[0] != store.MissionStart || c.Options[1] != store.MissionNotStart ||
		c.Meanwhile == "" || c.OwnerDuty != "goal" || c.Urgency == "" || c.Recommended != 0 || c.TaskID != "" {
		t.Fatalf("cards %+v", a.cards)
	}
	a.answer(question[start], store.QuestionAnswered, store.CardApplyKey)
	a.answer(question[refuse], store.QuestionAnswered, store.CardRefuseKey)
	a.answers[question[none]] = store.Question{ID: question[none], Status: store.QuestionAnswered, Option: "none",
		Text: "narrow it first\nand say why", AnsweredBy: "owner", AnsweredAt: "2026-10-14T21:00:00Z"}
	a.answer(question[expire], "expired", "")
	n, err := store.DecideMissions(ctx, w.rt, a.question, a.ack)
	ok(t, err)
	if n != 4 {
		t.Fatalf("decided %d; want 4", n)
	}
	for id, want := range map[string]string{start: "active", refuse: "stopped", none: "stopped", expire: "stopped", open: "awaiting_owner"} {
		if got := w.state(t, "mission", id); got != want {
			t.Errorf("mission %s is %s; want %s", id, got, want)
		}
	}
	var reason, answer string
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'reason', body_json->>'answer' FROM agentrt.event WHERE mission_id = $1
	  AND event_type = 'mission_state_changed' ORDER BY event_id DESC LIMIT 1`, none).Scan(&reason, &answer))
	if !strings.Contains(reason, "none of these") || !strings.Contains(reason, "narrow it first") || answer != "none" {
		t.Errorf("the none answer's reason %q, answer %q", reason, answer)
	}
	// The answers as read are kept whole, whatever the question store keeps.
	var text, at string
	ok(t, w.rt.QueryRow(ctx, `SELECT COALESCE(max(body_json->>'text'), ''), COALESCE(max(body_json->>'answered_at'), '')
	  FROM agentrt.event WHERE event_type = 'question_seen' AND body_json->>'question' = $1`, question[none]).Scan(&text, &at))
	if text != "narrow it first\nand say why" || at != "2026-10-14T21:00:00Z" {
		t.Errorf("the none answer kept as %q at %q", text, at)
	}
	for _, id := range []string{start, refuse, none} {
		var option, by string
		ok(t, w.rt.QueryRow(ctx, `SELECT COALESCE(max(body_json->>'option'), ''), COALESCE(max(body_json->>'answered_by'), '')
		  FROM agentrt.event WHERE event_type = 'question_seen' AND body_json->>'question' = $1`, question[id]).Scan(&option, &by))
		if option == "" || by != "owner" {
			t.Errorf("question %s seen as option %q by %q; want the whole answer kept", question[id], option, by)
		}
	}
	if n, err = store.DecideMissions(ctx, w.rt, a.question, a.ack); err != nil || n != 0 {
		t.Fatalf("a second pass decided %d (%v)", n, err)
	}
	for _, id := range []string{start, refuse, none} {
		if a.acked[question[id]] != 1 {
			t.Errorf("question %s acknowledged %d times; want once", question[id], a.acked[question[id]])
		}
	}
	for _, id := range []string{expire, open} {
		if a.acked[question[id]] != 0 {
			t.Errorf("question %s, with no answer, was acknowledged", question[id])
		}
	}
	doc := w.progress(t)
	found := false
	for _, it := range doc.NeedsYou.Items {
		if it["id"] == "question:"+question[open] && it["kind"] == "mission" && it["ref"] == open && it["can_act_now"] == nil {
			found = true
		}
		if it["id"] == "question:"+question[start] {
			t.Errorf("a decided card is still in needs_you: %v", it)
		}
	}
	if !found {
		t.Fatalf("needs_you %v; want the open mission card", doc.NeedsYou.Items)
	}
}

// Inside the standing envelope a mission starts at once and no card is
// asked, with a question store or without; outside it the same platform
// asks.
func TestMissionInsideTheEnvelopeAsksNobody(t *testing.T) {
	w := newKernelWorld(t)
	s := signer{t, rptest.NewKey(t)}
	ok(t, firstErr(s.sign(w, envelopeRule(1, rules.Initial, "h1"))))
	ok(t, firstErr(s.sign(w, ruleText("budget:dev", 2, rules.Tighten, since, until,
		"tenant: dev\nmission: {max_attempts: 30, max_wall_hours: 24}\nattempt_hard_limit_s: 3600\n"))))
	a := &asker{}
	in, err := store.ProposeMission(ctx, w.rt, wr("thread:s1"), "in", store.MissionProposal{Title: "m", Goal: "g", Success: []string{"done"}}, a.ask)
	ok(t, err)
	if in["state"] != "active" || in["question_id"] != nil || len(a.cards) != 0 {
		t.Fatalf("inside: %v, %d cards", in, len(a.cards))
	}
	out, err := store.ProposeMission(ctx, w.rt, wr("thread:s1"), "out",
		store.MissionProposal{Title: "m", Goal: "g", Success: []string{"done"}, MaxAttempts: 41, MaxWallHours: 10}, a.ask)
	ok(t, err)
	if out["state"] != "awaiting_owner" || len(a.cards) != 1 || !strings.Contains(fmt.Sprint(out["waiting"]), "above the envelope's") {
		t.Fatalf("outside: %v, %d cards", out, len(a.cards))
	}
	if !strings.Contains(a.cards[0].Body, "Budget: 41 attempts, 10 hours") {
		t.Errorf("the card does not show the budget asked: %s", a.cards[0].Body)
	}
}

// Reviews 190 A1, 192 A1: the card says nothing a retry could change, so the
// same proposal under the same key makes the same card whatever number the
// mission gets and whatever the budget rule says by then.
func TestMissionCardDoesNotNameTheNumber(t *testing.T) {
	cards := map[string]store.Card{}
	for _, extra := range []int{0, 1} {
		w := newKernelWorld(t)
		for i := range extra {
			ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
				_, err := store.CreateMission(ctx, tx, "controller", store.Mission{ID: fmt.Sprintf("M-%d", 3+i), Title: "x", Goal: "g",
					Deadline: time.Now().Add(7 * 24 * time.Hour)})
				return err
			}))
			// And the default budget the proposal would get has changed
			// (review 192 A1).
			_, err := store.TightenRule(ctx, w.rt, wr("thread:s9"), "budget:dev", []byte(ruleText("budget:dev", 2, rules.Tighten, since, until,
				"tenant: dev\nmission: {max_attempts: 500, max_wall_hours: 300}\nattempt_hard_limit_s: 3600\n")))
			ok(t, err)
		}
		a := &asker{}
		id, _ := proposeOutside(t, w, a.ask, "k")
		cards[id] = a.cards[0]
	}
	if len(cards) != 2 {
		t.Fatalf("the two proposals got the same number: %v", cards)
	}
	var first *store.Card
	for id, c := range cards {
		if strings.Contains(c.Title+c.Body, id) || strings.Contains(c.Body, "500") {
			t.Errorf("card names %s or the rule's budget: %+v", id, c)
		}
		if first == nil {
			c := c
			first = &c
		} else if first.Key != c.Key || first.Title != c.Title || first.Body != c.Body {
			t.Errorf("two cards for one proposal and key:\n%+v\n%+v", *first, c)
		}
	}
}

// Review 190 B2, B3, review 192's note on B2: the pass reads the waiting
// missions page by page, so cards it cannot read never hide later ones; a
// mission another pass moves between reading its card and locking it is
// left as that pass left it and not counted.
func TestMissionDecisionsPageAndCount(t *testing.T) {
	w := newKernelWorld(t)
	defer store.DecidePageForTest(2)()
	a := &asker{}
	var ids, qs []string
	for _, key := range []string{"a", "b", "c", "d"} {
		id, q := proposeOutside(t, w, a.ask, key)
		ids, qs = append(ids, id), append(qs, q)
	}
	unreadable := map[string]bool{qs[0]: true, qs[1]: true}
	question := func(ctx context.Context, id string) (store.Question, bool) {
		if unreadable[id] {
			return store.Question{}, false
		}
		if id == qs[3] {
			// Another pass decides it while this one reads the card.
			ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
				return store.MoveMission(ctx, tx, "controller", ids[3], "stopped", "moved by another pass")
			}))
		}
		return a.question(ctx, id)
	}
	a.answer(qs[2], store.QuestionAnswered, store.CardApplyKey)
	a.answer(qs[3], store.QuestionAnswered, store.CardApplyKey)
	n, err := store.DecideMissions(ctx, w.rt, question, nil)
	ok(t, err)
	if n != 1 || w.state(t, "mission", ids[2]) != "active" || w.state(t, "mission", ids[3]) != "stopped" {
		t.Fatalf("decided %d; %s is %s, %s is %s", n, ids[2], w.state(t, "mission", ids[2]), ids[3], w.state(t, "mission", ids[3]))
	}
	if seen := w.count(t, `SELECT count(*) FROM agentrt.event WHERE event_type = 'question_seen' AND body_json->>'question' = $1`, qs[3]); seen != 0 {
		t.Errorf("the answer of a card another pass decided was kept %d times", seen)
	}
}

// Review 190 B4: plan changes and missions share needs_you's one bound.
func TestNeedsYouOneBoundForEveryCard(t *testing.T) {
	w := newKernelWorld(t)
	a := &asker{}
	proposeOutside(t, w, a.ask, "a")
	_, err := store.ProposePlanChange(ctx, w.rt, wr("thread:s1"), "c", root, "rename the item", []store.PlanChange{{Op: "retitle", Node: item, Title: "x"}},
		func(context.Context, store.Card) (string, error) { return "q_plan", nil })
	ok(t, err)
	defer store.LimitPendingChangesForTest(1)()
	doc := w.progress(t)
	if doc.NeedsYou.Count != 1 || !doc.NeedsYou.Truncated {
		t.Fatalf("needs you at a bound of 1: %d, truncated %v", doc.NeedsYou.Count, doc.NeedsYou.Truncated)
	}
}

// When the Owner cannot be asked, a mission outside the envelope is refused
// whole: nothing is written, and the same key may try again.
func TestMissionCardOwnerUnreachable(t *testing.T) {
	w := newKernelWorld(t)
	failing := func(context.Context, store.Card) (string, error) { return "", errors.New("the question store is full") }
	p := store.MissionProposal{Title: "m", Goal: "g", Success: []string{"done"}}
	_, err := store.ProposeMission(ctx, w.rt, wr("thread:s1"), "first", p, nil)
	ok(t, err) // registers the thread, so the footprint below is the refusal's
	before := w.footprint(t)
	if _, err = store.ProposeMission(ctx, w.rt, wr("thread:s1"), "k", p, failing); !errors.Is(err, store.ErrAsk) {
		t.Fatalf("%v; want ErrAsk", err)
	}
	if after := w.footprint(t); after != before {
		t.Fatalf("a refused proposal left something: %s -> %s", before, after)
	}
	a := &asker{}
	out, err := store.ProposeMission(ctx, w.rt, wr("thread:s1"), "k", p, a.ask)
	ok(t, err)
	if out["state"] != "awaiting_owner" {
		t.Fatalf("again: %v", out)
	}
}

// Review 190 B4: a mission on its card says on which question it waits, with
// tasks or without, and its needs_you item counts its unfinished tasks.
func TestMissionOnACardSaysSo(t *testing.T) {
	w := newKernelWorld(t)
	a := &asker{}
	id, q := proposeOutside(t, w, a.ask, "a")
	bare, bq := proposeOutside(t, w, a.ask, "b")
	_, err := store.ProposeTasks(ctx, w.rt, wr("thread:s1"), "t", id, []store.TaskProposal{offPlan(proposal("e", w.spec))})
	ok(t, err)
	doc := w.progress(t)
	for _, m := range doc.Work.Missions {
		if (m.MissionID == id && !strings.Contains(m.Reason, "question "+q)) || (m.MissionID == bare && !strings.Contains(m.Reason, "question "+bq)) {
			t.Errorf("mission %s: %q; want the question it waits on", m.MissionID, m.Reason)
		}
	}
	held := false
	for _, it := range doc.NeedsYou.Items {
		if it["id"] == "question:"+q {
			blocks, _ := it["blocks"].(map[string]any)
			held = blocks["unfinished_tasks"] == float64(1)
		}
	}
	if !held {
		t.Errorf("needs_you %v; want the card holding up its one task", doc.NeedsYou.Items)
	}
}

// Review 192 A4: two proposals with the same words make two cards (each card
// names its proposal), so the question store never takes one for the other.
func TestTwoProposalsTwoCards(t *testing.T) {
	w := newKernelWorld(t)
	a := &asker{}
	same := store.MissionProposal{Title: "same", Goal: "g", Success: []string{"done"}}
	for _, key := range []string{"a", "b"} {
		out, err := store.ProposeMission(ctx, w.rt, wr("thread:s1"), key, same, a.ask)
		ok(t, err)
		if out["state"] != "awaiting_owner" {
			t.Fatalf("proposal %s: %v", key, out)
		}
	}
	if len(a.cards) != 2 || a.cards[0].Title != a.cards[1].Title || a.cards[0].Body == a.cards[1].Body {
		t.Fatalf("cards %+v; want two that differ", a.cards)
	}
}

// Owner 2026-10-15 「决策卡 A」: a proposal's words are bounded when it is
// made, inside the envelope or not; each bound holds at its limit and
// refuses one byte more, with the advice and nothing written. Review 193
// A5's proposal (a goal of 770 Chinese characters) is refused by the goal's
// bound.
func TestMissionProposalBounds(t *testing.T) {
	w := newKernelWorld(t)
	a := &asker{}
	base := store.MissionProposal{Title: "m", Goal: "g", Success: []string{"done"}}
	repos := func(n int) []string {
		r := make([]string, n)
		for i := range r {
			r[i] = fmt.Sprintf("repo-%d", i)
		}
		return r
	}
	successes := func(n int) []string {
		s := make([]string, n)
		for i := range s {
			s[i] = fmt.Sprintf("criterion %d", i)
		}
		return s
	}
	at := map[string]func(*store.MissionProposal){
		"title":   func(p *store.MissionProposal) { p.Title = strings.Repeat("t", store.MaxMissionTitleBytes) },
		"goal":    func(p *store.MissionProposal) { p.Goal = strings.Repeat("目", store.MaxMissionGoalBytes/3) },
		"success": func(p *store.MissionProposal) { p.Success = successes(store.MaxMissionSuccess) },
		"one": func(p *store.MissionProposal) {
			p.Success = []string{strings.Repeat("s", store.MaxMissionSuccessBytes)}
		},
		"repos": func(p *store.MissionProposal) { p.Repos = repos(store.MaxMissionRepos) },
	}
	over := map[string]func(*store.MissionProposal){
		"title":   func(p *store.MissionProposal) { p.Title = strings.Repeat("t", store.MaxMissionTitleBytes+1) },
		"goal":    func(p *store.MissionProposal) { p.Goal = strings.Repeat("目", store.MaxMissionGoalBytes/3) + "g" },
		"success": func(p *store.MissionProposal) { p.Success = successes(store.MaxMissionSuccess + 1) },
		"one": func(p *store.MissionProposal) {
			p.Success = []string{strings.Repeat("s", store.MaxMissionSuccessBytes+1)}
		},
		"repos": func(p *store.MissionProposal) { p.Repos = repos(store.MaxMissionRepos + 1) },
		"193 A5": func(p *store.MissionProposal) {
			p.Goal = strings.Repeat("核对任务卡片并确认授权范围。", 55)
		},
	}
	for name, edit := range at {
		p := base
		edit(&p)
		if _, err := store.ProposeMission(ctx, w.rt, wr("thread:s1"), "at-"+name, p, a.ask); err != nil {
			t.Errorf("%s at its bound: %v", name, err)
		}
	}
	before := w.footprint(t)
	for name, edit := range over {
		p := base
		edit(&p)
		_, err := store.ProposeMission(ctx, w.rt, wr("thread:s1"), "over-"+name, p, a.ask)
		if !errors.Is(err, store.ErrInvalid) || (name != "repos" && !strings.Contains(err.Error(), "put the details in a document")) {
			t.Errorf("%s over its bound: %v", name, err)
		}
	}
	if after := w.footprint(t); after != before {
		t.Fatalf("a proposal over a bound left something: %s -> %s", before, after)
	}
}

// Owner 2026-10-15 「决策卡 A」, review 193 A5: a proposal inside every bound
// whose card would still be longer than the question store takes (counted
// the store's way: cleaned, JSON-encoded, quotes and escapes included) is
// refused before the Owner is asked, with nothing written; the longest one
// that fits is asked, its body within the store's limit.
func TestMissionCardTooLongIsRefusedBeforeAsking(t *testing.T) {
	w := newKernelWorld(t)
	a := &asker{}
	// Quotes and newlines cost two bytes each once encoded.
	long := store.MissionProposal{Title: "m", Goal: strings.Repeat("\"", 1150),
		Success: []string{"done", strings.Repeat("\\", store.MaxMissionSuccessBytes), strings.Repeat("\n", store.MaxMissionSuccessBytes)}}
	_, err := store.ProposeMission(ctx, w.rt, wr("thread:s1"), "first", store.MissionProposal{Title: "m", Goal: "g", Success: []string{"done"}}, a.ask)
	ok(t, err)
	asked, before := len(a.cards), w.footprint(t)
	_, err = store.ProposeMission(ctx, w.rt, wr("thread:s1"), "long", long, a.ask)
	if !errors.Is(err, store.ErrInvalid) || !strings.Contains(err.Error(), "would be") || !strings.Contains(err.Error(), "put the details in a document") {
		t.Fatalf("a card too long: %v", err)
	}
	if len(a.cards) != asked {
		t.Fatalf("the Owner was asked a card too long to take")
	}
	if after := w.footprint(t); after != before {
		t.Fatalf("a card too long left something: %s -> %s", before, after)
	}
	// Short of the limit by the success criteria it drops, it fits.
	fits := long
	fits.Success = fits.Success[:1]
	_, err = store.ProposeMission(ctx, w.rt, wr("thread:s1"), "fits", fits, a.ask)
	ok(t, err)
	if n := questions.BodyBytes(a.cards[len(a.cards)-1].Body); n > questions.BodyLimit || n < questions.BodyLimit-800 {
		t.Fatalf("the card that fits is %d encoded bytes; want close to and within %d", n, questions.BodyLimit)
	}
}

// Review 320 A1: a key a platform before the bounds took for a long
// proposal gets its first answer again; the same words under a new key are
// refused.
func TestMissionBoundsSpareAnOlderKey(t *testing.T) {
	w := newKernelWorld(t)
	long := store.MissionProposal{Title: "m", Goal: strings.Repeat("g", store.MaxMissionGoalBytes+1), Success: []string{"done"}}
	first, err := store.ProposeUnboundedForTest(ctx, w.rt, wr("thread:s1"), "legacy-long", long)
	ok(t, err)
	again, err := store.ProposeMission(ctx, w.rt, wr("thread:s1"), "legacy-long", long, (&asker{}).ask)
	ok(t, err)
	same(t, again, first)
	_, err = store.ProposeMission(ctx, w.rt, wr("thread:s1"), "new-long", long, (&asker{}).ask)
	if !errors.Is(err, store.ErrInvalid) || !strings.Contains(err.Error(), "put the details in a document") {
		t.Fatalf("a new key for the long words: %v", err)
	}
}
