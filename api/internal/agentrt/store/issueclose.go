package store

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// Closing, parking, reopening and merging issues (DESIGN-crew-intake §2.2,
// §4.3 close predicate, §7.3). An issue is resolved only on the answer of
// the person served to this very delivery, for the current brief, with the
// seat's newest check of that answer supporting it; neither an agent's
// judgement nor silence closes one.

// IssueClose is close_issue: the conclusion, the ask it rests on and, for a
// decline, why.
type IssueClose struct {
	IssueID         string
	ExpectedVersion int
	Conclusion      string
	AskID           int64
	Reason          string
}

// askRow is an ask as the close predicate reads it.
type askRow struct {
	id                               int64
	purpose, verdict, brief, mission string
	commitment                       int
	superseded                       bool
}

// latestAsk is the issue's newest ask (the largest id), ok false when it has
// none.
func latestAsk(ctx context.Context, tx pgx.Tx, issue string) (askRow, bool, error) {
	var a askRow
	err := tx.QueryRow(ctx, `SELECT issue_ask_id, purpose, COALESCE(verdict, ''), COALESCE(brief_sha256, ''), COALESCE(mission_id, ''),
	  COALESCE(commitment, 0), superseded_at IS NOT NULL FROM agentrt.issue_ask WHERE issue_id = $1 ORDER BY issue_ask_id DESC LIMIT 1`, issue).
		Scan(&a.id, &a.purpose, &a.verdict, &a.brief, &a.mission, &a.commitment, &a.superseded)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, false, nil
	}
	return a, err == nil, err
}

