package store_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The Owner's cards on business issues (W-31 BI-1 slice 2, issuecard.go,
// issuecarddecide.go; DESIGN-crew-intake §2.2 request_owner, §5.3, X A).
// Every text here is synthetic; the markers stand for her words.

// cardDesk is a question store as the runtime sees it: the cards asked, the
// Owner's answers, what was withdrawn and acknowledged.
type cardDesk struct {
	cards     []store.Card
	ids       []string
	questions map[string]store.Question
	withdrawn []string
	acked     []string
}

func newCardDesk() *cardDesk { return &cardDesk{questions: map[string]store.Question{}} }

func (d *cardDesk) ask(_ context.Context, c store.Card) (string, error) {
	for i, k := range d.cards {
		if k.Key == c.Key {
			return d.ids[i], nil // the question store's own key: a retry finds its card
		}
	}
	id := fmt.Sprintf("q_%016d", len(d.cards)+1)
	d.cards, d.ids = append(d.cards, c), append(d.ids, id)
	d.questions[id] = store.Question{ID: id, Status: "open", Options: len(c.Options)}
	return id, nil
}

func (d *cardDesk) question(_ context.Context, id string) (store.Question, bool) {
	q, ok := d.questions[id]
	return q, ok
}

func (d *cardDesk) withdraw(_ context.Context, id, _ string) error {
	d.withdrawn = append(d.withdrawn, id)
	q := d.questions[id]
	q.Status = "withdrawn"
	d.questions[id] = q
	return nil
}

func (d *cardDesk) ack(_ context.Context, id string) error { d.acked = append(d.acked, id); return nil }

// answer is the Owner's answer on the Console (or a rule's, channel rule).
func (d *cardDesk) answer(id, option, channel string) {
	q := d.questions[id]
	q.Status, q.Option, q.AnsweredBy, q.AnsweredAt, q.Channel = store.QuestionAnswered, option, "admin", time.Now().UTC().Format(time.RFC3339), channel
	d.questions[id] = q
}

func (d *cardDesk) decide(t *testing.T, iw *issueWorld) int {
	t.Helper()
	n, err := store.DecideIssueCards(ctx, iw.rt, d.question, d.ask, d.withdraw, d.ack)
	ok(t, err)
	return n
}

func (d *cardDesk) last() store.Card { return d.cards[len(d.cards)-1] }

func (iw *issueWorld) requestOwner(t *testing.T, d *cardDesk, id, purpose, point string, letters int) (string, error) {
	t.Helper()
	out, err := store.RequestOwner(ctx, iw.rt, iw.crew, iw.key(), store.OwnerCardRequest{IssueID: id, ExpectedVersion: iw.state(t, id).Version,
		Purpose: purpose, Decision: point, Letters: letters}, d.ask)
	if err != nil {
		return "", err
	}
	return out["question_id"].(string), nil
}

// cardMarkers are the issue's texts as the tests write them: none may be on
// a card.
var cardMarkers = []string{"TITLE-7f3a", "WORDS-7f3a", "READING-7f3a", "BRIEF-7f3a", "DESIGN-7f3a", "REVIEW-7f3a", "MISSION-7f3a", "COMMIT-7f3a"}

func noMarkers(t *testing.T, c store.Card) {
	t.Helper()
	all := strings.Join(append([]string{c.Title, c.Body, c.Link, c.LinkText, c.Meanwhile, c.Urgency, c.RecommendedWhy}, c.Options...), "\n")
	for _, m := range cardMarkers {
		if strings.Contains(all, m) {
			t.Fatalf("card %q carries %s:\n%s", c.Title, m, all)
		}
	}
	if !strings.HasPrefix(c.WorkID, "BI-") || c.Recommended != 0 || c.ByRule || c.TaskID != "" || !strings.HasPrefix(c.Title, c.WorkID+" · ") {
		t.Fatalf("card %+v is not an issue's card by the template", c)
	}
}

// ownerIssue brings an issue for the Owner by a thread, with every text
// carrying a marker, its brief drafted and confirmed by the Owner.
func (iw *issueWorld) ownerIssue(t *testing.T) string {
	t.Helper()
	out, err := store.SubmitIssue(ctx, iw.rt, iw.crew, iw.key(), store.IssueSubmission{Product: "screening", Kind: "request",
		Title: "TITLE-7f3a", OnBehalfOf: store.OwnerPerson, Words: []store.IssueWords{{Text: "WORDS-7f3a"}}, Reading: "READING-7f3a"})
	ok(t, err)
	return out["issue_id"].(string)
}

