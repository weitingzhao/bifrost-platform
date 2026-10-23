package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/questions"
)

// The Owner's decision cards on a business issue (DESIGN-crew-intake §2.2
// request_owner, §4.3, §5.3; the Owner chose I-D7 A, X A and Y A on
// 2026-10-23). The runtime asks them, never a thread's ask_owner (an answer
// to a thread's question grants nothing, W-65), from a fixed template: the
// card carries the issue's number as its work id, its purpose, the first 12
// hex of the brief's (and design's) sha256, a link to the read-only issue
// page and neutral options, and not one word of the issue. The card store
// is read by viewer and up and fed to the AI assessment, so nothing on a
// card may be the issue's text; the issue page, for the issue readers only,
// has it. A business issue's card takes an option and no text (slice 1b),
// is not assessed and is never answered by a rule; an answer counts only
// from the Console (the Owner's admin token), never from the rule channel.
//
// A card is recorded where the issue's history is: request_owner writes
// issue_state_changed (change ask_owner, the issue waiting on the Owner),
// naming the question, its purpose and the versions it is bound to. The
// priority is asked in two steps (X A: a card holds 2 to 4 options): take
// it on, later or decline; on "take it on" the runtime asks the level, p1,
// p2 or p3, recording that card on a note of its own. A card is settled by
// the evidence its answer writes (issue_priority_set, issue_owner_decided,
// issue_answered, each naming the question) or, when its answer counts for
// nothing (it expired, was withdrawn, came by another channel, or the issue
// moved past the versions it is bound to), by a note saying so; a card still
// open on versions no longer current is withdrawn.

// The purposes of an Owner's card on an issue.
const (
	OwnerCardPriority       = "priority"        // step 1: take it on, later, decline
	OwnerCardPriorityLevel  = "priority_level"  // step 2: p1, p2, p3
	OwnerCardConfirmBrief   = "confirm_brief"   // the Owner confirms a brief that asks him (I-D6)
	OwnerCardDesignDecision = "design_decision" // a design's point D<n>: its letters
	OwnerCardReviewDispute  = "review_dispute"  // after the review rounds: proceed, stop, another round
	ownerCardMission        = "mission"         // a BI mission outside the standing envelope (missiondecide.go decides it)
)

// RequestablePurposes are the purposes request_owner takes; the level of a
// priority is asked by the runtime itself.
var RequestablePurposes = []string{OwnerCardPriority, OwnerCardConfirmBrief, OwnerCardDesignDecision, OwnerCardReviewDispute}

// IssuePageLink is the Console's read-only page of an issue, the link on its
// cards.
func IssuePageLink(issue string) string { return "#issue?id=" + issue }

