package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync/atomic"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/questions"
)

// A blocked task's decision card (design v5 §5.2 控制器「验收编排」: checks that
// fail with no round left block the task 「并提一条决策卡」; §6.2: an external
// task blocked after its attempt was lost gets one once that attempt's end is
// confirmed; T26's first run, 2026-10-19: a blocked task had no way out, so
// its mission stayed active for ever).
//
// The controller's pass gives every blocked task that needs one a card on
// the question store (AskBlocked), one open card per task at a time, and
// records it as owner_wait_opened, naming the task, the card and the
// task_state_changed that blocked it: the same block never gets a second
// open card, and a task blocked again gets a new one. The card has two
// answers: one more round (blocked -> ready; a task blocked for failing its
// last round gets one round more, round_no and max_rounds + 1) or not doing
// it (blocked -> cancelled). DecideBlocked reads the answer and acts on it in
// one transaction with owner_wait_closed, which says how the card ended; a
// dropped task (outcome dropped) no longer holds its mission open
// (close.go). "None of these" with the Owner's words, a card that expired or
// was withdrawn, closes the card and moves nothing: the task is still
// blocked, and the next pass asks again with a new card (his words on it).
//
// Which blocked tasks get a card:
//   - an implementation whose checks failed in its last round (accept.go) and
//     a planning task whose plan was refused in its last round
//     (planresult.go): one more round runs it again, a plan with the refusal
//     added to its spec as a rework round adds it;
//   - a task blocked at dispatch because a task it depends on was rejected or
//     cancelled (propose.go): one more round runs it without that work;
//   - an external task whose attempt was lost or ended on its host
//     (attempt.go), once every such attempt has a confirmed end: an
//     attempt_ended whose host cleared the process group (group_cleared).
//     Never while an earlier attempt may still be acting: an end reported
//     with the group not cleared gets no card, and since /ended takes one
//     report per attempt, such a task waits for the Owner outside the
//     runtime (v5 §6.2 「或 Owner 手工关闭」; round 2 dispatches no external
//     work, claim.go).
//
// Not given one:
//   - a brief (brief.go): its sender has already been answered that the
//     chief of staff could not answer and may ask again, re-running a brief
//     is none of the Owner's three duties, and the question store keeps at
//     most three of the runtime's cards open at once, which a failing brief
//     must not fill ahead of a plan's or a mission's;
//   - a task of a mission that is not active or paused (a planning task
//     blocked because its mission ended, planresult.go): nothing is left to
//     decide.

// The options of a blocked task's card, in the order the question store keys
// them: the first (CardApplyKey) gives it one more round, the second
// (CardRefuseKey) does not do it.
const (
	BlockedRetry = "Give it one more round"
	BlockedDrop  = "Do not do this task"
)

// The outcomes owner_wait_closed records. OutcomeDropped is the one that lets
// the task's mission close without it (close.go).
const (
	OutcomeRetried = "retried"
	OutcomeDropped = "dropped"
	OutcomeNone    = "none"
	OutcomeMoot    = "moot" // answered after the task moved on, or its mission ended: nothing moves
)

// blockedPage is how many blocked tasks or open cards a pass reads at a time.
// The passes page through all of them, so any size gives the same result; a
// test lowers it while others run.
var blockedPage atomic.Int64

func init() { blockedPage.Store(50) }

// latestBlock is the task_state_changed that blocked t, as the task's last
// state change (t is blocked).
const latestBlock = `JOIN LATERAL (SELECT e.event_id, e.body_json FROM agentrt.event e WHERE e.task_id = t.task_id
    AND e.event_type = 'task_state_changed' ORDER BY e.event_id DESC LIMIT 1) b ON b.body_json->>'to' = 'blocked'`