// The four purposes request_owner takes, each a card of the template: the
// issue's number as its work id, neutral options (2 to 4), no word of the
// issue, no recommendation, never by rule (§5.3, R2-A2).
func TestOwnerCardsCarryNoText(t *testing.T) {
	t.Parallel()
	iw := newIssueWorld(t)
	d := newCardDesk()
	id := iw.ownerIssue(t)
	ask := iw.brief(t, id, "BRIEF-7f3a")
	q, err := iw.requestOwner(t, d, id, store.OwnerCardConfirmBrief, "", 0)
	ok(t, err)
	c := d.last()
	noMarkers(t, c)
	if v := iw.state(t, id); c.Title != id+" · confirm brief" || len(c.Options) != 2 || c.Options[0] != "Confirm "+v.BriefSHA256[:12] ||
		v.State != "owner_deciding" || v.WaitingOn != "owner" || !strings.Contains(c.Body, v.BriefSHA256[:12]) || strings.Contains(c.Body, v.BriefSHA256) {
		t.Fatalf("confirm card %+v on %+v", c, v)
	}
	d.answer(q, "a", "console")
	if n := d.decide(t, iw); n != 1 {
		t.Fatalf("settled %d", n)
	}
	if v := iw.state(t, id); !v.BriefConfirmed || v.State != "brief_confirmed" || v.WaitingOn != "crew" {
		t.Fatalf("after the Owner's confirmation: %+v", v)
	}
	var by, channel, body, person string
	ok(t, iw.rt.QueryRow(ctx, `SELECT k.answered_by_actor_id, k.answer_channel, t.body, t.person FROM agentrt.issue_ask k
	  JOIN agentrt.issue_text t ON t.issue_text_id = k.words_text_id WHERE k.issue_ask_id = $1`, ask).Scan(&by, &channel, &body, &person))
	if by != "owner" || channel != store.ChannelCard || body != store.ConfirmLabel(iw.state(t, id).BriefSHA256) || person != store.OwnerPerson {
		t.Fatalf("the answer recorded: %s %s %q %s", by, channel, body, person)
	}
	if n := iw.count(t, `SELECT count(*) FROM agentrt.event WHERE event_type = 'issue_answered' AND actor_id = 'controller'
	  AND body_json->>'question_id' = $1`, q); n != 1 {
		t.Fatalf("issue_answered naming the card: %d", n)
	}

	ok(t, iw.classify(t, id, "large", "method_change"))
	_, err = iw.requestOwner(t, d, id, store.OwnerCardPriority, "", 0)
	ok(t, err)
	if c := d.last(); len(c.Options) != 3 || c.Title != id+" · priority" {
		t.Fatalf("priority card %+v", c)
	}
	noMarkers(t, d.last())
	_, err = store.PutDesign(ctx, iw.rt, iw.crew, iw.key(), store.IssueDesign{IssueID: id, ExpectedVersion: iw.state(t, id).Version,
		Text: "DESIGN-7f3a", Decisions: []store.DesignDecision{{Key: "D1", Proceed: []string{"B"}}}})
	ok(t, err)
	_, err = iw.requestOwner(t, d, id, store.OwnerCardDesignDecision, "D1", 3)
	ok(t, err)
	if c := d.last(); strings.Join(c.Options, ",") != "A,B,C" || c.Title != id+" · design decision D1" {
		t.Fatalf("decision card %+v", c)
	}
	noMarkers(t, d.last())
	for round, result := range []string{"先修", "先修"} {
		_, err = store.RecordReview(ctx, iw.rt, iw.crew, iw.key(), store.IssueReview{IssueID: id, ExpectedVersion: iw.state(t, id).Version,
			Report: "REVIEW-7f3a\n" + result, ReviewerTool: "codex", AuthorTool: "claude", Round: round + 1})
		ok(t, err)
	}
	_, err = iw.requestOwner(t, d, id, store.OwnerCardReviewDispute, "", 0)
	ok(t, err)
	if c := d.last(); strings.Join(c.Options, ",") != "Proceed,Stop,Another round" || c.Title != id+" · review dispute" {
		t.Fatalf("dispute card %+v", c)
	}
	noMarkers(t, d.last())
	// The events say which cards, never a text.
	var events string
	ok(t, iw.rt.QueryRow(ctx, `SELECT string_agg(body_json::text, '|') FROM agentrt.event WHERE event_stream_id = 'issue:' || $1`, id).Scan(&events))
	for _, m := range cardMarkers {
		if strings.Contains(events, m) {
			t.Fatalf("an event of %s carries %s", id, m)
		}
	}
}

