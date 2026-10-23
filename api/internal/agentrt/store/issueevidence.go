package store

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
)

// The evidence the gates read (DESIGN-crew-intake §4.2 issue_check, §4.3,
// §7.2): a classification by the seat, the design and its independent
// review, the Owner's priority and decisions, the seat's check of an answer.
// Each is bound to the brief (and design) current when it is recorded, and
// only the newest of a kind counts: a new version of the brief or design
// leaves the old evidence standing in the history and counting for nothing.

// IssueClassification is classify_issue: the class of risk and the category
// the seat judged (its answer, the report of a brief task, SeatReport) and
// why.
type IssueClassification struct {
	IssueID         string
	ExpectedVersion int
	RiskClass       string
	Category        string
	SeatReport      string
	Reason          string
}

// ClassifyIssue records a classification of the current, confirmed brief.
// A class only rises (0009 guards it too): reclassifying, merging and
// reopening never lower it.
func ClassifyIssue(ctx context.Context, db DB, w Writer, key string, c IssueClassification) (map[string]any, error) {
	switch {
	case !slices.Contains(riskClasses, c.RiskClass):
		return nil, invalid("a risk class is one of %v, not %q", riskClasses, c.RiskClass)
	case !slices.Contains(issueCategories, c.Category):
		return nil, invalid("a category is one of %v, not %q", issueCategories, c.Category)
	case !artifactIDRE.MatchString(c.SeatReport):
		return nil, invalid("a classification names the seat's answer, the report of a brief task (sha256:<64 hex>)")
	}
	if c.Reason != "" {
		if err := checkText("the reason", c.Reason, 0); err != nil {
			return nil, err
		}
	}
	req := map[string]any{"issue_id": c.IssueID, "expected_version": c.ExpectedVersion, "risk_class": c.RiskClass, "category": c.Category,
		"seat_report": c.SeatReport, "reason": c.Reason}
	return issueOp(ctx, db, w, "issue-classify", key, req, func(tx pgx.Tx, channel string) (map[string]any, error) {
		r, err := lockIssue(ctx, tx, c.IssueID, c.ExpectedVersion)
		if err != nil {
			return nil, err
		}
		if err := r.open(); err != nil {
			return nil, err
		}
		if !r.BriefConfirmed {
			return nil, fmt.Errorf("%w: %s has no confirmed brief: nothing is classified before the person confirms what is asked", ErrState, r.ID)
		}
		if slices.Index(riskClasses, c.RiskClass) < slices.Index(riskClasses, r.RiskClass) {
			return nil, fmt.Errorf("%w: %s is %s; a class only rises", ErrState, r.ID, r.RiskClass)
		}
		checks, err := nextIDs(ctx, tx, "issue_check", 1)
		if err != nil {
			return nil, err
		}
		body := map[string]any{"check_id": checks[0], "risk_class": c.RiskClass, "category": c.Category, "brief_sha256": r.briefSHA,
			"seat_report": c.SeatReport}
		var reason any
		if c.Reason != "" {
			ids, err := nextIDs(ctx, tx, "issue_text", 1)
			if err != nil {
				return nil, err
			}
			body["text_id"], reason = ids[0], ids[0]
		}
		if _, err := appendIssue(ctx, tx, "issue_classified", w.Actor, r.ID, "", body); err != nil {
			return nil, err
		}
		if reason != nil {
			if _, err := insertText(ctx, tx, r.ID, w.Actor, issueText{id: reason.(int64), kind: "note", channel: channel, body: c.Reason}); err != nil {
				return nil, err
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO agentrt.issue_check (issue_check_id, issue_id, check_kind, brief_sha256, result, category,
		    report_text_id, seat_artifact_id, created_by_actor_id)
		  OVERRIDING SYSTEM VALUE VALUES ($1, $2, 'classification', $3, $4, $5, $6, $7, $8)`,
			checks[0], r.ID, r.briefSHA, c.RiskClass, c.Category, reason, c.SeatReport, w.Actor); err != nil {
			return nil, err
		}
		state := r.State
		if state == "brief_confirmed" {
			state = "classified"
		}
		if _, err := tx.Exec(ctx, `UPDATE agentrt.issue SET risk_class = $2, state = $3, version = version + 1 WHERE issue_id = $1`,
			r.ID, c.RiskClass, state); err != nil {
			return nil, err
		}
		return written(ctx, tx, r.ID, map[string]any{"check_id": checks[0]})
	})
}

// IssueCloseCheck is the seat's check of an answer (§4.3 close predicate ④):
// whether the person's answer to an ask supports the conclusion the crew
// would close with, the seat's report (a brief task's) and an excerpt.
type IssueCloseCheck struct {
	IssueID         string
	ExpectedVersion int
	AskID           int64
	Result          string // supports or does_not
	SeatReport      string
	Excerpt         string
}

// RecordCloseCheck records the seat's check of an answer given, for the
// current brief. Only the newest check of an ask counts.
func RecordCloseCheck(ctx context.Context, db DB, w Writer, key string, c IssueCloseCheck) (map[string]any, error) {
	switch {
	case c.Result != "supports" && c.Result != "does_not":
		return nil, invalid("a seat's check supports or does_not, not %q", c.Result)
	case !artifactIDRE.MatchString(c.SeatReport):
		return nil, invalid("a seat's check names its report (sha256:<64 hex>)")
	}
	if c.Excerpt != "" {
		if err := checkText("the excerpt", c.Excerpt, 0); err != nil {
			return nil, err
		}
	}
	req := map[string]any{"issue_id": c.IssueID, "expected_version": c.ExpectedVersion, "ask_id": c.AskID, "result": c.Result,
		"seat_report": c.SeatReport, "excerpt": c.Excerpt}
	return issueOp(ctx, db, w, "issue-close-check", key, req, func(tx pgx.Tx, channel string) (map[string]any, error) {
		r, err := lockIssue(ctx, tx, c.IssueID, c.ExpectedVersion)
		if err != nil {
			return nil, err
		}
		if r.briefSHA == "" {
			return nil, fmt.Errorf("%w: %s has no brief", ErrState, r.ID)
		}
		var answered bool
		err = tx.QueryRow(ctx, `SELECT verdict IS NOT NULL FROM agentrt.issue_ask WHERE issue_ask_id = $1 AND issue_id = $2`, c.AskID, r.ID).Scan(&answered)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, invalid("%s has no ask %d", r.ID, c.AskID)
		}
		if err != nil {
			return nil, err
		}
		if !answered {
			return nil, fmt.Errorf("%w: ask %d is not answered: a seat checks an answer given", ErrState, c.AskID)
		}
		checks, err := nextIDs(ctx, tx, "issue_check", 1)
		if err != nil {
			return nil, err
		}
		body := map[string]any{"check_id": checks[0], "check_kind": checkClose, "result": c.Result, "brief_sha256": r.briefSHA,
			"checked_ask": c.AskID, "seat_report": c.SeatReport}
		var excerpt any
		if c.Excerpt != "" {
			ids, err := nextIDs(ctx, tx, "issue_text", 1)
			if err != nil {
				return nil, err
			}
			body["text_id"], excerpt = ids[0], ids[0]
		}
		if _, err := appendIssue(ctx, tx, "issue_reviewed", w.Actor, r.ID, "", body); err != nil {
			return nil, err
		}
		if excerpt != nil {
			if _, err := insertText(ctx, tx, r.ID, w.Actor, issueText{id: excerpt.(int64), kind: "check", channel: channel, body: c.Excerpt}); err != nil {
				return nil, err
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO agentrt.issue_check (issue_check_id, issue_id, check_kind, brief_sha256, result, report_text_id,
		    seat_artifact_id, ask_id, created_by_actor_id)
		  OVERRIDING SYSTEM VALUE VALUES ($1, $2, 'close_check', $3, $4, $5, $6, $7, $8)`,
			checks[0], r.ID, r.briefSHA, c.Result, excerpt, c.SeatReport, c.AskID, w.Actor); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE agentrt.issue SET version = version + 1 WHERE issue_id = $1`, r.ID); err != nil {
			return nil, err
		}
		return written(ctx, tx, r.ID, map[string]any{"check_id": checks[0]})
	})
}

// OwnerEvidence is the Owner's own decision on an issue (§4.3 items 3, 5, 6;
// §5.3): a priority (p1, p2, p3, later, declined), or an option on a point
// of the design (D<n>: a letter) or on a review the rounds did not settle
// (review_dispute: proceed or stop). It names the brief (and, for a design's
// point, the design) he decided on, by sha256; a decision on another version
// is stale. Written by owner (his admin token), or by the runtime
// (controller) from his card: then the card, who answered and the channel,
// never a rule's (slice 2 asks the cards).
type OwnerEvidence struct {
	IssueID                   string
	Priority                  string // a priority, or "" for a decision
	Decision, Option          string // a decision's point and option
	BriefSHA256, DesignSHA256 string
	QuestionID, AnsweredBy    string
	Channel                   string
}

// RecordOwnerEvidence records the Owner's priority or decision.
func RecordOwnerEvidence(ctx context.Context, db DB, w Writer, key string, e OwnerEvidence) (map[string]any, error) {
	card := e.QuestionID != "" || e.AnsweredBy != "" || e.Channel != ""
	switch {
	case w.Actor == "owner" && !card:
	case w.Actor == "controller" && e.QuestionID != "" && e.AnsweredBy != "" && e.Channel != "" && e.Channel != "rule":
	default:
		return nil, invalid("the Owner decides: by his own token, or on his card answered by him, not by a rule")
	}
	priority := e.Priority != ""
	switch {
	case priority && (e.Decision != "" || e.Option != ""):
		return nil, invalid("one decision at a time: a priority or a point's option")
	case priority && !slices.Contains(priorities, e.Priority):
		return nil, invalid("a priority is one of %v, not %q", priorities, e.Priority)
	case !priority && e.Decision == "review_dispute" && e.Option != "proceed" && e.Option != "stop" && e.Option != AnotherRound:
		return nil, invalid("a review dispute is decided proceed, stop or another_round")
	case !priority && e.Decision != "review_dispute" && (!decisionRE.MatchString(e.Decision) || !letterRE.MatchString(e.Option)):
		return nil, invalid("a design's point is D<n>, its option a letter")
	case !hex64RE.MatchString(e.BriefSHA256) || (!priority && !hex64RE.MatchString(e.DesignSHA256)):
		return nil, invalid("a decision names the brief (and the design) it was made on, by sha256")
	}
	req := map[string]any{"issue_id": e.IssueID, "priority": e.Priority, "decision": e.Decision, "option": e.Option,
		"brief_sha256": e.BriefSHA256, "design_sha256": e.DesignSHA256, "question_id": e.QuestionID, "answered_by": e.AnsweredBy,
		"channel": e.Channel}
	if key == "" {
		return nil, invalid("an idempotency key is required")
	}
	return idempotentAs(ctx, db, w, "issue-owner", key, req, nil, func(tx pgx.Tx) (map[string]any, error) {
		if err := ensureWriter(ctx, tx, w, false); err != nil {
			return nil, err
		}
		if !issueIDRE.MatchString(e.IssueID) {
			return nil, invalid("%q is not an issue id (BI-<n>)", e.IssueID)
		}
		r, err := readIssue(ctx, tx, e.IssueID, true)
		if err != nil {
			return nil, err
		}
		if err := r.open(); err != nil {
			return nil, err
		}
		if e.BriefSHA256 != r.briefSHA || (!priority && e.DesignSHA256 != r.designSHA) {
			return nil, &StaleError{Why: fmt.Sprintf("the decision was made on another version of %s's brief or design", r.ID), Issue: r.IssueView}
		}
		if !priority && e.Decision != "review_dispute" {
			points, err := designPoints(ctx, tx, r)
			if err != nil {
				return nil, err
			}
			if !slices.ContainsFunc(points, func(p DesignDecision) bool { return p.Key == e.Decision }) {
				return nil, invalid("the design of %s has no point %s", r.ID, e.Decision)
			}
		}
		checks, err := nextIDs(ctx, tx, "issue_check", 1)
		if err != nil {
			return nil, err
		}
		body := map[string]any{"check_id": checks[0], "brief_sha256": r.briefSHA}
		if card {
			body["question_id"], body["answered_by"], body["channel"] = e.QuestionID, e.AnsweredBy, e.Channel
		}
		typ, kind, result, category, design := "issue_priority_set", checkPriority, e.Priority, any(nil), any(nil)
		if priority {
			body["priority"] = e.Priority
		} else {
			typ, kind, result, category, design = "issue_owner_decided", checkOwnerDecision, e.Option, e.Decision, r.designSHA
			body["decision"], body["option"], body["design_sha256"] = e.Decision, e.Option, r.designSHA
		}
		if _, err := appendIssue(ctx, tx, typ, w.Actor, r.ID, "", body); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO agentrt.issue_check (issue_check_id, issue_id, check_kind, brief_sha256, design_sha256, result,
		    category, question_id, answered_by, answer_channel, created_by_actor_id)
		  OVERRIDING SYSTEM VALUE VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
			checks[0], r.ID, kind, r.briefSHA, design, result, category, nullable(e.QuestionID), nullable(e.AnsweredBy), nullable(e.Channel),
			w.Actor); err != nil {
			return nil, err
		}
		if priority && e.Priority != "declined" {
			_, err = tx.Exec(ctx, `UPDATE agentrt.issue SET priority = $2, version = version + 1 WHERE issue_id = $1`, r.ID, e.Priority)
		} else {
			_, err = tx.Exec(ctx, `UPDATE agentrt.issue SET version = version + 1 WHERE issue_id = $1`, r.ID)
		}
		if err != nil {
			return nil, err
		}
		return written(ctx, tx, r.ID, map[string]any{"check_id": checks[0]})
	})
}
