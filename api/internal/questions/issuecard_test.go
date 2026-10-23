package questions

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

// herWords stands for what a business issue's card must never carry: words
// typed on it would be read back by every viewer of the generic card routes.
const herWords = "her-words-7f3a: the receipt total is wrong again"

// issueCard is a business issue's card as a later version asks it (this one
// cannot: workIDPattern takes no BI- id): created here on W-31 with three
// neutral options, then given work in the store, as that version leaves it.
func issueCard(t *testing.T, s *Service, session, key, work string, extra map[string]any) string {
	t.Helper()
	fields := map[string]any{"work_id": "W-31", "options": []map[string]string{{"label": "p1"}, {"label": "p2"}, {"label": "p3"}}}
	for k, v := range extra {
		fields[k] = v
	}
	id := mustCreate(t, s, session, key, fields)["id"].(string)
	setWorkID(t, s, id, work)
	return id
}

func setWorkID(t *testing.T, s *Service, id, work string) {
	t.Helper()
	if err := s.store.update(func(doc *document) error {
		i, ok := doc.find(id)
		if !ok {
			return fmt.Errorf("no question %s", id)
		}
		doc.Questions[i].WorkID = work
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// snap is what a write may move: the store's bytes and the audit log's.
// Every card a test here asks lives a day, so no sweep moves either while it
// runs (Service.apply expires due cards before anything else).
type snap struct{ store, audit string }

func snapshot(t *testing.T, s *Service) snap {
	t.Helper()
	return snap{readRaw(t, s), readAudit(t, s)}
}

// unchanged fails unless the store and the audit log hold exactly what they
// held at before.
func unchanged(t *testing.T, s *Service, before snap, what string) {
	t.Helper()
	after := snapshot(t, s)
	if after.store != before.store {
		t.Fatalf("%s: the store changed:\n%s\n%s", what, before.store, after.store)
	}
	if after.audit != before.audit {
		t.Fatalf("%s: the audit log changed:\n%s\n%s", what, before.audit, after.audit)
	}
}

// openUntouched is unchanged for a card that must still be open, with no
// answer and no withdrawal.
func openUntouched(t *testing.T, s *Service, id string, before snap, what string) {
	t.Helper()
	unchanged(t, s, before, what)
	q, ok, err := s.Lookup(id)
	if err != nil || !ok || q.Status != StatusOpen || q.Answer != nil || q.Withdrawal != nil {
		t.Fatalf("%s: the card is %+v %+v (%v %v)", what, q, q.Answer, ok, err)
	}
}

// audited fails unless the audit log is before's with one record put in
// front: the one want describes, its id and time aside.
func audited(t *testing.T, s *Service, before snap, want actuation.AuditRecord) {
	t.Helper()
	var was, now []actuation.AuditRecord
	if err := json.Unmarshal([]byte(before.audit), &was); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(readAudit(t, s)), &now); err != nil {
		t.Fatal(err)
	}
	if len(now) != len(was)+1 || !reflect.DeepEqual(now[1:], was) {
		t.Fatalf("the audit log: %d records after %d; want one more in front", len(now), len(was))
	}
	got := now[0]
	got.ID, got.At = "", time.Time{}
	if got != want {
		t.Fatalf("the audit record: %+v; want %+v", got, want)
	}
}

// sameJSON fails unless got and want encode to the same JSON.
func sameJSON(t *testing.T, what string, got, want any) {
	t.Helper()
	g, w := string(mustJSON(t, got)), string(mustJSON(t, want))
	if g != w {
		t.Fatalf("%s:\n got %s\nwant %s", what, g, w)
	}
}

func TestBusinessIssueWorkIDs(t *testing.T) {
	for work, want := range map[string]bool{
		"BI-1": true, "BI-42": true, "BI-0": true, "BI-01": true, "bi-7": true, " BI-7 ": true, "Bi-x": true, "BI-": true,
		"": false, "W-31": false, "TD-411": false, "LANE-C": false, "LANE-BI-1": false, "B-1": false, "BI": false, "BI1": false, "XBI-1": false,
	} {
		if got := businessIssue(work); got != want {
			t.Errorf("businessIssue(%q) = %v; want %v", work, got, want)
		}
	}
}

// W-31 BI-1 slice 1b (the Owner 2026-10-23 「X 和 Y 都选 A」): the Owner's
// answer on a business issue's card is one of its options and nothing else,
// whatever allow_text says. Text with none, with an option or alone, and none
// itself, are refused before anything is stored: the card stays open and the
// store's bytes do not move. An option alone is taken, with no text; asked
// again it is the same answer, and text on the answered card is still
// refused.
func TestIssueCardTakesOnlyAnOption(t *testing.T) {
	for _, allow := range []bool{false, true} {
		t.Run(fmt.Sprintf("allow_text=%v", allow), func(t *testing.T) {
			s := testSvc(t)
			id := issueCard(t, s, "thread-a", "k-issue-card-priority", "BI-7", map[string]any{"allow_text": allow})
			before := snapshot(t, s)
			for name, c := range map[string]struct {
				body  map[string]any
				field string
				msg   string
			}{
				"none and text":    {map[string]any{"option": "none", "text": herWords, "channel": "console"}, "text", issueCardNoText},
				"option and text":  {map[string]any{"option": "a", "text": herWords, "channel": "console"}, "text", issueCardNoText},
				"text alone":       {map[string]any{"text": herWords, "channel": "console"}, "text", issueCardNoText},
				"text, no channel": {map[string]any{"option": "b", "text": herWords}, "text", issueCardNoText},
				"none alone":       {map[string]any{"option": "none", "channel": "console"}, "option", issueCardNoNone},
				"nothing":          {map[string]any{"channel": "console"}, "option", issueCardNoNone},
			} {
				r := answer(t, s, id, c.body)
				if r.Code != http.StatusBadRequest || r.Body.(map[string]any)["error"] != c.msg {
					t.Fatalf("%s: %d %v; want 400 %q", name, r.Code, r.Body, c.msg)
				}
				openUntouched(t, s, id, before, name)
			}
			// An option that is not the card's is refused as before.
			if r := answer(t, s, id, map[string]any{"option": "d", "channel": "console"}); r.Code != http.StatusBadRequest {
				t.Fatalf("no such option: %d %v", r.Code, r.Body)
			}
			openUntouched(t, s, id, before, "no such option")

			r := answer(t, s, id, map[string]any{"option": "b", "channel": "console"})
			if r.Code != http.StatusOK {
				t.Fatalf("an option alone: %d %v", r.Code, r.Body)
			}
			q, ok, err := s.Lookup(id)
			if err != nil || !ok || q.Status != StatusAnswered || q.Answer == nil || q.Answer.Option != "b" || q.Answer.Text != "" ||
				q.Answer.Channel != "console" || q.Answer.By != "admin" {
				t.Fatalf("the answer: %+v %+v %v %v", q, q.Answer, ok, err)
			}
			audited(t, s, before, actuation.AuditRecord{Actor: "admin", Role: actuation.RoleAdmin, Action: "question.answer", Target: id,
				Status: StatusAnswered, Detail: "session=owner-console principal=admin"})
			answered := snapshot(t, s)
			if r = answer(t, s, id, map[string]any{"option": "b", "channel": "console"}); r.Code != http.StatusOK || r.Body.(map[string]any)["replayed"] != true {
				t.Fatalf("the same answer again: %d %v", r.Code, r.Body)
			}
			unchanged(t, s, answered, "the same answer again")
			for _, body := range []map[string]any{
				{"option": "b", "text": herWords, "channel": "console"},
				{"option": "none", "text": herWords, "channel": "console"},
				{"option": "none", "channel": "console"},
			} {
				if r = answer(t, s, id, body); r.Code != http.StatusBadRequest {
					t.Fatalf("on the answered card %v: %d %v", body, r.Code, r.Body)
				}
				unchanged(t, s, answered, fmt.Sprintf("on the answered card %v", body))
			}
			// Another option is "already answered", as on any card.
			if r = answer(t, s, id, map[string]any{"option": "a", "channel": "console"}); r.Code != http.StatusConflict {
				t.Fatalf("another option on the answered card: %d %v", r.Code, r.Body)
			}
			unchanged(t, s, answered, "another option on the answered card")
			if strings.Contains(answered.store, herWords) || strings.Contains(answered.audit, herWords) {
				t.Fatalf("her words were kept:\n%s\n%s", answered.store, answered.audit)
			}
		})
	}
}

// The guard holds whatever a later version calls the issue (issuecard.go):
// none with text is refused on each.
func TestIssueCardWorkIDVariants(t *testing.T) {
	for i, work := range []string{"BI-1", "BI-120", "bi-3", " BI-4", "BI-05"} {
		s := testSvc(t)
		id := issueCard(t, s, "thread-a", fmt.Sprintf("k-issue-variant-%04d", i), work, map[string]any{"allow_text": true})
		before := snapshot(t, s)
		if r := answer(t, s, id, map[string]any{"option": "none", "text": herWords, "channel": "console"}); r.Code != http.StatusBadRequest {
			t.Fatalf("%q: none and text: %d %v", work, r.Code, r.Body)
		}
		openUntouched(t, s, id, before, work)
	}
}

// Everything but a business issue's card answers exactly as before
// (transition.go parseAnswer), in full: the status, the whole response, the
// stored answer (option, text, who, when, channel), the store's bytes and
// the audit log. With allow_text false, none with text and an option alone
// are taken (text that cleans to nothing is no text), text with an option or
// alone is refused, none alone is refused; with allow_text true each is
// taken but none alone; no option and no text, or an option the card does
// not have, are refused on both. A taken answer asked again is replayed and
// another is "already answered", moving nothing. A card with no work id, and
// one on other work, alike.
func TestOtherCardsAnswerAsBefore(t *testing.T) {
	at := time.Date(2026, 10, 23, 6, 0, 0, 0, time.UTC)
	cases := []struct {
		name         string
		allow        bool
		body         map[string]any
		code         int
		err          string // a refusal's error
		option, text string // what is stored when taken
	}{
		{"allow_text false, none and text", false, map[string]any{"option": "none", "text": "See settings."}, http.StatusOK, "", "none", "See settings."},
		{"allow_text false, option alone", false, map[string]any{"option": "a"}, http.StatusOK, "", "a", ""},
		{"allow_text false, option and blank text", false, map[string]any{"option": "a", "text": " \t "}, http.StatusOK, "", "a", ""},
		{"allow_text false, option and text", false, map[string]any{"option": "a", "text": "x"}, http.StatusBadRequest, "text is not allowed", "", ""},
		{"allow_text false, text alone", false, map[string]any{"text": "x"}, http.StatusBadRequest, "text is not allowed", "", ""},
		{"allow_text false, none alone", false, map[string]any{"option": "none"}, http.StatusBadRequest, "text is required", "", ""},
		{"allow_text false, nothing", false, map[string]any{}, http.StatusBadRequest, "option or text is required", "", ""},
		{"allow_text false, no such option", false, map[string]any{"option": "d"}, http.StatusBadRequest, "option is invalid", "", ""},
		{"allow_text true, none and text", true, map[string]any{"option": "none", "text": "See settings."}, http.StatusOK, "", "none", "See settings."},
		{"allow_text true, option and text", true, map[string]any{"option": "a", "text": "because"}, http.StatusOK, "", "a", "because"},
		{"allow_text true, text alone", true, map[string]any{"text": "a thought"}, http.StatusOK, "", "", "a thought"},
		{"allow_text true, option alone", true, map[string]any{"option": "b"}, http.StatusOK, "", "b", ""},
		{"allow_text true, none alone", true, map[string]any{"option": "none"}, http.StatusBadRequest, "text is required", "", ""},
		{"allow_text true, nothing", true, map[string]any{}, http.StatusBadRequest, "option or text is required", "", ""},
	}
	for _, work := range []string{"", "W-31", "TD-411", "LANE-BI-1"} {
		for _, c := range cases {
			t.Run(work+"/"+c.name, func(t *testing.T) {
				s := testSvc(t)
				s.SetClock(func() time.Time { return at })
				extra := map[string]any{"options": twoOptions(), "allow_text": c.allow}
				if work != "" {
					extra["work_id"] = work
				}
				id := mustCreate(t, s, "thread-a", "k-other-card-0001", extra)["id"].(string)
				before := snapshot(t, s)
				body := map[string]any{"channel": "console"}
				for k, v := range c.body {
					body[k] = v
				}
				r := answer(t, s, id, body)
				if r.Code != c.code {
					t.Fatalf("%d %v; want %d", r.Code, r.Body, c.code)
				}
				if c.code != http.StatusOK {
					sameJSON(t, "the refusal", r.Body, map[string]any{"error": c.err})
					openUntouched(t, s, id, before, "the refusal")
					return
				}
				want := &Answer{Option: c.option, Text: c.text, By: "admin", At: at, Channel: "console"}
				sameJSON(t, "the response", r.Body, map[string]any{"id": id, "number": 1, "status": StatusAnswered, "answer": want})
				doc, err := parse([]byte(before.store))
				if err != nil {
					t.Fatal(err)
				}
				doc.Questions[0].Status, doc.Questions[0].Answer = StatusAnswered, want
				raw, err := doc.marshal()
				if err != nil {
					t.Fatal(err)
				}
				if got := readRaw(t, s); got != string(raw) {
					t.Fatalf("the store:\n got %s\nwant %s", got, raw)
				}
				audited(t, s, before, actuation.AuditRecord{Actor: "admin", Role: actuation.RoleAdmin, Action: "question.answer", Target: id,
					Status: StatusAnswered, Detail: "session=owner-console principal=admin"})

				answered := snapshot(t, s)
				r = answer(t, s, id, body)
				sameJSON(t, "the same answer again", r.Body, map[string]any{"id": id, "number": 1, "status": StatusAnswered, "answer": want,
					"replayed": true})
				if r.Code != http.StatusOK {
					t.Fatalf("the same answer again: %d", r.Code)
				}
				unchanged(t, s, answered, "the same answer again")
				other := "a"
				if c.option == "a" {
					other = "b"
				}
				r = answer(t, s, id, map[string]any{"option": other, "channel": "console"})
				if r.Code != http.StatusConflict {
					t.Fatalf("another answer: %d %v", r.Code, r.Body)
				}
				sameJSON(t, "another answer", r.Body, map[string]any{"error": "already answered", "status": StatusAnswered, "id": id, "number": 1,
					"answer": want})
				unchanged(t, s, answered, "another answer")
			})
		}
	}
}

// The rule never answers a business issue's card (the Owner's alone): not
// with the option the card recommends, not with another and why. Each try
// stores nothing and the card stays open and listed for the rule's pass; the
// Owner then answers it with an option. A runtime card on other work is the
// rule's as before.
func TestRuleDoesNotAnswerAnIssueCard(t *testing.T) {
	s := testSvc(t)
	ctx := context.Background()
	far := time.Now().Add(time.Hour)
	id := issueCard(t, s, RuntimeSession, "k-issue-card-runtime", "BI-7", map[string]any{"recommended": 1})
	before := snapshot(t, s)
	for name, c := range map[string]struct{ option, why string }{
		"the option it recommends":         {"a", ""},
		"the option it recommends and why": {"a", "the rule takes it"},
		"another option and why":           {"c", "the card's assessment differs"},
		"none and why":                     {"none", herWords},
	} {
		r := s.AnswerAsRule(ctx, RuntimeSession, "rule card-auto-answer v1", id, c.option, far, c.why)
		if r.Code != http.StatusConflict || r.Body.(map[string]any)["error"] != issueCardNoRule {
			t.Fatalf("%s: %d %v; want 409 %q", name, r.Code, r.Body, issueCardNoRule)
		}
		openUntouched(t, s, id, before, name)
	}
	if cards, err := s.RuleCards(RuntimeSession); err != nil || len(cards) != 1 || cards[0].ID != id || cards[0].Status != StatusOpen {
		t.Fatalf("the runtime's cards: %+v %v", cards, err)
	}
	if r := answer(t, s, id, map[string]any{"option": "a", "channel": "console"}); r.Code != http.StatusOK {
		t.Fatalf("the Owner's option: %d %v", r.Code, r.Body)
	}

	other := mustCreate(t, s, RuntimeSession, "k-runtime-card-on-w31", map[string]any{"title": "A plan change", "work_id": "W-31",
		"options": twoOptions(), "recommended": 1})["id"].(string)
	if r := s.AnswerAsRule(ctx, RuntimeSession, "rule card-auto-answer v1", other, "b", far, "the option that changes nothing"); r.Code != http.StatusOK {
		t.Fatalf("a card on W-31: %d %v", r.Code, r.Body)
	}
	if q, _, _ := s.Lookup(other); q.Answer == nil || q.Answer.Channel != RuleChannel || q.Answer.Text != "the option that changes nothing" {
		t.Fatalf("a card on W-31: %+v", q.Answer)
	}
}

// Over the routes, as the Console and every other client reach them (the
// review's cross probe, the new side): the Owner's text on a business
// issue's card is refused before it is stored, so neither the list nor the
// card, read by a viewer, ever shows it; his option is taken and shown.
func TestIssueCardOverTheRoutes(t *testing.T) {
	h, s := newHTTP(t)
	id := issueCard(t, s, "thread-a", "k-issue-card-routes", "BI-7", map[string]any{"allow_text": true})
	before := snapshot(t, s)
	for _, body := range []string{
		`{"option":"none","text":"` + herWords + `","channel":"console"}`,
		`{"option":"a","text":"` + herWords + `","channel":"console"}`,
		`{"text":"` + herWords + `","channel":"console"}`,
		`{"option":"none","channel":"console"}`,
	} {
		if rec := call(t, h, http.MethodPost, "/api/v1/questions/"+id+"/answer", body, "admin-test-token", "owner-console"); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: %d %s", body, rec.Code, rec.Body)
		}
	}
	openUntouched(t, s, id, before, "over the routes")
	if rec := call(t, h, http.MethodPost, "/api/v1/questions/"+id+"/answer", `{"option":"a","channel":"console"}`, "admin-test-token", "owner-console"); rec.Code != http.StatusOK {
		t.Fatalf("the Owner's option: %d %s", rec.Code, rec.Body)
	}
	for _, path := range []string{"/api/v1/questions/" + id, "/api/v1/questions?status=all", "/api/v1/questions?status=answered"} {
		rec := call(t, h, http.MethodGet, path, "", "viewer-test-token", "viewer-console")
		if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "her-words-7f3a") || !strings.Contains(rec.Body.String(), `"option":"a"`) {
			t.Fatalf("%s as a viewer: %d %s", path, rec.Code, rec.Body)
		}
	}
}