// Priority in two steps (X A): take it on, later or decline; "take it on"
// asks the level, p1, p2 or p3, and only that answer writes the priority,
// on the brief both cards are bound to; the card's answer is acknowledged,
// the issue shows its step again.
func TestPriorityTakesTwoSteps(t *testing.T) {
	t.Parallel()
	iw := newIssueWorld(t)
	d := newCardDesk()
	id := iw.submit(t, "TITLE-7f3a")
	ask := iw.brief(t, id, "BRIEF-7f3a")
	_, err := iw.answer(t, iw.dots, id, ask, "confirmed")
	ok(t, err)
	ok(t, iw.classify(t, id, "small", "improvement"))
	q1, err := iw.requestOwner(t, d, id, store.OwnerCardPriority, "", 0)
	ok(t, err)
	_, err = iw.requestOwner(t, d, id, store.OwnerCardPriority, "", 0)
	wantIs(t, err, store.ErrState, "is open")
	d.answer(q1, "a", "console")
	d.decide(t, iw)
	if len(d.cards) != 2 || d.last().Title != id+" · priority, step 2" || strings.Join(d.last().Options, ",") != "p1,p2,p3" {
		t.Fatalf("the second step: %+v", d.cards)
	}
	noMarkers(t, d.last())
	if v := iw.state(t, id); v.Priority != "" || v.State != "owner_deciding" {
		t.Fatalf("a priority before the second step: %+v", v)
	}
	q2 := d.ids[1]
	d.decide(t, iw) // still open: nothing
	d.answer(q2, "b", "console")
	d.decide(t, iw)
	v := iw.state(t, id)
	if v.Priority != "p2" || v.State != "classified" || v.WaitingOn != "crew" {
		t.Fatalf("after the level: %+v", v)
	}
	var by, channel, question string
	ok(t, iw.rt.QueryRow(ctx, `SELECT created_by_actor_id, answer_channel, question_id FROM agentrt.issue_check WHERE issue_id = $1
	  AND check_kind = 'priority'`, id).Scan(&by, &channel, &question))
	if by != "controller" || channel != "console" || question != q2 {
		t.Fatalf("the priority's evidence: %s %s %s", by, channel, question)
	}
	if strings.Join(d.acked, ",") != q1+","+q2 {
		t.Fatalf("acknowledged %v", d.acked)
	}
	if n := d.decide(t, iw); n != 0 {
		t.Fatalf("settled again: %d", n)
	}
	// The take predicate counts it: the Owner chose on his card.
	_, err = iw.take(t, id, fitting)
	ok(t, err)

	// Later and decline are priorities of their own, in one step.
	for _, c := range []struct{ option, want string }{{"b", "later"}, {"c", "declined"}} {
		other := iw.submit(t, "another "+c.want)
		ask := iw.brief(t, other, "brief of "+c.want)
		_, err := iw.answer(t, iw.dots, other, ask, "confirmed")
		ok(t, err)
		q, err := iw.requestOwner(t, d, other, store.OwnerCardPriority, "", 0)
		ok(t, err)
		before := len(d.cards)
		d.answer(q, c.option, "console")
		d.decide(t, iw)
		var got string
		ok(t, iw.rt.QueryRow(ctx, `SELECT result FROM agentrt.issue_check WHERE issue_id = $1 AND check_kind = 'priority'`, other).Scan(&got))
		if got != c.want || len(d.cards) != before {
			t.Fatalf("%s: %s, %d cards", c.option, got, len(d.cards))
		}
	}
}