// needsCard is a blocked task (t, its mission m, its block b) that needs a
// card now: as the comment above says, with no card of this block open.
const needsCard = `t.state = 'blocked' AND t.role <> 'brief' AND m.state IN ('active','paused')
  AND (t.effect_class <> 'external' OR NOT EXISTS (SELECT 1 FROM agentrt.attempt a WHERE a.task_id = t.task_id
    AND (a.state IN ('leased','running','stale') OR (a.state IN ('lost','failed') AND NOT EXISTS (SELECT 1 FROM agentrt.event x
      WHERE x.task_id = t.task_id AND x.attempt_id = a.attempt_id AND x.event_type = 'attempt_ended'
        AND (x.body_json->>'group_cleared')::boolean)))))
  AND NOT EXISTS (SELECT 1 FROM agentrt.event o WHERE o.task_id = t.task_id AND o.event_type = 'owner_wait_opened'
    AND (o.body_json->>'blocked_event_id')::bigint = b.event_id AND ` + cardOpen + `)`

// cardOpen: the card owner_wait_opened o names has not been closed.
const cardOpen = `NOT EXISTS (SELECT 1 FROM agentrt.event c WHERE c.task_id = o.task_id AND c.event_type = 'owner_wait_closed'
      AND c.body_json->>'question_id' = o.body_json->>'question_id')`

// AskBlocked is one pass giving each blocked task that needs a card one, page
// by page, so a task whose card cannot be made does not hold up the ones
// after it. It returns how many cards it asked. A task whose card cannot be
// made now (the question store full or not answering) is tried again by the
// next pass.
func AskBlocked(ctx context.Context, db DB, ask Ask) (int, error) {
	if ask == nil {
		return 0, nil
	}
	n := 0
	var errs []error
	after := ""
	page := int(blockedPage.Load())
	for {
		var ids []string
		if err := InTx(ctx, db, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `SELECT t.task_id FROM agentrt.task t JOIN agentrt.mission m ON m.mission_id = t.mission_id `+
				latestBlock+` WHERE t.task_id > $1 AND `+needsCard+` ORDER BY t.task_id LIMIT $2`, after, page)
			if err != nil {
				return err
			}
			ids, err = pgx.CollectRows(rows, pgx.RowTo[string])
			return err
		}); err != nil {
			return n, errors.Join(append(errs, err)...)
		}
		for _, id := range ids {
			did, err := askBlocked(ctx, db, id, ask)
			if err != nil {
				errs = append(errs, fmt.Errorf("task %s: %w", id, err))
			}
			if did {
				n++
			}
		}
		if len(ids) < page {
			return n, errors.Join(errs...)
		}
		after = ids[len(ids)-1]
	}
}

