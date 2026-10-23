package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

// Briefs, asks and answers (DESIGN-crew-intake §2.1, §2.2, §4.3, §7.1): the
// crew writes a brief and asks the person it serves to confirm it; replies
// to her and asks her to add, to try a delivery or to check an answer. Each
// ask is one row bound to the text it asks about and to the brief current
// then; her answer is her own words, relayed by an agent or a thread, to
// that ask, once. A new brief supersedes every open ask; a new trial or
// answer to check supersedes the ones before it.

// BriefDraft is draft_brief.
type BriefDraft struct {
	IssueID         string
	ExpectedVersion int
	Text            string
}

// DraftBrief writes a new brief for an issue: every open ask is superseded,
// the brief's confirmation cleared, and the person asked (the person served;
// for an agent's own proposal the Owner) is asked to confirm this version.
// First batch (R2-A4): refused while the issue's work is live or a trial or
// an answer to check is open; a scope is not changed in the middle of a
// delivery.
func DraftBrief(ctx context.Context, db DB, w Writer, key string, d BriefDraft) (map[string]any, error) {
	if err := checkText("the brief", d.Text, 0); err != nil {
		return nil, err
	}
	req := map[string]any{"issue_id": d.IssueID, "expected_version": d.ExpectedVersion, "text": d.Text}
	return issueOp(ctx, db, w, "issue-brief", key, req, func(tx pgx.Tx, channel string) (map[string]any, error) {
		r, err := lockIssue(ctx, tx, d.IssueID, d.ExpectedVersion)
		if err != nil {
			return nil, err
		}
		if err := r.open(); err != nil {
			return nil, err
		}
		if m, err := liveWork(ctx, tx, r.ID); err != nil || m != "" {
			return nil, orState(err, "%s has live work (%s): its brief changes once that work has ended", r.ID, m)
		}
		if busy, err := openAsk(ctx, tx, r.ID, AskReadyToTry, AskCheckAnswer); err != nil || busy {
			return nil, orState(err, "%s has a trial or an answer asked of the person: its brief changes once that is settled", r.ID)
		}
		if _, err := supersede(ctx, tx, w.Actor, r.ID, `verdict IS NULL`); err != nil {
			return nil, err
		}
		ids, err := nextIDs(ctx, tx, "issue_text", 1)
		if err != nil {
			return nil, err
		}
		asks, err := nextIDs(ctx, tx, "issue_ask", 1)
		if err != nil {
			return nil, err
		}
		sum := textSHA(d.Text)
		if _, err := appendIssue(ctx, tx, "issue_brief_drafted", w.Actor, r.ID, "",
			map[string]any{"text_id": ids[0], "brief_sha256": sum, "ask_id": asks[0]}); err != nil {
			return nil, err
		}
		if _, err := insertText(ctx, tx, r.ID, w.Actor, issueText{id: ids[0], kind: "brief", channel: channel, body: d.Text, toRequester: true}); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE agentrt.issue SET brief_text_id = $2, brief_confirmed_ask = NULL, state = 'clarifying',
		  waiting_on = $3, version = version + 1 WHERE issue_id = $1`, r.ID, ids[0], r.waitsOn()); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO agentrt.issue_ask (issue_ask_id, issue_id, person, purpose, bound_text_id, bound_sha256, brief_sha256)
		  OVERRIDING SYSTEM VALUE VALUES ($1, $2, $3, 'confirm_brief', $4, $5, $5)`, asks[0], r.ID, r.AsksPerson, ids[0], sum); err != nil {
			return nil, err
		}
		return written(ctx, tx, r.ID, map[string]any{"ask_id": asks[0], "brief_sha256": sum})
	})
}

// orState is err, or ErrState saying why when there is none.
func orState(err error, format string, args ...any) error {
	if err != nil {
		return err
	}
	return fmt.Errorf("%w: %s", ErrState, fmt.Sprintf(format, args...))
}