// An answer counts only from the Owner on the Console, with one of the
// card's options, on the versions it was asked on: a rule's answer, an
// expired card, an answer after the brief changed count for nothing (a note
// says why); a card open on a brief no longer current is withdrawn.
func TestCardAnswersThatDoNotCount(t *testing.T) {
	t.Parallel()
	iw := newIssueWorld(t)
	d := newCardDesk()
	ready := func(title string) string {
		id := iw.submit(t, title)
		ask := iw.brief(t, id, "brief of "+title)
		_, err := iw.answer(t, iw.dots, id, ask, "confirmed")
		ok(t, err)
		return id
	}
	settled := func(q string) string {
		var outcome string
		ok(t, iw.rt.QueryRow(ctx, `SELECT body_json->'card'->>'outcome' FROM agentrt.event WHERE event_type = 'note'
		  AND body_json->'card'->>'state' = 'settled' AND body_json->'card'->>'question_id' = $1`, q).Scan(&outcome))
		return outcome
	}
	priorities := func(id string) int {
		return iw.count(t, `SELECT count(*) FROM agentrt.issue_check WHERE issue_id = '`+id+`' AND check_kind = 'priority'`)
	}

	ruled := ready("ruled")
	q, err := iw.requestOwner(t, d, ruled, store.OwnerCardPriority, "", 0)
	ok(t, err)
	d.answer(q, "b", "rule")
	d.decide(t, iw)
	if settled(q) != "not_counted" || priorities(ruled) != 0 {
		t.Fatalf("a rule's answer counted")
	}
	none := ready("none")
	q, err = iw.requestOwner(t, d, none, store.OwnerCardPriority, "", 0)
	ok(t, err)
	d.answer(q, "none", "console")
	d.decide(t, iw)
	if settled(q) != "not_counted" || priorities(none) != 0 {
		t.Fatalf("none counted")
	}

	expired := ready("expired")
	q, err = iw.requestOwner(t, d, expired, store.OwnerCardPriority, "", 0)
	ok(t, err)
	e := d.questions[q]
	e.Status = "expired"
	d.questions[q] = e
	d.decide(t, iw)
	if settled(q) != "expired" {
		t.Fatal("an expired card is not settled")
	}

	moved := ready("moved")
	q, err = iw.requestOwner(t, d, moved, store.OwnerCardPriority, "", 0)
	ok(t, err)
	iw.brief(t, moved, "a new brief")
	d.decide(t, iw)
	if settled(q) != "superseded" || len(d.withdrawn) != 1 || d.withdrawn[0] != q {
		t.Fatalf("an open card on an old brief: withdrawn %v", d.withdrawn)
	}

	late := ready("late")
	q, err = iw.requestOwner(t, d, late, store.OwnerCardPriority, "", 0)
	ok(t, err)
	d.answer(q, "b", "console")
	iw.brief(t, late, "a new brief, before the answer is read")
	d.decide(t, iw)
	if settled(q) != "stale" || priorities(late) != 0 {
		t.Fatal("an answer on an old brief counted")
	}
}

// request_owner's refusals: no brief, live work, a card of the same purpose
// open, a point the design does not have or a card too small for its
// letters, a brief's confirmation that does not ask the Owner, a dispute
// before two rounds; only the crew asks.
func TestRequestOwnerRefuses(t *testing.T) {
	t.Parallel()
	iw := newIssueWorld(t)
	d := newCardDesk()
	id := iw.submit(t, "refusals")
	_, err := iw.requestOwner(t, d, id, store.OwnerCardPriority, "", 0)
	wantIs(t, err, store.ErrState, "has no brief")
	iw.brief(t, id, "a brief")
	_, err = iw.requestOwner(t, d, id, store.OwnerCardConfirmBrief, "", 0)
	wantIs(t, err, store.ErrInvalid, "through her agent")
	_, err = iw.requestOwner(t, d, id, store.OwnerCardReviewDispute, "", 0)
	wantIs(t, err, store.ErrState, "no design")
	_, err = iw.requestOwner(t, d, id, "priority_level", "", 0)
	wantIs(t, err, store.ErrInvalid, "purpose")
	_, err = iw.requestOwner(t, d, id, store.OwnerCardDesignDecision, "D1", 5)
	wantIs(t, err, store.ErrInvalid, "2 to 4 letters")
	_, err = iw.requestOwner(t, d, id, store.OwnerCardPriority, "D1", 0)
	wantIs(t, err, store.ErrInvalid, "only a design's point")
	for _, w := range []store.Writer{iw.dots, {Actor: "seat:chief_of_staff", Token: "seat"}} {
		_, err = store.RequestOwner(ctx, iw.rt, w, iw.key(), store.OwnerCardRequest{IssueID: id, ExpectedVersion: iw.state(t, id).Version,
			Purpose: store.OwnerCardPriority}, d.ask)
		wantIs(t, err, store.ErrInvalid, "is not the crew")
	}
	if len(d.cards) != 0 {
		t.Fatalf("cards asked: %+v", d.cards)
	}
	iw.readySmall(t, id, "improvement")
	_, err = iw.take(t, id, fitting)
	ok(t, err)
	_, err = iw.requestOwner(t, d, id, store.OwnerCardPriority, "", 0)
	wantIs(t, err, store.ErrState, "has live work")
}

