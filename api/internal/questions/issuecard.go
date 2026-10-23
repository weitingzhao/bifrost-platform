package questions

import "strings"

// A business issue's card (W-31 BI-1: the intake design, DESIGN-crew-intake-
// 2026-10-22 §5.3 and slice 1b; the Owner 2026-10-23 「X 和 Y 都选 A」). The
// Owner's decision card on a business issue carries the issue's number, the
// card's purpose, a hash prefix and neutral options only; the issue's words
// are on a read-only page only its readers reach. The generic card reads
// (GET /questions, GET /questions/{id}) show a card's answer and withdrawal,
// text included, to viewer and up, so text written on such a card would
// publish what its content contract keeps off it. So every writer that
// closes the card keeps no caller's words on it, before anything is stored:
//   - the Owner's answer (Answer, POST /questions/{id}/answer): one of its
//     options and nothing else; any text, with an option, with "none" or
//     alone, and "none" itself (it always needs text) are refused;
//   - a rule's answer (AnswerAsRule, the runtime's card-auto-answer rule): any
//     answer at all is refused. The card is the Owner's, and the issue's
//     evidence does not count the rule's channel; a rule's answer would only
//     close the card on him (and one with another option keeps why as its
//     text);
//   - the requester's withdrawal (Withdraw: POST /questions/{id}/withdraw,
//     the MCP's withdraw_question, the runtime's own): taken, never refused
//     (the runtime withdraws a card whose mission or plan moved on, and a
//     refusal would leave it open for good), but its reason is the fixed
//     issueCardWithdrawn, never the caller's; the reason is still asked for
//     and checked as on any card. The issue holds the record of why.
//
// This version is the card writers' rollback floor (the Owner's Y=A): once a
// business issue's card exists, no process that writes the question store
// runs a platform older than this one, or that older writer takes "none" with
// text, or a withdrawal's own reason, on it again. That covers every way
// back, the release pipeline's and the others (an Argo rollback, an image set
// by hand).
//
// The test is wider than the work id business issues are created with
// (^BI-[1-9][0-9]*$, from a later slice): any work id that starts "BI-", in
// any case, past surrounding space, so a card a later version names a little
// differently is held too. No card has had such a work id: the store has
// taken only W-, TD- and LANE- ids (workIDPattern) since it began.
func businessIssue(workID string) bool {
	w := strings.TrimSpace(workID)
	return len(w) >= 3 && strings.EqualFold(w[:3], "BI-")
}

// businessIssueCard says whether q is a business issue's card.
func (q Question) businessIssueCard() bool {
	return businessIssue(q.WorkID)
}

// The refusals, as the answer's writers give them, and the one reason a
// business issue's card is withdrawn with.
const (
	issueCardNoText    = "a business issue's card takes no text: answer with one of its options"
	issueCardNoNone    = "a business issue's card is answered with one of its options, not none"
	issueCardNoRule    = "a business issue's card is the Owner's: no rule answers it"
	issueCardWithdrawn = "withdrawn; a business issue's card keeps no reason (the issue holds the record)"
)