// IssueReply is reply_to_requester: a reply (Purpose "reply"), or an ask
// with it (needs_info, check_answer, ready_to_try). A trial names the
// delivery: the commitment and mission of the issue's work, the environment
// and the commits; the text says what to try, where, and what counts as
// good.
type IssueReply struct {
	IssueID         string
	ExpectedVersion int
	Purpose         string
	Text            string
	Commitment      int
	MissionID, Env  string
	Commits         []string
}

// ReplyPlain is IssueReply's Purpose for a reply that asks nothing.
const ReplyPlain = "reply"

// Reply writes a reply to the side that brought the issue (to_requester) and,
// with an ask, opens it: a needs_info, a check_answer or a ready_to_try
// supersedes the earlier asks of its purpose. A trial is only of work taken
// for the current brief (R2-A4); an answer to check needs a brief.
func Reply(ctx context.Context, db DB, w Writer, key string, rp IssueReply) (map[string]any, error) {
	if err := checkText("the reply", rp.Text, 0); err != nil {
		return nil, err
	}
	if rp.Purpose != ReplyPlain && !slices.Contains([]string{AskNeedsInfo, AskCheckAnswer, AskReadyToTry}, rp.Purpose) {
		return nil, invalid("a reply is a reply, needs_info, check_answer or ready_to_try, not %q", rp.Purpose)
	}
	trial := rp.Purpose == AskReadyToTry
	if trial {
		switch {
		case rp.Commitment < 1 || rp.MissionID == "":
			return nil, invalid("a trial names the commitment and the mission of the work it delivers")
		case !slices.Contains([]string{"dev", "stg", "prod"}, rp.Env):
			return nil, invalid("a trial names its environment: dev, stg or prod")
		case len(rp.Commits) == 0:
			return nil, invalid("a trial names the commits it delivers")
		}
		for _, c := range rp.Commits {
			if !commitRE.MatchString(c) {
				return nil, invalid("%q is not a commit (40 hex)", c)
			}
		}
	} else if rp.Commitment != 0 || rp.MissionID != "" || rp.Env != "" || len(rp.Commits) > 0 {
		return nil, invalid("only a trial names a delivery")
	}
	req := map[string]any{"issue_id": rp.IssueID, "expected_version": rp.ExpectedVersion, "purpose": rp.Purpose, "text": rp.Text,
		"commitment": rp.Commitment, "mission_id": rp.MissionID, "env": rp.Env, "commits": nonNil(rp.Commits)}
	return issueOp(ctx, db, w, "issue-reply", key, req, func(tx pgx.Tx, channel string) (map[string]any, error) {
		r, err := lockIssue(ctx, tx, rp.IssueID, rp.ExpectedVersion)
		if err != nil {
			return nil, err
		}
		if rp.Purpose != ReplyPlain {
			if err := r.open(); err != nil {
				return nil, err
			}
		}
		if rp.Purpose == AskCheckAnswer && r.briefSHA == "" {
			return nil, fmt.Errorf("%w: %s has no brief yet: an answer is checked against one", ErrState, r.ID)
		}
		if trial {
			var b string
			err := tx.QueryRow(ctx, `SELECT brief_sha256 FROM agentrt.issue_work WHERE issue_id = $1 AND commitment = $2 AND mission_id = $3`,
				r.ID, rp.Commitment, rp.MissionID).Scan(&b)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, invalid("%s has no work C%d on %s", r.ID, rp.Commitment, rp.MissionID)
			}
			if err != nil {
				return nil, err
			}
			if b != r.briefSHA {
				return nil, fmt.Errorf("%w: work C%d on %s was taken for an earlier brief; a trial of the current one is of work taken for it",
					ErrState, rp.Commitment, rp.MissionID)
			}
		}
		if rp.Purpose != ReplyPlain {
			// The earlier asks of this purpose: for a trial and an answer to
			// check, answered ones too (a close rests on the newest, §4.3).
			cond := `purpose = $2 AND verdict IS NULL`
			if rp.Purpose != AskNeedsInfo {
				cond = `purpose = $2`
			}
			if _, err := supersede(ctx, tx, w.Actor, r.ID, cond, rp.Purpose); err != nil {
				return nil, err
			}
		}
		ids, err := nextIDs(ctx, tx, "issue_text", 1)
		if err != nil {
			return nil, err
		}
		if rp.Purpose == ReplyPlain {
			ev, err := appendIssue(ctx, tx, "issue_replied", w.Actor, r.ID, "", map[string]any{"text_id": ids[0]})
			if err != nil {
				return nil, err
			}
			if _, err := insertText(ctx, tx, r.ID, w.Actor, issueText{id: ids[0], kind: "reply", channel: channel, body: rp.Text, toRequester: true}); err != nil {
				return nil, err
			}
			if _, err := tx.Exec(ctx, `UPDATE agentrt.issue SET version = version + 1 WHERE issue_id = $1`, r.ID); err != nil {
				return nil, err
			}
			return written(ctx, tx, r.ID, map[string]any{"update_id": ev.EventID})
		}
		asks, err := nextIDs(ctx, tx, "issue_ask", 1)
		if err != nil {
			return nil, err
		}
		body := map[string]any{"text_id": ids[0], "ask_id": asks[0], "purpose": rp.Purpose}
		if r.briefSHA != "" {
			body["brief_sha256"] = r.briefSHA
		}
		mission := ""
		if trial {
			body["commitment"], body["env"] = rp.Commitment, rp.Env
			mission = rp.MissionID
		}
		ev, err := appendIssue(ctx, tx, "issue_asked", w.Actor, r.ID, mission, body)
		if err != nil {
			return nil, err
		}
		sum, err := insertText(ctx, tx, r.ID, w.Actor, issueText{id: ids[0], kind: "reply", channel: channel, body: rp.Text, toRequester: true})
		if err != nil {
			return nil, err
		}
		var commitment, commits any
		if trial {
			commitment, commits = rp.Commitment, rp.Commits
		}
		if _, err := tx.Exec(ctx, `INSERT INTO agentrt.issue_ask (issue_ask_id, issue_id, person, purpose, bound_text_id, bound_sha256,
		    brief_sha256, commitment, mission_id, env, commits)
		  OVERRIDING SYSTEM VALUE VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
			asks[0], r.ID, r.AsksPerson, rp.Purpose, ids[0], sum, nullable(r.briefSHA), commitment, nullable(mission), nullable(rp.Env), commits); err != nil {
			return nil, err
		}
		state := r.State
		if trial {
			state = "awaiting_user"
		}
		if _, err := tx.Exec(ctx, `UPDATE agentrt.issue SET state = $2, waiting_on = $3, version = version + 1 WHERE issue_id = $1`,
			r.ID, state, r.waitsOn()); err != nil {
			return nil, err
		}
		return written(ctx, tx, r.ID, map[string]any{"update_id": ev.EventID, "ask_id": asks[0]})
	})
}

// AskAnswer is relay_user_response (and the Owner's own answer on the
// Console): the person's own words, verbatim, to one ask (in_reply_to), and
// what they say (verdict); an agent's own remark goes in Note, never as her
// words. The runtime (controller) records the Owner's answer on his card
// (issuecarddecide.go): channel card, the card's question, who answered it
// and by which channel of the question store, never a rule's.
type AskAnswer struct {
	IssueID         string
	ExpectedVersion int
	AskID           int64
	Words           string
	SaidAt          time.Time
	Verdict         string
	Note            string
	Channel         string
	QuestionID      string
	AnsweredBy      string
	AnswerChannel   string
}

// AnswerAsk records the person's answer to an open ask of the issue (§2.1):
// once; from a writer that speaks for the person asked (an agent bound to
// her, a thread relaying the Owner or his relay, the Owner on the Console);
// her words required; a verdict the ask's purpose allows; a brief's
// confirmation only of the brief still current; a withdrawal only before the
// issue is taken. A confirmed brief becomes the issue's confirmed brief. It
// never closes the issue: closing is the crew's, by the close predicate.
func AnswerAsk(ctx context.Context, db DB, w Writer, key string, a AskAnswer) (map[string]any, error) {
	channel, err := writerChannel(w, a.Channel)
	if err != nil {
		return nil, err
	}
	if err := checkText("the person's words", a.Words, MaxIssueTextChars); err != nil {
		return nil, err
	}
	if a.Note != "" {
		if err := checkText("the agent's note", a.Note, MaxIssueTextChars); err != nil {
			return nil, err
		}
	}
	if !slices.Contains(askVerdicts, a.Verdict) {
		return nil, invalid("a verdict is one of %v, not %q", askVerdicts, a.Verdict)
	}
	if !issueIDRE.MatchString(a.IssueID) {
		return nil, invalid("%q is not an issue id (BI-<n>)", a.IssueID)
	}
	card := channel == ChannelCard
	switch {
	case card && (a.QuestionID == "" || a.AnsweredBy == "" || a.AnswerChannel == "" || isRuleChannel(a.AnswerChannel) || a.Note != ""):
		return nil, invalid("an answer on the Owner's card names the card and who answered it, and is not a rule's")
	case !card && (a.QuestionID != "" || a.AnsweredBy != "" || a.AnswerChannel != ""):
		return nil, invalid("only an answer on the Owner's card names a card")
	}
	req := map[string]any{"issue_id": a.IssueID, "expected_version": a.ExpectedVersion, "ask_id": a.AskID, "words": a.Words,
		"said_at": timeOr(a.SaidAt), "verdict": a.Verdict, "note": a.Note, "channel": a.Channel}
	if card {
		req["question_id"], req["answered_by"], req["answer_channel"] = a.QuestionID, a.AnsweredBy, a.AnswerChannel
	}
	return idempotentHeld(ctx, db, w, "issue-answer", key, req, agentSees(ctx, w, a.IssueID), func(tx pgx.Tx) (map[string]any, error) {
		if err := ensureWriter(ctx, tx, w, true); err != nil {
			return nil, err
		}
		r, err := lockIssue(ctx, tx, a.IssueID, a.ExpectedVersion)
		if err != nil {
			return nil, err
		}
		var purpose, person, bound, mission string
		var brief *string
		var answered, superseded bool
		err = tx.QueryRow(ctx, `SELECT purpose, person, bound_sha256, brief_sha256, COALESCE(mission_id, ''), verdict IS NOT NULL,
		    superseded_at IS NOT NULL FROM agentrt.issue_ask WHERE issue_ask_id = $1 AND issue_id = $2 FOR UPDATE`, a.AskID, r.ID).
			Scan(&purpose, &person, &bound, &brief, &mission, &answered, &superseded)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, invalid("%s has no ask %d", r.ID, a.AskID)
		}
		if err != nil {
			return nil, err
		}
		switch {
		case answered || superseded:
			return nil, &StaleError{Why: fmt.Sprintf("ask %d of %s is no longer open (answered or replaced); answer the newest", a.AskID, r.ID),
				Issue: r.IssueView}
		case !slices.Contains(askVerdictsOf[purpose], a.Verdict):
			return nil, invalid("a %s ask is answered %v, not %s", purpose, askVerdictsOf[purpose], a.Verdict)
		case (brief == nil && r.briefSHA != "") || (brief != nil && *brief != r.briefSHA):
			return nil, &StaleError{Why: fmt.Sprintf("ask %d was asked of an earlier brief of %s", a.AskID, r.ID), Issue: r.IssueView}
		case purpose == AskConfirmBrief && a.Verdict == "confirmed" && bound != r.briefSHA:
			return nil, &StaleError{Why: fmt.Sprintf("ask %d confirms a brief of %s that is no longer current", a.AskID, r.ID), Issue: r.IssueView}
		case r.ServedPerson == "" && (purpose == AskConfirmBrief || purpose == AskReadyToTry) && w.Actor != "owner" && !card:
			return nil, invalid("%s is an agent's own proposal: the Owner himself confirms its brief and tries it", r.ID)
		case card && (person != OwnerPerson || purpose != AskConfirmBrief):
			return nil, invalid("the Owner's card answers only the confirmation of a brief that asks him")
		}
		if kindOf(w.Actor) == "thread" && channel == ChannelThread && person != OwnerPerson {
			return nil, invalid("a thread relays %s's answer as the Owner's relay (channel owner_relay)", person)
		}
		if a.Verdict == "withdrawn" {
			var taken bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.issue_work WHERE issue_id = $1)`, r.ID).Scan(&taken); err != nil {
				return nil, err
			}
			if taken {
				return nil, fmt.Errorf("%w: %s was taken; a withdrawal comes before that", ErrState, r.ID)
			}
		}
		n := 1
		if a.Note != "" {
			n = 2
		}
		ids, err := nextIDs(ctx, tx, "issue_text", n)
		if err != nil {
			return nil, err
		}
		body := map[string]any{"ask_id": a.AskID, "text_ids": anyInts(ids), "verdict": a.Verdict, "channel": channel, "person": person}
		answerer := w.Actor
		if card {
			// The Owner answered, on his card; the runtime records it.
			body["question_id"], answerer = a.QuestionID, "owner"
		}
		ev, err := appendIssue(ctx, tx, "issue_answered", w.Actor, r.ID, mission, body)
		if err != nil {
			return nil, err
		}
		if _, err := insertText(ctx, tx, r.ID, w.Actor, issueText{id: ids[0], kind: "response", person: person, channel: channel,
			saidAt: a.SaidAt, body: a.Words}); err != nil {
			return nil, err
		}
		if a.Note != "" {
			if _, err := insertText(ctx, tx, r.ID, w.Actor, issueText{id: ids[1], kind: "note", channel: channel, body: a.Note}); err != nil {
				return nil, err
			}
		}
		var saidAt any
		if !a.SaidAt.IsZero() {
			saidAt = a.SaidAt
		}
		if _, err := tx.Exec(ctx, `UPDATE agentrt.issue_ask SET verdict = $2, words_text_id = $3, said_at = $4, answered_by_actor_id = $5,
		  answer_channel = $6, answered_at = clock_timestamp() WHERE issue_ask_id = $1`, a.AskID, a.Verdict, ids[0], saidAt, answerer, channel); err != nil {
			return nil, err
		}
		state, confirmed := r.State, any(nil)
		switch {
		case purpose == AskConfirmBrief && a.Verdict == "confirmed":
			state, confirmed = "brief_confirmed", a.AskID
		case purpose == AskConfirmBrief && a.Verdict == "rejected":
			state = "clarifying"
		case purpose == AskReadyToTry && r.State == "awaiting_user" && a.Verdict != "withdrawn":
			state = "active"
		}
		if confirmed != nil {
			_, err = tx.Exec(ctx, `UPDATE agentrt.issue SET state = $2, waiting_on = 'crew', brief_confirmed_ask = $3, version = version + 1
			  WHERE issue_id = $1`, r.ID, state, confirmed)
		} else {
			_, err = tx.Exec(ctx, `UPDATE agentrt.issue SET state = $2, waiting_on = 'crew', version = version + 1 WHERE issue_id = $1`, r.ID, state)
		}
		if err != nil {
			return nil, err
		}
		return written(ctx, tx, r.ID, map[string]any{"update_id": ev.EventID})
	})
}