// A BI mission outside the standing envelope waits on the Owner's card,
// by the template (review 522 A1 closed in slice 2): the issue's number as
// its work id, the mission's number, its two options, nothing of the
// mission's material or the issue's text; its answer starts the mission as
// any mission's card does.
func TestIssueMissionCardIsTheTemplate(t *testing.T) {
	t.Parallel()
	iw := newIssueWorld(t)
	d := newCardDesk()
	id := iw.submit(t, "TITLE-7f3a")
	iw.readySmall(t, id, "improvement")
	big := store.MissionProposal{Title: "MISSION-7f3a", Goal: "MISSION-7f3a goal", Success: []string{"MISSION-7f3a works"}, MaxAttempts: 100,
		MaxWallHours: 48}
	out, err := store.TakeIssue(ctx, iw.rt, iw.crew, iw.key(), store.IssueTake{IssueID: id, ExpectedVersion: iw.state(t, id).Version,
		Commitment: "COMMIT-7f3a", Mission: big, Ask: d.ask})
	ok(t, err)
	mission := out["mission_id"].(string)
	if out["mission_state"] != "awaiting_owner" || out["question_id"] != d.ids[0] || len(d.cards) != 1 {
		t.Fatalf("taken: %v, cards %+v", out, d.cards)
	}
	c := d.cards[0]
	noMarkers(t, c)
	if c.Title != id+" · start mission "+mission || strings.Join(c.Options, ",") != store.MissionStart+","+store.MissionNotStart {
		t.Fatalf("the mission's card %+v", c)
	}
	if n := iw.count(t, `SELECT count(*) FROM agentrt.event WHERE event_type = 'issue_taken' AND body_json->>'question_id' = '`+d.ids[0]+`'`); n != 1 {
		t.Fatalf("issue_taken naming the card: %d", n)
	}
	d.answer(d.ids[0], store.CardApplyKey, "console")
	n, err := store.DecideMissions(ctx, iw.rt, d.question, d.ack)
	ok(t, err)
	var state string
	ok(t, iw.rt.QueryRow(ctx, `SELECT state FROM agentrt.mission WHERE mission_id = $1`, mission).Scan(&state))
	if n != 1 || state != "active" {
		t.Fatalf("decided %d, mission %s", n, state)
	}
	// DecideIssueCards leaves the mission's card to DecideMissions.
	if n := d.decide(t, iw); n != 0 {
		t.Fatalf("the issue cards' pass settled %d", n)
	}
}

// An agent's own proposal (I-D6 A): its brief asks the Owner, who confirms
// it on his card; the answer is recorded as his, on the card's channel, and
// the agent itself may not confirm it.
func TestOwnerConfirmsAProposalByCard(t *testing.T) {
	t.Parallel()
	iw := newIssueWorld(t)
	d := newCardDesk()
	out, err := store.SubmitIssue(ctx, iw.rt, iw.dots, iw.key(), store.IssueSubmission{Product: "screening", Kind: "proposal",
		Title: "TITLE-7f3a", OnBehalfOf: store.SelfIssue, Proposal: "PROPOSAL of the agent"})
	ok(t, err)
	id := out["issue_id"].(string)
	ask := iw.brief(t, id, "BRIEF-7f3a")
	_, err = iw.answer(t, iw.dots, id, ask, "confirmed")
	wantIs(t, err, store.ErrInvalid, "the Owner himself confirms")
	q, err := iw.requestOwner(t, d, id, store.OwnerCardConfirmBrief, "", 0)
	ok(t, err)
	noMarkers(t, d.last())
	d.answer(q, "b", "console") // not this version
	d.decide(t, iw)
	if v := iw.state(t, id); v.BriefConfirmed || v.State != "clarifying" {
		t.Fatalf("after not this version: %+v", v)
	}
	var verdict, by string
	ok(t, iw.rt.QueryRow(ctx, `SELECT verdict, answered_by_actor_id FROM agentrt.issue_ask WHERE issue_ask_id = $1`, ask).Scan(&verdict, &by))
	if verdict != "rejected" || by != "owner" {
		t.Fatalf("the answer: %s by %s", verdict, by)
	}
	// A new brief, a new card; this time confirmed.
	iw.brief(t, id, "BRIEF-7f3a, again")
	q, err = iw.requestOwner(t, d, id, store.OwnerCardConfirmBrief, "", 0)
	ok(t, err)
	d.answer(q, "a", "console")
	d.decide(t, iw)
	if v := iw.state(t, id); !v.BriefConfirmed || v.State != "brief_confirmed" {
		t.Fatalf("after confirm: %+v", v)
	}
	// A card the Owner answers that a rule could only have answered on a
	// writer before slice 1b: never an answer of his.
	_, err = store.AnswerAsk(ctx, iw.rt, store.Writer{Actor: "controller", Token: "controller"}, iw.key(), store.AskAnswer{IssueID: id,
		ExpectedVersion: iw.state(t, id).Version, AskID: ask, Words: "x", Verdict: "confirmed", Channel: store.ChannelCard, QuestionID: q,
		AnsweredBy: "rule card-auto-answer v1", AnswerChannel: "rule"})
	wantIs(t, err, store.ErrInvalid, "not a rule's")
}