// theirWords stands for a withdrawal's reason a caller writes: on a business
// issue's card it must never be kept.
const theirWords = "withdraw-marker-5b1e: she says the receipt total is wrong"

// withdrawnAs fails unless the withdrawal r of card id, as session
// (principal, role) at at, left exactly the card before with the reason kept
// and the one audit record of a withdrawal, and answered with the card as
// session reads it.
func withdrawnAs(t *testing.T, s *Service, before snap, r Result, id, session, principal string, role actuation.Role, kept string, at time.Time) {
	t.Helper()
	if r.Code != http.StatusOK {
		t.Fatalf("the withdrawal: %d %v", r.Code, r.Body)
	}
	doc, err := parse([]byte(before.store))
	if err != nil {
		t.Fatal(err)
	}
	i, ok := doc.find(id)
	if !ok {
		t.Fatalf("no card %s before", id)
	}
	doc.Questions[i].Status = StatusWithdrawn
	doc.Questions[i].Withdrawal = &Withdrawal{Session: session, Principal: principal, At: at, Reason: kept}
	sameJSON(t, "the response", r.Body, doc.Questions[i].public(session))
	raw, err := doc.marshal()
	if err != nil {
		t.Fatal(err)
	}
	if got := readRaw(t, s); got != string(raw) {
		t.Fatalf("the store:\n got %s\nwant %s", got, raw)
	}
	audited(t, s, before, actuation.AuditRecord{Actor: principal, Role: role, Action: "question.withdraw", Target: id, Status: StatusWithdrawn,
		Detail: "session=" + session + " principal=" + principal})
}