// ownerCard is the fixed template of an Owner's card on an issue: ids,
// hashes and neutral words only. letters is how many letters a design's
// point offers; mission is a BI mission's number.
func ownerCard(issue, purpose, decision, brief, design string, letters int, mission string) Card {
	what, line := purpose, ""
	var options []string
	switch purpose {
	case OwnerCardPriority:
		what, line = "priority", "Its priority, step 1 of 2: take it on (its level, p1, p2 or p3, is the next card), later, or decline."
		options = []string{"Take it on: set its level next", "Later", "Decline"}
	case OwnerCardPriorityLevel:
		what, line = "priority, step 2", "Its priority, step 2 of 2: the level."
		options = []string{"p1", "p2", "p3"}
	case OwnerCardConfirmBrief:
		what, line = "confirm brief", "Whether this version of its brief is what is asked."
		options = []string{ConfirmLabel(brief), "Not this version"}
	case OwnerCardDesignDecision:
		what, line = "design decision "+decision, "Point "+decision+" of its design: the letters are explained in the design's decision table."
		for i := 0; i < letters; i++ {
			options = append(options, string(rune('A'+i)))
		}
	case OwnerCardReviewDispute:
		what, line = "review dispute", "Its design's independent review did not settle in its rounds: go on, stop, or one more round."
		options = []string{"Proceed", "Stop", "Another round"}
	case ownerCardMission:
		what, line = "start mission "+mission, "Whether mission "+mission+", taken for it, starts: it does not fit the standing envelope; its record says which bound."
		options = []string{MissionStart, MissionNotStart}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Business issue %s. %s\n\nBrief sha256 %s", issue, line, short(brief))
	if design != "" {
		fmt.Fprintf(&b, "; design sha256 %s", short(design))
	}
	fmt.Fprintf(&b, ".\n\nThis card carries no text of the issue: read it on the issue's page (Console, %s). "+
		"Answer with one of the options; a business issue's card takes no text.", IssuePageLink(issue))
	return Card{Title: issue + " · " + what, Body: b.String(), Options: options, Meanwhile: "the issue waits for this answer",
		OwnerDuty: "goal", Urgency: "when you can: only this issue waits for you", WorkID: issue, Link: IssuePageLink(issue),
		LinkText: "Issue " + issue}
}

// ConfirmLabel is the option that confirms a brief: the first 12 hex of its
// sha256, which the issue page shows in full.
func ConfirmLabel(brief string) string { return "Confirm " + short(brief) }

// OwnerCardRequest is request_owner: the issue at the version read, the
// purpose, and for a design's point the point and how many letters it
// offers (2 to 4). Nothing else: the card's words are the template's.
type OwnerCardRequest struct {
	IssueID         string
	ExpectedVersion int
	Purpose         string
	Decision        string
	Letters         int
}

// RequestOwner asks the Owner on a card (request_owner): for the current
// brief (and design), not while the issue has live work, not while a card
// of the same purpose (and point) is open. The issue waits on the Owner
// (owner_deciding) until its cards are settled.
func RequestOwner(ctx context.Context, db DB, w Writer, key string, req OwnerCardRequest, ask Ask) (map[string]any, error) {
	if !slices.Contains(RequestablePurposes, req.Purpose) {
		return nil, invalid("a card's purpose is one of %v, not %q", RequestablePurposes, req.Purpose)
	}
	design := req.Purpose == OwnerCardDesignDecision
	switch {
	case design && (!decisionRE.MatchString(req.Decision) || req.Letters < 2 || req.Letters > 4):
		return nil, invalid("a design's point is D<n> and its card offers 2 to 4 letters")
	case !design && (req.Decision != "" || req.Letters != 0):
		return nil, invalid("only a design's point names a point and its letters")
	case ask == nil:
		return nil, fmt.Errorf("%w: the platform has no question store", ErrAsk)
	}
	r0 := map[string]any{"issue_id": req.IssueID, "expected_version": req.ExpectedVersion, "purpose": req.Purpose,
		"decision": req.Decision, "letters": req.Letters}
	return issueOp(ctx, db, w, "issue-owner-card", key, r0, func(tx pgx.Tx, _ string) (map[string]any, error) {
		r, err := lockIssue(ctx, tx, req.IssueID, req.ExpectedVersion)
		if err != nil {
			return nil, err
		}
		if err := r.open(); err != nil {
			return nil, err
		}
		if r.briefSHA == "" {
			return nil, fmt.Errorf("%w: %s has no brief: a card is bound to one", ErrState, r.ID)
		}
		if m, err := liveWork(ctx, tx, r.ID); err != nil || m != "" {
			return nil, orState(err, "%s has live work (%s): the Owner's cards on it come before it is taken", r.ID, m)
		}
		body := map[string]any{"change": "ask_owner", "from": r.State, "to": "owner_deciding", "purpose": req.Purpose,
			"brief_sha256": r.briefSHA}
		dsum := ""
		switch req.Purpose {
		case OwnerCardConfirmBrief:
			if r.AsksPerson != OwnerPerson {
				return nil, invalid("%s asks %s: she confirms its brief through her agent; the Owner's card confirms only what asks him", r.ID, r.AsksPerson)
			}
			var a int64
			err := tx.QueryRow(ctx, `SELECT issue_ask_id FROM agentrt.issue_ask WHERE issue_id = $1 AND purpose = 'confirm_brief'
			  AND verdict IS NULL AND superseded_at IS NULL AND bound_sha256 = $2`, r.ID, r.briefSHA).Scan(&a)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, fmt.Errorf("%w: %s asks no confirmation of its current brief", ErrState, r.ID)
			}
			if err != nil {
				return nil, err
			}
			body["basis_ask"] = a
		case OwnerCardDesignDecision:
			points, err := designPoints(ctx, tx, r)
			if err != nil {
				return nil, err
			}
			i := slices.IndexFunc(points, func(p DesignDecision) bool { return p.Key == req.Decision })
			if i < 0 {
				return nil, invalid("the design of %s has no point %s", r.ID, req.Decision)
			}
			for _, l := range points[i].Proceed {
				if int(l[0]-'A') >= req.Letters {
					return nil, invalid("point %s goes on under %s, which a card of %d letters does not offer", req.Decision, l, req.Letters)
				}
			}
			dsum, body["decision"] = r.designSHA, req.Decision
		case OwnerCardReviewDispute:
			if r.designSHA == "" {
				return nil, fmt.Errorf("%w: %s has no design to dispute the review of", ErrState, r.ID)
			}
			var result string
			var round int
			err := tx.QueryRow(ctx, `SELECT result, COALESCE(review_round, 0) FROM agentrt.issue_check WHERE issue_id = $1 AND check_kind = 'review'
			  AND brief_sha256 = $2 AND design_sha256 = $3 ORDER BY issue_check_id DESC LIMIT 1`, r.ID, r.briefSHA, r.designSHA).Scan(&result, &round)
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && (result != "fix" || round < MaxReviewRounds)) {
				return nil, fmt.Errorf("%w: a dispute comes after %d review rounds of the current design that did not pass", ErrState, MaxReviewRounds)
			}
			if err != nil {
				return nil, err
			}
			dsum = r.designSHA
		}
		if dsum != "" {
			body["design_sha256"] = dsum
		}
		open, err := issueCardRecords(ctx, tx, r.ID)
		if err != nil {
			return nil, err
		}
		for _, c := range open {
			same := c.purpose == req.Purpose || (req.Purpose == OwnerCardPriority && c.purpose == OwnerCardPriorityLevel)
			if !c.settled && same && c.decision == req.Decision {
				return nil, fmt.Errorf("%w: the Owner's card %s on %s's %s is open", ErrState, c.question, r.ID, strings.ReplaceAll(c.purpose, "_", " "))
			}
		}
		card := ownerCard(r.ID, req.Purpose, req.Decision, r.briefSHA, dsum, req.Letters, "")
		card.Key = canonKey(w.Actor, "issue-card:"+key)
		q, err := askCard(ctx, ask, card)
		if err != nil {
			return nil, err
		}
		body["question_id"] = q
		if _, err := appendIssue(ctx, tx, "issue_state_changed", w.Actor, r.ID, "", body); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE agentrt.issue SET state = 'owner_deciding', waiting_on = 'owner', version = version + 1
		  WHERE issue_id = $1`, r.ID); err != nil {
			return nil, err
		}
		return written(ctx, tx, r.ID, map[string]any{"question_id": q, "purpose": req.Purpose})
	})
}

// askCard asks a card of the template, refusing one the question store
// would not take for its size before it is asked.
func askCard(ctx context.Context, ask Ask, card Card) (string, error) {
	if n := questions.BodyBytes(card.Body); n > questions.BodyLimit {
		return "", invalid("the card would be %d bytes, more than the question store's %d", n, questions.BodyLimit)
	}
	q, err := ask(ctx, card)
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			return "", err
		}
		return "", fmt.Errorf("%w: %v", ErrAsk, err)
	}
	return q, nil
}

// askIssueMission puts a BI mission outside the standing envelope on the
// Owner's card, by the template (§4.4, §5.3; slice 1 asked none, review 522
// A1): take_issue asks it, for the issue it holds locked, once the take
// predicate held. The card has the issue's number, the mission's, the
// versions it was taken for and the two options of a mission's card, which
// missiondecide.go reads as any mission's. The mission's own record holds
// its title, goal and criteria, the crew's engineering material, never her
// words (§4.4).
func askIssueMission(ctx context.Context, tx pgx.Tx, w Writer, key, id string, r issueRow, why string, ask Ask) (string, error) {
	card := ownerCard(r.ID, ownerCardMission, "", r.briefSHA, r.designSHA, 0, id)
	card.Key = canonKey(w.Actor, "mission:"+key)
	q, err := askCard(ctx, ask, card)
	if err != nil {
		return "", err
	}
	return q, moveMission(ctx, tx, "controller", id, "awaiting_owner", why, q, "")
}

// issueCardRec is an Owner's card on an issue as the issue's events record
// it: opened (by request_owner, or by the runtime for a priority's level),
// and settled with an outcome, or not yet.
type issueCardRec struct {
	issue, question, purpose, decision, brief, design string
	ask                                               int64 // a brief's confirmation: the ask it answers
	opened                                            time.Time
	settled                                           bool
	outcome                                           string
}

// issueCardRecords are an issue's cards ("" for every issue's cards opened
// in the last ten days, a card's longest life with room to spare), in the
// order they were opened, read from its events.
func issueCardRecords(ctx context.Context, tx pgx.Tx, issue string) ([]*issueCardRec, error) {
	rows, err := tx.Query(ctx, `SELECT e.event_stream_id, e.event_type, e.at, e.body_json FROM agentrt.event e
	  WHERE (CASE WHEN $1 = '' THEN e.event_stream_id LIKE 'issue:%' ELSE e.event_stream_id = 'issue:' || $1 END)
	    AND ((e.event_type = 'issue_state_changed' AND e.body_json->>'change' = 'ask_owner')
	      OR (e.event_type = 'note' AND e.body_json ? 'card')
	      OR (e.event_type IN ('issue_priority_set','issue_owner_decided','issue_answered') AND e.body_json ? 'question_id'))
	    AND ($1 <> '' OR e.at > clock_timestamp() - interval '10 days')
	  ORDER BY e.event_id`, issue)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*issueCardRec
	byQ := map[string]*issueCardRec{}
	for rows.Next() {
		var stream, typ string
		var at time.Time
		var body map[string]any
		if err := rows.Scan(&stream, &typ, &at, &body); err != nil {
			return nil, err
		}
		str := func(m map[string]any, k string) string { s, _ := m[k].(string); return s }
		src, opened := body, typ == "issue_state_changed"
		if typ == "note" {
			src, _ = body["card"].(map[string]any)
			opened = str(src, "state") == "opened"
		}
		q := str(src, "question_id")
		if q == "" {
			continue
		}
		if opened {
			c := &issueCardRec{issue: trimIssuePrefix(stream), question: q, purpose: str(src, "purpose"), decision: str(src, "decision"),
				brief: str(src, "brief_sha256"), design: str(src, "design_sha256"), opened: at}
			switch n := src["basis_ask"].(type) {
			case float64:
				c.ask = int64(n)
			case json.Number:
				c.ask, _ = n.Int64()
			}
			list, byQ[q] = append(list, c), c
			continue
		}
		if c, ok := byQ[q]; ok && !c.settled {
			c.settled, c.outcome = true, str(src, "outcome")
			if typ != "note" {
				c.outcome = "recorded"
			}
		}
	}
	return list, rows.Err()
}

// issueCards are an issue's cards as its full view shows them, a BI
// mission's card among them while its mission awaits the Owner.
func issueCards(ctx context.Context, tx pgx.Tx, issue string) ([]any, error) {
	recs, err := issueCardRecords(ctx, tx, issue)
	if err != nil {
		return nil, err
	}
	out := []any{}
	for _, c := range recs {
		m := map[string]any{"question_id": c.question, "purpose": c.purpose, "brief_sha256": c.brief, "open": !c.settled, "opened_at": c.opened}
		if c.decision != "" {
			m["decision"] = c.decision
		}
		if c.design != "" {
			m["design_sha256"] = c.design
		}
		if c.settled {
			m["outcome"] = c.outcome
		}
		out = append(out, m)
	}
	rows, err := tx.Query(ctx, `SELECT s.body_json->>'question_id', w.mission_id, m.state FROM agentrt.issue_work w
	  JOIN agentrt.mission m ON m.mission_id = w.mission_id
	  JOIN agentrt.event s ON s.mission_id = w.mission_id AND s.event_type = 'mission_state_changed' AND s.body_json->>'to' = 'awaiting_owner'
	  WHERE w.issue_id = $1 AND s.body_json ? 'question_id' ORDER BY s.event_id`, issue)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var q, mission, state string
		if err := rows.Scan(&q, &mission, &state); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"question_id": q, "purpose": ownerCardMission, "mission_id": mission, "open": state == "awaiting_owner"})
	}
	return out, rows.Err()
}
