package questions

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
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

// openUntouched fails unless the store holds exactly before and the card is
// open with no answer, and nothing answered it in the audit log.
func openUntouched(t *testing.T, s *Service, id, before, what string) {
	t.Helper()
	if after := readRaw(t, s); after != before {
		t.Fatalf("%s: the store changed:\n%s\n%s", what, before, after)
	}
	q, ok, err := s.Lookup(id)
	if err != nil || !ok || q.Status != StatusOpen || q.Answer != nil {
		t.Fatalf("%s: the card is %+v %+v (%v %v)", what, q, q.Answer, ok, err)
	}
	if contains(readAudit(t, s), "question.answer") {
		t.Fatalf("%s: the audit log has an answer", what)
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
			before := readRaw(t, s)
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
			if r = answer(t, s, id, map[string]any{"option": "b", "channel": "console"}); r.Code != http.StatusOK || r.Body.(map[string]any)["replayed"] != true {
				t.Fatalf("the same answer again: %d %v", r.Code, r.Body)
			}
			answered := readRaw(t, s)
			for _, body := range []map[string]any{
				{"option": "b", "text": herWords, "channel": "console"},
				{"option": "none", "text": herWords, "channel": "console"},
			} {
				if r = answer(t, s, id, body); r.Code != http.StatusBadRequest {
					t.Fatalf("text on the answered card %v: %d %v", body, r.Code, r.Body)
				}
			}
			if after := readRaw(t, s); after != answered || strings.Contains(after, herWords) {
				t.Fatalf("text on the answered card moved the store:\n%s", after)
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
		before := readRaw(t, s)
		if r := answer(t, s, id, map[string]any{"option": "none", "text": herWords, "channel": "console"}); r.Code != http.StatusBadRequest {
			t.Fatalf("%q: none and text: %d %v", work, r.Code, r.Body)
		}
		openUntouched(t, s, id, before, work)
	}
}

// Everything but a business issue's card answers exactly as before
// (transition.go parseAnswer): with allow_text false, none with text and an
// option alone are taken, text with an option or alone is refused, none
// alone is refused; with allow_text true each is taken but none alone. A
// card with no work id, and one on other work, alike.
func TestOtherCardsAnswerAsBefore(t *testing.T) {
	type want struct {
		code int
		text string
	}
	cases := []struct {
		name  string
		allow bool
		body  map[string]any
		want  want
	}{
		{"allow_text false, none and text", false, map[string]any{"option": "none", "text": "See settings."}, want{http.StatusOK, "See settings."}},
		{"allow_text false, option alone", false, map[string]any{"option": "a"}, want{http.StatusOK, ""}},
		{"allow_text false, option and text", false, map[string]any{"option": "a", "text": "x"}, want{http.StatusBadRequest, ""}},
		{"allow_text false, text alone", false, map[string]any{"text": "x"}, want{http.StatusBadRequest, ""}},
		{"allow_text false, none alone", false, map[string]any{"option": "none"}, want{http.StatusBadRequest, ""}},
		{"allow_text true, none and text", true, map[string]any{"option": "none", "text": "See settings."}, want{http.StatusOK, "See settings."}},
		{"allow_text true, option and text", true, map[string]any{"option": "a", "text": "because"}, want{http.StatusOK, "because"}},
		{"allow_text true, text alone", true, map[string]any{"text": "a thought"}, want{http.StatusOK, "a thought"}},
		{"allow_text true, option alone", true, map[string]any{"option": "b"}, want{http.StatusOK, ""}},
		{"allow_text true, none alone", true, map[string]any{"option": "none"}, want{http.StatusBadRequest, ""}},
	}
	for _, work := range []string{"", "W-31", "TD-411", "LANE-BI-1"} {
		for _, c := range cases {
			t.Run(work+"/"+c.name, func(t *testing.T) {
				s := testSvc(t)
				extra := map[string]any{"options": twoOptions(), "allow_text": c.allow}
				if work != "" {
					extra["work_id"] = work
				}
				id := mustCreate(t, s, "thread-a", "k-other-card-0001", extra)["id"].(string)
				c.body["channel"] = "console"
				r := answer(t, s, id, c.body)
				if r.Code != c.want.code {
					t.Fatalf("%d %v; want %d", r.Code, r.Body, c.want.code)
				}
				q, _, err := s.Lookup(id)
				if err != nil {
					t.Fatal(err)
				}
				if c.want.code != http.StatusOK {
					if q.Status != StatusOpen || q.Answer != nil {
						t.Fatalf("a refused answer was stored: %+v", q.Answer)
					}
					return
				}
				if q.Status != StatusAnswered || q.Answer == nil || q.Answer.Text != c.want.text {
					t.Fatalf("stored %+v; want text %q", q.Answer, c.want.text)
				}
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
	before := readRaw(t, s)
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
	before := readRaw(t, s)
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
