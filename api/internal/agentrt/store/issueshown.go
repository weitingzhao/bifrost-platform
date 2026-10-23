package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// What an issue shows: its state and whom it waits on as the reads give them
// (DESIGN-crew-intake §4.3's table, §8; review 524 B1). The stored state is
// a display copy the writes keep; the Owner's cards are the one thing it can
// lag behind: the runtime settles a card that expired, was withdrawn or
// answered in a way that does not count with a note only (0009 lets it write
// no issue_state_changed), and a card can open or close beside another write.
// So the reads derive it from the card records the issue's stream holds
// (issuecard.go): while an Owner's card is open, a pre-take issue shows
// owner_deciding and waits on the Owner; when none is, an issue stored as
// owner_deciding shows the step its evidence has reached and waits on the
// crew (afterOwnerCard is the same rule, for the writes). Every other state
// shows as stored. All in SQL over an issue row i, so a list's filters and
// its rows read the same state.

// openCardSQL says whether issue i has an Owner's card opened and not
// settled (issueCardRecords' rule).
const openCardSQL = `EXISTS (SELECT 1 FROM agentrt.event o WHERE o.event_stream_id = 'issue:' || i.issue_id
  AND ((o.event_type = 'issue_state_changed' AND o.body_json->>'change' = 'ask_owner')
    OR (o.event_type = 'note' AND o.body_json->'card'->>'state' = 'opened'))
  AND NOT EXISTS (SELECT 1 FROM agentrt.event s WHERE s.event_stream_id = o.event_stream_id AND s.event_id > o.event_id
    AND COALESCE(s.body_json->>'question_id', s.body_json->'card'->>'question_id') = COALESCE(o.body_json->>'question_id', o.body_json->'card'->>'question_id')
    AND (s.event_type IN ('issue_priority_set','issue_owner_decided','issue_answered')
      OR (s.event_type = 'note' AND s.body_json->'card'->>'state' = 'settled'))))`

// evidenceStateSQL is the step issue i's evidence on its current brief and
// design has reached (afterOwnerCard's rule).
const evidenceStateSQL = `(CASE WHEN i.brief_confirmed_ask IS NULL THEN 'clarifying'
  WHEN NOT EXISTS (SELECT 1 FROM agentrt.issue_check c WHERE c.issue_id = i.issue_id AND c.check_kind = 'classification'
    AND c.brief_sha256 = (SELECT t.sha256 FROM agentrt.issue_text t WHERE t.issue_text_id = i.brief_text_id)) THEN 'brief_confirmed'
  WHEN i.design_text_id IS NULL THEN 'classified'
  WHEN NOT EXISTS (SELECT 1 FROM agentrt.issue_check c WHERE c.issue_id = i.issue_id AND c.check_kind = 'review'
    AND c.brief_sha256 = (SELECT t.sha256 FROM agentrt.issue_text t WHERE t.issue_text_id = i.brief_text_id)
    AND c.design_sha256 = (SELECT t.sha256 FROM agentrt.issue_text t WHERE t.issue_text_id = i.design_text_id)) THEN 'designing'
  ELSE 'in_review' END)`

// preTakeSQL are the states an Owner's card may stand over.
const preTakeSQL = `i.state IN ('clarifying','brief_confirmed','classified','designing','in_review','owner_deciding')`

// shownStateSQL and shownWaitingSQL are issue i's state and whom it waits
// on, as shown.
const (
	shownStateSQL = `(CASE WHEN ` + preTakeSQL + ` AND ` + openCardSQL + ` THEN 'owner_deciding'
  WHEN i.state = 'owner_deciding' THEN ` + evidenceStateSQL + ` ELSE i.state END)`
	shownWaitingSQL = `(CASE WHEN ` + preTakeSQL + ` AND ` + openCardSQL + ` THEN 'owner'
  WHEN i.state = 'owner_deciding' THEN ` + evidenceWaitingSQL + ` ELSE i.waiting_on END)`
)

// evidenceWaitingSQL is whom issue i waits on at the step its evidence has
// reached: the person asked while the confirmation of its brief is asked
// and unanswered (the Owner for an agent's own proposal), else the crew.
const evidenceWaitingSQL = `(CASE WHEN i.brief_confirmed_ask IS NULL AND EXISTS (SELECT 1 FROM agentrt.issue_ask k WHERE k.issue_id = i.issue_id
    AND k.purpose = 'confirm_brief' AND k.verdict IS NULL AND k.superseded_at IS NULL)
  THEN CASE WHEN i.served_person IS NULL THEN 'owner' ELSE 'user' END ELSE 'crew' END)`

// cardDeclineSQL is the Owner's decline of issue i on his card, standing on
// its current brief (review 524 A2): the newest priority evidence of the
// current brief is "declined", the runtime's record of his answer on a card
// answered on the Console. Its question id, or NULL.
const cardDeclineSQL = `(SELECT CASE WHEN c.result = 'declined' AND c.created_by_actor_id = 'controller' AND c.question_id IS NOT NULL
    AND c.answer_channel = 'console' THEN c.question_id END
  FROM agentrt.issue_check c WHERE c.issue_id = i.issue_id AND c.check_kind = 'priority'
    AND c.brief_sha256 = (SELECT t.sha256 FROM agentrt.issue_text t WHERE t.issue_text_id = i.brief_text_id)
  ORDER BY c.issue_check_id DESC LIMIT 1)`

// shownNextSQL is issue i's next step: the one stored, or, once the Owner
// declined a person's issue on his card, the crew's close of it as declined.
const shownNextSQL = `(CASE WHEN i.next_step IS NOT NULL OR i.next_by IS NOT NULL OR i.next_due_at IS NOT NULL
    THEN jsonb_build_object('by', i.next_by, 'what', i.next_step, 'due_at', i.next_due_at)
  WHEN i.state <> 'closed' AND i.served_person IS NOT NULL AND ` + cardDeclineSQL + ` IS NOT NULL
    THEN jsonb_build_object('by', 'crew', 'what', 'close it declined: the Owner declined it on his card ' || ` + cardDeclineSQL + `) END)`

// shownOf is an issue's state and whom it waits on, as shown.
func shownOf(ctx context.Context, tx pgx.Tx, id string) (state, waiting string, err error) {
	err = tx.QueryRow(ctx, `SELECT `+shownStateSQL+`, `+shownWaitingSQL+` FROM agentrt.issue i WHERE i.issue_id = $1`, id).Scan(&state, &waiting)
	return state, waiting, err
}

// cardDecline is the question of the Owner's decline of the issue on his
// card, standing on its current brief, or "".
func cardDecline(ctx context.Context, tx pgx.Tx, id string) (string, error) {
	var q *string
	err := tx.QueryRow(ctx, `SELECT `+cardDeclineSQL+` FROM agentrt.issue i WHERE i.issue_id = $1`, id).Scan(&q)
	if q == nil {
		return "", err
	}
	return *q, err
}
