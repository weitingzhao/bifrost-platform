package questions

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// Answers a signed runtime rule gives (TD-390, the Owner 2026-10-20 「STG 用
// A」): the agent runtime's card-auto-answer rule answers a card it asked
// with the option the card itself recommends, once it has been open long
// enough unanswered. Such an answer is the rule's, never the Owner's: its By
// names the rule and its version, its channel is RuleChannel. Nothing here is
// reachable from a route; the runtime calls it in-process.

// RuleChannel is the channel of an answer a rule gave.
const RuleChannel = "rule"

// RuntimeSession is the agent runtime's own session: its cards are asked,
// and answered by its rule, in-process. No route takes it (review 442 B3: a
// thread naming it would ask cards the rule takes for the runtime's).
const RuntimeSession = "agentrt"

// RuleCards is every question requester asked that is open and not past its
// expiry, and every one a rule answered that requester has not acknowledged,
// as the store holds them; it writes nothing.
func (s *Service) RuleCards(requester string) ([]Question, error) {
	doc, err := s.load()
	if err != nil {
		return nil, err
	}
	now := s.clock()
	var out []Question
	for _, q := range doc.Questions {
		open := q.Status == StatusOpen && now.Before(q.ExpiresAt)
		ruled := q.unread() && q.Answer.Channel == RuleChannel
		if q.Requester == requester && (open || ruled) {
			out = append(out, q.clone())
		}
	}
	return out, nil
}

// AnswerAsRule answers question ref of requester with option, as rule, while
// the rule holds (until) and ctx has not ended: both are checked when the
// answer is written (review 442 A2, B2). It refuses a question another
// requester asked, one that is not open, and a business issue's card, which
// is the Owner's alone (issuecard.go). The option is the one the
// question recommends, or, with why (TD-393: the runtime's option that
// changes nothing, when the card recommends none or its AI assessment
// differs), another of the question's options; why is kept as the answer's
// text, so the record says why the rule took it.
func (s *Service) AnswerAsRule(ctx context.Context, requester, rule, ref, option string, until time.Time, why string) Result {
	if err := ValidSession(requester); err != nil {
		return fail(http.StatusBadRequest, err.Error(), nil)
	}
	if strings.TrimSpace(rule) == "" {
		return fail(http.StatusBadRequest, "a rule names itself", nil)
	}
	c := caller{session: requester, principal: clipPrincipal(rule), role: "operator", op: "answer", ref: ref}
	return s.apply(ctx, c, func(doc *document, now time.Time) applied {
		i, ok := doc.find(strings.TrimSpace(ref))
		if !ok {
			return applied{res: fail(http.StatusNotFound, "not found", nil)}
		}
		q := doc.Questions[i]
		switch {
		case ctx.Err() != nil:
			return applied{res: fail(http.StatusServiceUnavailable, "the rule's answer came too late: "+ctx.Err().Error(), nil)}
		case !now.Before(until):
			return applied{res: fail(http.StatusConflict, "the rule no longer holds", nil)}
		case q.Requester != requester:
			return applied{res: fail(http.StatusForbidden, "a rule answers only its own runtime's questions", nil)}
		case q.Status != StatusOpen:
			return applied{res: fail(http.StatusConflict, "not open", map[string]any{"status": q.Status})}
		case q.businessIssueCard():
			// The Owner's alone, with or without why (issuecard.go).
			return applied{res: fail(http.StatusConflict, issueCardNoRule, nil)}
		case option == q.Recommended && option != "":
		case strings.TrimSpace(why) == "":
			return applied{res: fail(http.StatusConflict, "a rule answers with the option the question recommends, or says why it takes another", nil)}
		case !q.hasOption(option):
			return applied{res: fail(http.StatusConflict, "no such option", map[string]any{"option": option})}
		}
		q.Status = StatusAnswered
		q.Answer = &Answer{Option: option, By: c.principal, At: now, Channel: RuleChannel}
		if option != q.Recommended {
			q.Answer.Text = clipRuleWhy(why)
		}
		doc.Questions[i] = q
		return applied{
			res:     Result{Code: http.StatusOK, Body: answerBody(q, false)},
			changed: true,
			extra:   []event{{"question.answer", q.ID, StatusAnswered, "rule=" + c.principal}},
		}
	})
}

// hasOption says whether key is one of the question's options.
func (q Question) hasOption(key string) bool {
	for _, o := range q.Options {
		if o.Key == key {
			return true
		}
	}
	return false
}

// ruleWhyLimit bounds the text a rule's answer keeps.
const ruleWhyLimit = 500

// clipRuleWhy is why on one line, at most ruleWhyLimit bytes, whole runes.
func clipRuleWhy(why string) string {
	why = strings.Join(strings.Fields(why), " ")
	for len(why) > ruleWhyLimit {
		r := []rune(why)
		why = string(r[:len(r)-1])
	}
	return why
}