// CloseIssue closes an issue by the close predicate (§4.3):
//
//   - resolved, partial: the ask named is the issue's newest ask, not
//     superseded, a trial (ready_to_try) asked for the current brief, of work
//     taken for that same brief; answered confirmed (resolved) or partly
//     (partial) by the person asked; and the seat's newest check of that
//     answer, for the current brief, supports it (R2-A4).
//   - already_possible: the ask named is the newest, not superseded, an
//     answer to check (check_answer) for the current brief, answered
//     confirmed; no live work; and, as the standing exception for an issue
//     only answered still requires (items 1, 2, 8), the brief confirmed, the
//     classification of the current brief, the intake-gates rule in force;
//     the Owner's priority too unless that rule opens answer_only.
//   - declined: for a person, only the Owner, or her own withdrawal as the
//     newest answer before any work; an agent's own proposal, the crew with a
//     reason.
//
// merged is MergeIssues'. Open asks are superseded with the close.
func CloseIssue(ctx context.Context, db DB, w Writer, key string, c IssueClose) (map[string]any, error) {
	switch c.Conclusion {
	case ConclusionResolved, ConclusionPartial, ConclusionAlreadyPossible, ConclusionDeclined:
	case ConclusionMerged:
		return nil, invalid("an issue is merged by merge_issues")
	default:
		return nil, invalid("a conclusion is one of %v, not %q", conclusions, c.Conclusion)
	}
	if c.Reason != "" {
		if err := checkText("the reason", c.Reason, 0); err != nil {
			return nil, err
		}
	}
	req := map[string]any{"issue_id": c.IssueID, "expected_version": c.ExpectedVersion, "conclusion": c.Conclusion, "ask_id": c.AskID,
		"reason": c.Reason}
	return issueOp(ctx, db, w, "issue-close", key, req, func(tx pgx.Tx, channel string) (map[string]any, error) {
		r, err := lockIssue(ctx, tx, c.IssueID, c.ExpectedVersion)
		if err != nil {
			return nil, err
		}
		if r.State == "closed" {
			return nil, fmt.Errorf("%w: %s is closed already (%s)", ErrState, r.ID, r.Conclusion)
		}
		a, has, err := latestAsk(ctx, tx, r.ID)
		if err != nil {
			return nil, err
		}
		refuse := func(format string, args ...any) error {
			return fmt.Errorf("%w: %s is not closed %s: %s", ErrState, r.ID, c.Conclusion, fmt.Sprintf(format, args...))
		}
		basis := c.AskID != 0
		switch c.Conclusion {
		case ConclusionResolved, ConclusionPartial:
			want := map[string]string{ConclusionResolved: "confirmed", ConclusionPartial: "partly"}[c.Conclusion]
			switch {
			case !has || a.id != c.AskID || a.superseded:
				return nil, refuse("ask %d is not its newest open-standing ask", c.AskID)
			case a.purpose != AskReadyToTry:
				return nil, refuse("ask %d is a %s, not a trial of a delivery", c.AskID, a.purpose)
			case r.briefSHA == "" || a.brief != r.briefSHA:
				return nil, refuse("ask %d was asked for an earlier brief", c.AskID)
			case a.verdict != want:
				return nil, refuse("the answer to ask %d is %q, not %s", c.AskID, a.verdict, want)
			}
			var workBrief string
			err := tx.QueryRow(ctx, `SELECT brief_sha256 FROM agentrt.issue_work WHERE issue_id = $1 AND commitment = $2 AND mission_id = $3`,
				r.ID, a.commitment, a.mission).Scan(&workBrief)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return nil, err
			}
			if workBrief != r.briefSHA {
				return nil, refuse("the work ask %d delivers was not taken for the current brief", c.AskID)
			}
			var result, checkBrief string
			err = tx.QueryRow(ctx, `SELECT result, brief_sha256 FROM agentrt.issue_check WHERE issue_id = $1 AND check_kind = 'close_check'
			  AND ask_id = $2 ORDER BY issue_check_id DESC LIMIT 1`, r.ID, c.AskID).Scan(&result, &checkBrief)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, refuse("no seat has checked the answer to ask %d", c.AskID)
			}
			if err != nil {
				return nil, err
			}
			if result != "supports" || checkBrief != r.briefSHA {
				return nil, refuse("the seat's newest check of the answer to ask %d is %s (of the current brief: %t)", c.AskID, result,
					checkBrief == r.briefSHA)
			}
		case ConclusionAlreadyPossible:
			switch {
			case !has || a.id != c.AskID || a.superseded:
				return nil, refuse("ask %d is not its newest open-standing ask", c.AskID)
			case a.purpose != AskCheckAnswer:
				return nil, refuse("ask %d is a %s, not an answer to check", c.AskID, a.purpose)
			case r.briefSHA == "" || a.brief != r.briefSHA:
				return nil, refuse("ask %d was asked for an earlier brief", c.AskID)
			case a.verdict != "confirmed":
				return nil, refuse("the answer to ask %d is %q, not confirmed", c.AskID, a.verdict)
			}
			if m, err := liveWork(ctx, tx, r.ID); err != nil || m != "" {
				return nil, orState(err, "%s has live work (%s)", r.ID, m)
			}
			if err := answerOnlyHolds(ctx, tx, r); err != nil {
				return nil, err
			}
		case ConclusionDeclined:
			switch {
			case r.ServedPerson == "":
				if c.Reason == "" {
					return nil, refuse("an agent's own proposal is declined with a reason")
				}
			case w.Actor == "owner":
			case basis && has && a.id == c.AskID && !a.superseded && a.verdict == "withdrawn":
			default:
				return nil, refuse("a person's issue is declined by the Owner, or on her own withdrawal")
			}
			if basis && (!has || a.id != c.AskID) {
				return nil, refuse("ask %d is not its newest ask", c.AskID)
			}
		}
		if _, err := supersede(ctx, tx, w.Actor, r.ID, `verdict IS NULL`); err != nil {
			return nil, err
		}
		body := map[string]any{"change": "close", "from": r.State, "to": "closed", "conclusion": c.Conclusion}
		if basis {
			body["basis_ask"] = c.AskID
		}
		var note int64
		if c.Reason != "" {
			ids, err := nextIDs(ctx, tx, "issue_text", 1)
			if err != nil {
				return nil, err
			}
			note, body["text_id"] = ids[0], ids[0]
		}
		if _, err := appendIssue(ctx, tx, "issue_state_changed", w.Actor, r.ID, "", body); err != nil {
			return nil, err
		}
		if note != 0 {
			if _, err := insertText(ctx, tx, r.ID, w.Actor, issueText{id: note, kind: "note", channel: channel, body: c.Reason}); err != nil {
				return nil, err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE agentrt.issue SET state = 'closed', conclusion = $2, waiting_on = 'crew', version = version + 1
		  WHERE issue_id = $1`, r.ID, c.Conclusion); err != nil {
			return nil, err
		}
		return written(ctx, tx, r.ID, nil)
	})
}

// answerOnlyHolds is what an issue only answered still needs to close
// already_possible (§4.3: the standing exception skips item 3 only): the
// brief confirmed, classified, the rule in force; and the Owner's priority
// when the rule does not open answer_only.
func answerOnlyHolds(ctx context.Context, tx pgx.Tx, r issueRow) error {
	rule, err := inForce(ctx, tx, rules.KindIntake)
	if err != nil {
		return err
	}
	if rule == nil || rule.Intake == nil {
		return fmt.Errorf("%w: no intake-gates rule is in force and reads: %s is not closed already_possible", ErrState, r.ID)
	}
	ev, err := issueEvidence(ctx, tx, r)
	if err != nil {
		return err
	}
	var missing []string
	if !ev.briefConfirmed {
		missing = append(missing, "the brief is not confirmed")
	}
	if !ev.classified {
		missing = append(missing, "the current brief is not classified")
	}
	if !rule.Intake.Allows(rules.IntakeAnswerOnly) && !ev.prioritized {
		missing = append(missing, "the rule does not open answer_only and the Owner set no priority")
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: %s is not closed already_possible: %v", ErrState, r.ID, missing)
	}
	return nil
}

// IssueNote is park_issue and reopen_issue: why (for a park, and when it
// resumes).
type IssueNote struct {
	IssueID         string
	ExpectedVersion int
	Reason          string
}

// ParkIssue parks an issue (§4.3 parked): why and when it resumes are its
// note; open asks are superseded (the crew asks again on resuming). Refused
// while its work is live (R2-A3): parking never frees a product's slot.
func ParkIssue(ctx context.Context, db DB, w Writer, key string, n IssueNote) (map[string]any, error) {
	if err := checkText("why it is parked and when it resumes", n.Reason, 0); err != nil {
		return nil, err
	}
	req := map[string]any{"issue_id": n.IssueID, "expected_version": n.ExpectedVersion, "reason": n.Reason}
	return issueOp(ctx, db, w, "issue-park", key, req, func(tx pgx.Tx, channel string) (map[string]any, error) {
		r, err := lockIssue(ctx, tx, n.IssueID, n.ExpectedVersion)
		if err != nil {
			return nil, err
		}
		if err := r.open(); err != nil {
			return nil, err
		}
		if m, err := liveWork(ctx, tx, r.ID); err != nil || m != "" {
			return nil, orState(err, "%s has live work (%s): its work ends first, by its mission", r.ID, m)
		}
		return changeState(ctx, tx, w.Actor, channel, r, "park", "parked", n.Reason, `verdict IS NULL`)
	})
}

// ResumeIssue takes a parked issue back to where it was parked from; an
// issue parked from active or awaiting_user (its work had ended) resumes as
// classified, since only take_issue makes an issue active.
func ResumeIssue(ctx context.Context, db DB, w Writer, key string, n IssueNote) (map[string]any, error) {
	req := map[string]any{"issue_id": n.IssueID, "expected_version": n.ExpectedVersion, "reason": n.Reason}
	return issueOp(ctx, db, w, "issue-resume", key, req, func(tx pgx.Tx, channel string) (map[string]any, error) {
		r, err := lockIssue(ctx, tx, n.IssueID, n.ExpectedVersion)
		if err != nil {
			return nil, err
		}
		if r.State != "parked" {
			return nil, fmt.Errorf("%w: %s is not parked", ErrState, r.ID)
		}
		var from string
		if err := tx.QueryRow(ctx, `SELECT COALESCE(body_json->>'from', 'clarifying') FROM agentrt.event WHERE event_stream_id = $1
		  AND event_type = 'issue_state_changed' AND body_json->>'change' = 'park' ORDER BY event_id DESC LIMIT 1`, IssueStream(r.ID)).Scan(&from); err != nil {
			return nil, err
		}
		if from == "active" || from == "awaiting_user" {
			from = "classified"
		}
		return changeState(ctx, tx, w.Actor, channel, r, "resume", from, n.Reason, "")
	})
}

// ReopenIssue reopens a closed issue (not a merged one): every open ask and
// every trial and answer to check asked before are superseded, so nothing
// answered before the reopening closes it again (R2-A4); it returns to
// classified (its class and priority stand, still bound to its brief), or
// to clarifying when it was never classified.
func ReopenIssue(ctx context.Context, db DB, w Writer, key string, n IssueNote) (map[string]any, error) {
	req := map[string]any{"issue_id": n.IssueID, "expected_version": n.ExpectedVersion, "reason": n.Reason}
	return issueOp(ctx, db, w, "issue-reopen", key, req, func(tx pgx.Tx, channel string) (map[string]any, error) {
		r, err := lockIssue(ctx, tx, n.IssueID, n.ExpectedVersion)
		if err != nil {
			return nil, err
		}
		switch {
		case r.State != "closed":
			return nil, fmt.Errorf("%w: %s is not closed", ErrState, r.ID)
		case r.Conclusion == ConclusionMerged:
			return nil, fmt.Errorf("%w: %s was merged into %s and stays so", ErrState, r.ID, r.DuplicateOf)
		}
		to := "classified"
		if r.RiskClass == "" {
			to = "clarifying"
		}
		return changeState(ctx, tx, w.Actor, channel, r, "reopen", to, n.Reason, `verdict IS NULL OR purpose IN ('ready_to_try','check_answer')`)
	})
}

// changeState supersedes the asks cond names ("" none), records the change
// with its note and moves the issue to state (reopening clears the
// conclusion).
func changeState(ctx context.Context, tx pgx.Tx, actor, channel string, r issueRow, change, to, reason, cond string) (map[string]any, error) {
	if cond != "" {
		if _, err := supersede(ctx, tx, actor, r.ID, cond); err != nil {
			return nil, err
		}
	}
	body := map[string]any{"change": change, "from": r.State, "to": to}
	var note int64
	if reason != "" {
		if err := checkText("the note", reason, 0); err != nil {
			return nil, err
		}
		ids, err := nextIDs(ctx, tx, "issue_text", 1)
		if err != nil {
			return nil, err
		}
		note, body["text_id"] = ids[0], ids[0]
	}
	if _, err := appendIssue(ctx, tx, "issue_state_changed", actor, r.ID, "", body); err != nil {
		return nil, err
	}
	if note != 0 {
		if _, err := insertText(ctx, tx, r.ID, actor, issueText{id: note, kind: "note", channel: channel, body: reason}); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE agentrt.issue SET state = $2, conclusion = NULL, waiting_on = 'crew', version = version + 1
	  WHERE issue_id = $1`, r.ID, to); err != nil {
		return nil, err
	}
	return written(ctx, tx, r.ID, nil)
}

// IssueMerge is merge_issues: From is merged into Into, each at the version
// read.
type IssueMerge struct {
	From, Into               string
	FromVersion, IntoVersion int
}

// MergeIssues merges a duplicate into another issue (§7.3): only one
// person's issues of one product (an agent's own proposals only the same
// agent's), first batch only issues no work was ever taken for, with no
// trial or answer asked (B2); never across people (A7). Every word stays
// with the issue it came with; the duplicate closes merged; the class of
// the one kept only rises. The rows are locked in ascending number.
func MergeIssues(ctx context.Context, db DB, w Writer, key string, m IssueMerge) (map[string]any, error) {
	if !issueIDRE.MatchString(m.From) || !issueIDRE.MatchString(m.Into) || m.From == m.Into {
		return nil, invalid("a merge names two issues")
	}
	req := map[string]any{"from": m.From, "into": m.Into, "from_version": m.FromVersion, "into_version": m.IntoVersion}
	return issueOp(ctx, db, w, "issue-merge", key, req, func(tx pgx.Tx, channel string) (map[string]any, error) {
		order := []struct {
			id      string
			version int
		}{{m.From, m.FromVersion}, {m.Into, m.IntoVersion}}
		if issueNumber(m.Into) < issueNumber(m.From) {
			order[0], order[1] = order[1], order[0]
		}
		rows := map[string]issueRow{}
		for _, o := range order {
			r, err := lockIssue(ctx, tx, o.id, o.version)
			if err != nil {
				return nil, err
			}
			rows[o.id] = r
		}
		from, into := rows[m.From], rows[m.Into]
		switch {
		case from.State == "closed" || into.State == "closed":
			return nil, fmt.Errorf("%w: a closed issue is neither merged nor merged into", ErrState)
		case from.Product != into.Product || from.ServedPerson != into.ServedPerson ||
			(from.ServedPerson == "" && from.creator != into.creator):
			return nil, fmt.Errorf("%w: %s and %s are not one person's issues of one product: they are not merged", ErrState, m.From, m.Into)
		}
		for _, id := range []string{m.From, m.Into} {
			var taken bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.issue_work WHERE issue_id = $1)`, id).Scan(&taken); err != nil {
				return nil, err
			}
			if taken {
				return nil, fmt.Errorf("%w: work was taken for %s: first batch merges only issues never taken", ErrState, id)
			}
			if busy, err := openAsk(ctx, tx, id, AskReadyToTry, AskCheckAnswer); err != nil || busy {
				return nil, orState(err, "%s has a trial or an answer asked", id)
			}
		}
		if _, err := supersede(ctx, tx, w.Actor, m.From, `verdict IS NULL`); err != nil {
			return nil, err
		}
		if _, err := appendIssue(ctx, tx, "issue_merged", w.Actor, m.From, "", map[string]any{"into": m.Into}); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE agentrt.issue SET duplicate_of = $2, state = 'closed', conclusion = 'merged', waiting_on = 'crew',
		  version = version + 1 WHERE issue_id = $1`, m.From, m.Into); err != nil {
			return nil, err
		}
		risk := into.RiskClass
		body := map[string]any{"merged_from": m.From}
		if slices.Index(riskClasses, from.RiskClass) > slices.Index(riskClasses, into.RiskClass) {
			risk = from.RiskClass
			body["risk_class"] = risk
		}
		if _, err := appendIssue(ctx, tx, "issue_merged", w.Actor, m.Into, "", body); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE agentrt.issue SET risk_class = $2, version = version + 1 WHERE issue_id = $1`,
			m.Into, nullable(risk)); err != nil {
			return nil, err
		}
		return written(ctx, tx, m.Into, map[string]any{"merged": m.From})
	})
}

// issueNumber is n of BI-n.
func issueNumber(id string) int {
	var n int
	_, _ = fmt.Sscanf(id, "BI-%d", &n)
	return n
}