// W-31 BI-1 slice 1b, review 520 A1: a business issue's card is withdrawn as
// any card is, by the runtime that asked it (in-process, as
// agentRuntimeWithdraw does) and by a thread that asked it (over the route,
// as the MCP's withdraw_question does), but the reason kept is the fixed
// one, never the caller's: not in the store, the response or any viewer
// read. The reason is still asked for and checked, and a withdrawal by
// another session or of a card no longer open is refused as before; each
// refusal moves neither the store nor the audit log.
func TestIssueCardWithdrawnWithTheFixedReason(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 10, 23, 6, 0, 0, 0, time.UTC)
	s := testSvc(t)
	s.SetClock(func() time.Time { return at })
	id := issueCard(t, s, RuntimeSession, "k-issue-card-withdraw-rt", "BI-7", map[string]any{"recommended": 1})
	before := snapshot(t, s)
	for name, c := range map[string]struct {
		session, body string
		code          int
	}{
		"no reason":         {RuntimeSession, `{}`, http.StatusBadRequest},
		"no body":           {RuntimeSession, ``, http.StatusBadRequest},
		"a blank reason":    {RuntimeSession, `{"reason":"  "}`, http.StatusBadRequest},
		"a reason too long": {RuntimeSession, `{"reason":"` + strings.Repeat("x", lineLimit+1) + `"}`, http.StatusBadRequest},
		"not a string":      {RuntimeSession, `{"reason":7}`, http.StatusBadRequest},
		"another field":     {RuntimeSession, `{"reason":"r","text":"t"}`, http.StatusBadRequest},
		"another session":   {"thread-a", `{"reason":"` + theirWords + `"}`, http.StatusForbidden},
		"no such card":      {RuntimeSession, `{"reason":"` + theirWords + `"}`, http.StatusNotFound},
	} {
		ref := id
		if name == "no such card" {
			ref = "q_0000000000000000"
		}
		if r := s.Withdraw(ctx, c.session, c.session, "operator", ref, []byte(c.body)); r.Code != c.code {
			t.Fatalf("%s: %d %v; want %d", name, r.Code, r.Body, c.code)
		}
		openUntouched(t, s, id, before, name)
	}
	r := s.Withdraw(ctx, RuntimeSession, RuntimeSession, "operator", id, mustJSON(t, map[string]any{"reason": theirWords}))
	withdrawnAs(t, s, before, r, id, RuntimeSession, RuntimeSession, actuation.RoleOperator, issueCardWithdrawn, at)
	withdrawn := snapshot(t, s)
	if strings.Contains(withdrawn.store, "withdraw-marker-5b1e") || strings.Contains(withdrawn.audit, "withdraw-marker-5b1e") {
		t.Fatalf("their words were kept:\n%s\n%s", withdrawn.store, withdrawn.audit)
	}
	if r = s.Withdraw(ctx, RuntimeSession, RuntimeSession, "operator", id, mustJSON(t, map[string]any{"reason": theirWords})); r.Code != http.StatusConflict {
		t.Fatalf("withdrawn again: %d %v", r.Code, r.Body)
	}
	unchanged(t, s, withdrawn, "withdrawn again")

	// A thread's card, withdrawn over the route; a viewer reads it back.
	h, hs := newHTTP(t)
	hs.SetClock(func() time.Time { return at })
	hid := issueCard(t, hs, "thread-a", "k-issue-card-withdraw-http", "BI-8", nil)
	hbefore := snapshot(t, hs)
	rec := call(t, h, http.MethodPost, "/api/v1/questions/"+hid+"/withdraw", `{"reason":"`+theirWords+`"}`, "reporter-test-token", "thread-b")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("another thread: %d %s", rec.Code, rec.Body)
	}
	openUntouched(t, hs, hid, hbefore, "another thread")
	rec = call(t, h, http.MethodPost, "/api/v1/questions/"+hid+"/withdraw", `{"reason":"`+theirWords+`"}`, "reporter-test-token", "thread-a")
	var body Question
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	withdrawnAs(t, hs, hbefore, Result{Code: rec.Code, Body: body}, hid, "thread-a", "reporter", actuation.RoleReporter, issueCardWithdrawn, at)
	if raw := readRaw(t, hs); strings.Contains(raw, "withdraw-marker-5b1e") {
		t.Fatalf("their words were kept: %s", raw)
	}
	for _, path := range []string{"/api/v1/questions/" + hid, "/api/v1/questions?status=all", "/api/v1/questions?status=withdrawn"} {
		rec := call(t, h, http.MethodGet, path, "", "viewer-test-token", "viewer-console")
		if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "withdraw-marker-5b1e") || !strings.Contains(rec.Body.String(), issueCardWithdrawn) {
			t.Fatalf("%s as a viewer: %d %s", path, rec.Code, rec.Body)
		}
	}
}

// Every other card keeps its withdrawal's reason, the caller's, exactly as
// before: the runtime's and a thread's, with no work id and on other work.
func TestOtherCardsWithdrawnAsBefore(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 10, 23, 6, 0, 0, 0, time.UTC)
	for _, work := range []string{"", "W-31", "LANE-BI-1"} {
		for _, session := range []string{RuntimeSession, "thread-a"} {
			t.Run(work+"/"+session, func(t *testing.T) {
				s := testSvc(t)
				s.SetClock(func() time.Time { return at })
				extra := map[string]any{"options": twoOptions()}
				if work != "" {
					extra["work_id"] = work
				}
				id := mustCreate(t, s, session, "k-other-card-withdraw", extra)["id"].(string)
				before := snapshot(t, s)
				r := s.Withdraw(ctx, session, "reporter", "reporter", id, mustJSON(t, map[string]any{"reason": theirWords}))
				withdrawnAs(t, s, before, r, id, session, "reporter", actuation.RoleReporter, theirWords, at)
			})
		}
	}
}