// askBlocked makes one task's card, under the task's lock, once the task is
// read again as needing one. It asks inside the transaction (as askMission
// does): a card made by a transaction that then fails is found again by its
// key on the next pass.
func askBlocked(ctx context.Context, db DB, taskID string, ask Ask) (bool, error) {
	did := false
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		did = false
		t, err := readTask(ctx, tx, taskID, true)
		if err != nil || t.state != "blocked" {
			return err
		}
		// The mission after the task (kernel.go's order), shared: it cannot
		// stop while this card is made for an open mission.
		if _, err = tx.Exec(ctx, `SELECT 1 FROM agentrt.mission WHERE mission_id = $1 FOR SHARE`, t.mission); err != nil {
			return err
		}
		var block int64
		var reason string
		var failed bool
		err = tx.QueryRow(ctx, `SELECT b.event_id, COALESCE(b.body_json->>'reason', ''), COALESCE((b.body_json->>'failed')::boolean, false)
		  FROM agentrt.task t JOIN agentrt.mission m ON m.mission_id = t.mission_id `+latestBlock+` WHERE t.task_id = $1 AND `+needsCard,
			taskID).Scan(&block, &reason, &failed)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // another pass asked, or it no longer needs one
		}
		if err != nil {
			return err
		}
		var asked int
		var words string
		if err = tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM agentrt.event WHERE task_id = $1 AND event_type = 'owner_wait_opened'
		    AND (body_json->>'blocked_event_id')::bigint = $2),
		  COALESCE((SELECT c.body_json->>'text' FROM agentrt.event c JOIN agentrt.event o ON o.task_id = c.task_id
		      AND o.event_type = 'owner_wait_opened' AND o.body_json->>'question_id' = c.body_json->>'question_id'
		      AND (o.body_json->>'blocked_event_id')::bigint = $2
		    WHERE c.task_id = $1 AND c.event_type = 'owner_wait_closed' AND c.body_json->>'outcome' = 'none'
		    ORDER BY c.event_id DESC LIMIT 1), '')`, taskID, block).Scan(&asked, &words); err != nil {
			return err
		}
		d, err := readDetail(ctx, tx, taskID)
		if err != nil {
			return err
		}
		var title string
		if err = tx.QueryRow(ctx, `SELECT title FROM agentrt.task WHERE task_id = $1`, taskID).Scan(&title); err != nil {
			return err
		}
		check, err := lastCheck(ctx, tx, taskID)
		if err != nil {
			return err
		}
		card := asked + 1
		body := blockedCardBody(blockedCard{task: t, title: title, repo: d.repo, round: d.round, maxRounds: d.maxRounds,
			reason: reason, failed: failed, check: check, words: words, card: card})
		q, err := ask(ctx, Card{Key: blockedCardKey(taskID, block, card), Title: "Blocked in " + t.mission + ": " + firstLine(title),
			Body: body, Options: []string{BlockedRetry, BlockedDrop}, OwnerDuty: "goal", TaskID: taskID,
			Meanwhile: "the task stays blocked and mission " + t.mission + " stays open",
			Urgency:   "when you can: only this task waits for you, and its mission cannot finish without your answer"})
		if err != nil {
			if errors.Is(err, ErrInvalid) {
				return err
			}
			return fmt.Errorf("%w: %v", ErrAsk, err)
		}
		if _, err = Append(ctx, tx, Event{Stream: MissionStream(t.mission), Type: "owner_wait_opened", Actor: "controller",
			MissionID: t.mission, PlanNode: t.step, TaskID: taskID,
			Body: map[string]any{"question_id": q, "blocked_event_id": block, "card": card}}); err != nil {
			return err
		}
		did = true
		return nil
	})
	return did && err == nil, err
}

// blockedCardKey is the card's key in the question store: one per task, block
// and card number, so a retried pass finds the card it made and a card asked
// again after an unusable answer is a new one.
func blockedCardKey(taskID string, block int64, card int) string {
	sum, _ := canon.Sum(map[string]any{"task": taskID, "block": block, "card": card, "route": "blocked"})
	return strings.TrimPrefix(sum, "sha256:")
}

type checkSummary struct {
	method, verdict, attempt string
	round                    int
	report                   string // the start of the check's report, "" when it has none
}

// lastCheck is the task's last verification and the start of its report.
func lastCheck(ctx context.Context, tx pgx.Tx, taskID string) (*checkSummary, error) {
	var c checkSummary
	var evidence string
	err := tx.QueryRow(ctx, `SELECT v.method, v.verdict, v.attempt_id, a.attempt_no, COALESCE(v.evidence_artifact_id, '')
	  FROM agentrt.verification v JOIN agentrt.attempt a ON a.attempt_id = v.attempt_id
	  WHERE v.task_id = $1 ORDER BY v.created_at DESC, v.verification_id DESC LIMIT 1`, taskID).
		Scan(&c.method, &c.verdict, &c.attempt, &c.round, &evidence)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if evidence != "" {
		var body []byte
		if err = tx.QueryRow(ctx, `SELECT substring(body for 4096) FROM agentrt.artifact_blob WHERE artifact_id = $1`, evidence).Scan(&body); err != nil {
			return nil, err
		}
		c.report = strings.TrimSpace(strings.ToValidUTF8(string(body), "?"))
	}
	return &c, nil
}

type blockedCard struct {
	task                   taskRow
	title, repo, reason    string
	round, maxRounds, card int
	failed                 bool // blocked for failing its last round: one more round is round + 1
	check                  *checkSummary
	words                  string // the Owner's last "none of these" on a card of this block
}

// excerptRunes is the most of a report a card quotes; it is cut further to
// keep the card within the question store's limit.
const excerptRunes = 1200

// The parts of a card that may be cut to fit, in the order they are cut,
// and how short each may get: the report's start, the Owner's last words,
// the reason, the title (review 420 A2: a long reason and long words with no
// report left made a card the question store refuses, on every pass).
const (
	cutReport = iota
	cutWords
	cutReason
	cutTitle
)

var cutFloors = [4]int{0, 0, 40, 40}

// blockedCardBody is the card's text: the task, why it is blocked, its last
// check and the start of that check's report, what each answer does. The
// parts above are cut in that order until the body fits the question store's
// limit; what each answer does is never cut.
func blockedCardBody(c blockedCard) string {
	full := [4][]rune{[]rune(c.check.reportOrEmpty()), []rune(oneLineRunes(c.words, 300)), []rune(oneLineRunes(c.reason, 500)),
		[]rune(oneLineRunes(c.title, 200))}
	parts := full
	if len(parts[cutReport]) > excerptRunes {
		parts[cutReport] = parts[cutReport][:excerptRunes]
	}
	for i := 0; ; {
		body := blockedCardText(c, parts, full)
		if questions.BodyBytes(body) <= questions.BodyLimit || i == len(parts) {
			return body
		}
		if len(parts[i]) <= cutFloors[i] {
			i++
			continue
		}
		parts[i] = parts[i][:max(cutFloors[i], len(parts[i])*3/4)]
	}
}

func (c *checkSummary) reportOrEmpty() string {
	if c == nil {
		return ""
	}
	return c.report
}

// blockedCardText is the card with each cuttable part as given; a part
// shorter than it was ends in "…" (the report says it was cut).
func blockedCardText(c blockedCard, parts, full [4][]rune) string {
	text := func(i int) string {
		if len(parts[i]) < len(full[i]) && i != cutReport {
			return string(parts[i]) + "…"
		}
		return string(parts[i])
	}
	var b strings.Builder
	// A repository is any name a proposal gave (review 421 B3): cut, as it is
	// the only other part of no fixed length.
	repo := oneLineRunes(c.repo, 100)
	if repo == "" {
		repo = "no repository"
	}
	fmt.Fprintf(&b, "Task %s (%s, %s) of mission %s is blocked; card %d for this block.\n\nTitle: %s\n\nWhy: %s\n",
		c.task.id, c.task.role, repo, c.task.mission, c.card, text(cutTitle), text(cutReason))
	if c.check != nil {
		fmt.Fprintf(&b, "\nLast check: %s, %s, on attempt %d (%s).\n", c.check.method, c.check.verdict, c.check.round, c.check.attempt)
		if excerpt := text(cutReport); excerpt != "" {
			cut := ""
			if len(parts[cutReport]) < len(full[cutReport]) {
				cut = "\n(cut; the whole report is on the task's page)"
			}
			fmt.Fprintf(&b, "Its report begins:\n\n%s%s\n", excerpt, cut)
		}
	}
	if len(parts[cutWords]) > 0 {
		fmt.Fprintf(&b, "\nYour last answer on this block's card was none of these: %q. The runtime does not act on words; choose one of the two.\n",
			text(cutWords))
	} else if c.words != "" {
		b.WriteString("\nYour last answer on this block's card was none of these (your words are on that card). The runtime does not act on words; choose one of the two.\n")
	}
	more := "it goes back to ready as it is, and a host takes it as usual"
	switch {
	case c.task.effect == "external":
		// claim.go dispatches no external work in round 2.
		more = "it goes back to ready, where it waits: no host takes external work yet"
	case c.failed:
		more = fmt.Sprintf("it goes back to ready for round %d of %d, and a host takes it as usual", c.round+1, c.round+1)
	}
	fmt.Fprintf(&b, "\n%s: %s.\n%s: the task is cancelled; mission %s finishes once every other task is accepted "+
		"(a task that depends on this one is then blocked and gets a card of its own).\n", BlockedRetry, more, BlockedDrop, c.task.mission)
	return b.String()
}

// roundSuffix is how a plan's block for its last round ends its reason,
// " (round 2 of 2)" (refusePlan); the next round's spec says why without it.
var roundSuffix = regexp.MustCompile(` \(round \d+ of \d+\)$`)

type openCard struct {
	task, question string
	block          int64
	event          int64
}

// DecideBlocked is one pass over the open cards of blocked tasks: an answered
// card is acted on (one more round, or the task dropped), and "none of
// these", an expired or a withdrawn card closes it and moves nothing. The
// answer as read is kept (question_seen) in the transaction that acts on it.
// It returns how many cards it closed. With ack, the answers it used are then
// acknowledged.
func DecideBlocked(ctx context.Context, db DB, question func(context.Context, string) (Question, bool),
	ack func(context.Context, string) error) (int, error) {
	n := 0
	var errs []error
	var after int64
	size := int(blockedPage.Load())
	for {
		var page []openCard
		err := InTx(ctx, db, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `SELECT o.task_id, o.body_json->>'question_id', (o.body_json->>'blocked_event_id')::bigint, o.event_id
			  FROM agentrt.event o WHERE o.event_type = 'owner_wait_opened' AND o.task_id IS NOT NULL AND o.event_id > $1 AND `+cardOpen+`
			  ORDER BY o.event_id LIMIT $2`, after, size)
			if err != nil {
				return err
			}
			page, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (openCard, error) {
				var c openCard
				return c, r.Scan(&c.task, &c.question, &c.block, &c.event)
			})
			return err
		})
		if err != nil {
			return n, errors.Join(append(errs, err)...)
		}
		for _, c := range page {
			did, derr := decideBlocked(ctx, db, c, question)
			if derr != nil {
				errs = append(errs, fmt.Errorf("task %s: %w", c.task, derr))
			}
			if did {
				n++
			}
		}
		if len(page) < size {
			break
		}
		after = page[len(page)-1].event
	}
	if ack != nil {
		errs = append(errs, ackBlockedCards(ctx, db, ack))
	}
	return n, errors.Join(errs...)
}

// decideBlocked acts on one card; did is whether it closed it.
func decideBlocked(ctx context.Context, db DB, c openCard, question func(context.Context, string) (Question, bool)) (bool, error) {
	q, ok := question(ctx, c.question)
	if !ok || q.Status == "open" {
		return false, nil
	}
	outcome := ""
	switch {
	case q.Status == QuestionAnswered && q.Option == CardApplyKey:
		outcome = OutcomeRetried
	case q.Status == QuestionAnswered && q.Option == CardRefuseKey:
		outcome = OutcomeDropped
	case q.Status == QuestionAnswered && q.Option == "none":
		// The question store always offers "none of these" with the Owner's
		// words (review 190 A2): nothing the runtime can act on.
		outcome = OutcomeNone
	case q.Status == QuestionAnswered:
		return false, fmt.Errorf("question %s was answered %q, no option of its card", c.question, q.Option)
	case q.Status == "expired" || q.Status == "withdrawn":
		outcome = q.Status
	default:
		return false, nil
	}
	seenBody, _, err := questionSeenBody(c.question, q)
	if err != nil {
		return false, err
	}
	key, err := canon.Encode(map[string]any{"question": c.question})
	if err != nil {
		return false, err
	}
	did := false
	err = InTx(ctx, db, func(tx pgx.Tx) error {
		did = false
		t, err := readTask(ctx, tx, c.task, true)
		if err != nil {
			return err
		}
		// The mission after the task (kernel.go's order), shared: it cannot
		// stop or end between this read and the move below (review 420 B2).
		var mstate string
		if err = tx.QueryRow(ctx, `SELECT state FROM agentrt.mission WHERE mission_id = $1 FOR SHARE`, t.mission).Scan(&mstate); err != nil {
			return err
		}
		var open bool
		var latest int64
		var reason string
		var failed bool
		if err = tx.QueryRow(ctx, `SELECT NOT EXISTS (SELECT 1 FROM agentrt.event WHERE task_id = $1 AND event_type = 'owner_wait_closed'
		    AND body_json->>'question_id' = $2),
		  COALESCE((SELECT e.event_id FROM agentrt.event e WHERE e.task_id = $1 AND e.event_type = 'task_state_changed' ORDER BY e.event_id DESC LIMIT 1), 0),
		  COALESCE((SELECT e.body_json->>'reason' FROM agentrt.event e WHERE e.event_id = $3), ''),
		  COALESCE((SELECT (e.body_json->>'failed')::boolean FROM agentrt.event e WHERE e.event_id = $3), false)`,
			c.task, c.question, c.block).Scan(&open, &latest, &reason, &failed); err != nil {
			return err
		}
		if !open {
			return nil // another pass decided it
		}
		if _, err = recordIfChanged(ctx, tx, "question_seen", string(key), func(context.Context) (map[string]any, bool, error) {
			return seenBody, true, nil
		}); err != nil {
			return err
		}
		acted := outcome
		if (outcome == OutcomeRetried || outcome == OutcomeDropped) && (t.state != "blocked" || latest != c.block ||
			(mstate != "active" && mstate != "paused")) {
			acted = OutcomeMoot
		}
		var spec string
		if acted == OutcomeRetried && failed && t.role == "plan" {
			// A plan refused in its last round goes again with the refusal in
			// its spec, as a rework round does (refusePlan).
			d, derr := readDetail(ctx, tx, t.id)
			if derr != nil {
				return derr
			}
			why := strings.TrimSpace(roundSuffix.ReplaceAllString(reason, ""))
			if spec, err = reworkSpec(ctx, tx, d.spec, d.round+1, why); err != nil {
				return err
			}
		}
		closed := map[string]any{"question_id": c.question, "outcome": acted}
		if q.Option != "" {
			closed["answer"] = q.Option
		}
		if q.Text != "" {
			closed["text"] = q.Text
		}
		if _, err = Append(ctx, tx, Event{Stream: MissionStream(t.mission), Type: "owner_wait_closed", Actor: "controller",
			MissionID: t.mission, PlanNode: t.step, TaskID: t.id, Body: closed}); err != nil {
			return err
		}
		did = true
		switch acted {
		case OutcomeRetried:
			if err = moveTask(ctx, tx, "controller", t, "ready", fmt.Sprintf("the Owner gave it one more round (question %s)", c.question)); err != nil {
				return err
			}
			if failed {
				// One more round: round_no and its bound both go up by one.
				_, err = tx.Exec(ctx, `UPDATE agentrt.task SET round_no = round_no + 1, max_rounds = GREATEST(max_rounds, round_no + 1),
				  spec_artifact_id = COALESCE(NULLIF($2, ''), spec_artifact_id) WHERE task_id = $1`, t.id, spec)
			}
			return err
		case OutcomeDropped:
			return moveTask(ctx, tx, "controller", t, "cancelled", fmt.Sprintf("the Owner chose not to do it (question %s)", c.question))
		}
		return nil
	})
	return did && err == nil, err
}

// ackBlockedCards acknowledges the answers the decisions of the last 7 days
// used, recording a note on the task once the question store took it (or no
// longer has it); a failure leaves it for the next pass.
func ackBlockedCards(ctx context.Context, db DB, ack func(context.Context, string) error) error {
	type card struct{ mission, task, step, question string }
	var cards []card
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT DISTINCT d.mission_id, d.task_id, COALESCE(d.plan_node_id, ''), d.body_json->>'question_id'
		  FROM agentrt.event d WHERE d.event_type = 'owner_wait_closed' AND d.task_id IS NOT NULL AND d.body_json ? 'answer'
		    AND d.at > clock_timestamp() - interval '7 days'
		    AND NOT EXISTS (SELECT 1 FROM agentrt.event n WHERE n.event_type = 'note' AND n.task_id = d.task_id
		      AND n.body_json->>'text' IN ('acknowledged question ' || (d.body_json->>'question_id'),
		                                  'question ' || (d.body_json->>'question_id') || ' is gone from the question store'))
		  LIMIT 100`)
		if err != nil {
			return err
		}
		cards, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (card, error) {
			var c card
			return c, r.Scan(&c.mission, &c.task, &c.step, &c.question)
		})
		return err
	})
	if err != nil {
		return err
	}
	var errs []error
	for _, c := range cards {
		text := "acknowledged question " + c.question
		if aerr := ack(ctx, c.question); errors.Is(aerr, ErrQuestionGone) {
			text = "question " + c.question + " is gone from the question store"
		} else if aerr != nil {
			errs = append(errs, fmt.Errorf("acknowledge %s: %w", c.question, aerr))
			continue
		}
		if nerr := InTx(ctx, db, func(tx pgx.Tx) error {
			_, err := Append(ctx, tx, Event{Stream: MissionStream(c.mission), Type: "note", Actor: "controller", MissionID: c.mission,
				PlanNode: c.step, TaskID: c.task, Body: map[string]any{"text": text}})
			return err
		}); nerr != nil {
			errs = append(errs, nerr)
		}
	}
	return errors.Join(errs...)
}
