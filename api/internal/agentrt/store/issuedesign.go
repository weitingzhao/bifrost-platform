package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// The design and its independent review (DESIGN-crew-intake §4.3 items 4–6,
// §7.1, §7.2).
//
// What D covers (review 522 A3): D is the sha256 of the canonical design
// document, which is the design's decision table followed by its text, as
// designDoc renders it and as issue_text stores it whole. The table lists
// each point the Owner decides (D1, D2, …) with the options under which the
// work goes on, so the same text with another table is another D, and a
// review or an Owner's decision bound to the old D no longer counts. The
// take predicate reads the points from that stored document, never from
// anything beside it.

// DesignDecision is one point of a design the Owner decides (§4.3 item 6):
// its key (D<n>) and the options (letters) under which the design says the
// work may go on. What each letter means is in the design's text.
type DesignDecision struct {
	Key     string
	Proceed []string
}

// IssueDesign is put_design: the design's text and the points the Owner
// decides. A text may carry the decision table itself, at its head, as
// designDoc writes it; Decisions, when given too, must say the same.
type IssueDesign struct {
	IssueID         string
	ExpectedVersion int
	Text            string
	Decisions       []DesignDecision
}

// The canonical design document: designHeader, one line a point ("D1: A",
// "D2: A, C") in the order of their numbers or the line "none", the
// separator line, then the text.
const (
	designHeader    = "Decisions (each point the Owner decides: the options under which the work goes on):"
	designNone      = "none"
	designSeparator = "---"
)

// canonicalDecisions checks points and puts them in their order: keys D1,
// D2, … each once, options letters each once, sorted.
func canonicalDecisions(ds []DesignDecision) ([]DesignDecision, error) {
	out := make([]DesignDecision, 0, len(ds))
	seen := map[string]bool{}
	for _, d := range ds {
		if !decisionRE.MatchString(d.Key) || seen[d.Key] {
			return nil, invalid("a design's points are D1, D2, …, each once; not %q", d.Key)
		}
		seen[d.Key] = true
		if len(d.Proceed) == 0 {
			return nil, invalid("point %s names the options under which the work goes on", d.Key)
		}
		proceed := slices.Clone(d.Proceed)
		slices.Sort(proceed)
		for i, l := range proceed {
			if !letterRE.MatchString(l) || (i > 0 && proceed[i-1] == l) {
				return nil, invalid("point %s's options are letters A to Z, each once", d.Key)
			}
		}
		out = append(out, DesignDecision{Key: d.Key, Proceed: proceed})
	}
	slices.SortFunc(out, func(a, b DesignDecision) int { return pointNumber(a.Key) - pointNumber(b.Key) })
	return out, nil
}

func pointNumber(key string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(key, "D"))
	return n
}

// designDoc is the canonical design document D is the sha256 of.
func designDoc(text string, ds []DesignDecision) string {
	var b strings.Builder
	b.WriteString(designHeader + "\n")
	if len(ds) == 0 {
		b.WriteString(designNone + "\n")
	}
	for _, d := range ds {
		fmt.Fprintf(&b, "%s: %s\n", d.Key, strings.Join(d.Proceed, ", "))
	}
	b.WriteString(designSeparator + "\n")
	b.WriteString(text)
	return b.String()
}

// parseDesign reads a design document back: its points (canonical) and its
// text. ok is false when doc does not start with a decision table.
func parseDesign(doc string) (ds []DesignDecision, text string, ok bool, err error) {
	rest, has := strings.CutPrefix(doc, designHeader+"\n")
	if !has {
		return nil, doc, false, nil
	}
	table, text, has := strings.Cut(rest, "\n"+designSeparator+"\n")
	if !has {
		if t, cut := strings.CutPrefix(rest, designSeparator+"\n"); cut {
			table, text = "", t
		} else {
			return nil, "", true, invalid("the design's decision table has no %q line after it", designSeparator)
		}
	}
	if strings.TrimSpace(table) == designNone {
		return []DesignDecision{}, text, true, nil
	}
	for _, line := range strings.Split(table, "\n") {
		key, opts, found := strings.Cut(line, ":")
		if !found {
			return nil, "", true, invalid("the design's decision table has a line %q, not D<n>: <options>", cutBytes(line, 60))
		}
		d := DesignDecision{Key: strings.TrimSpace(key)}
		for _, o := range strings.Split(opts, ",") {
			d.Proceed = append(d.Proceed, strings.TrimSpace(o))
		}
		ds = append(ds, d)
	}
	ds, err = canonicalDecisions(ds)
	return ds, text, true, err
}

// designPoints are the points of an issue's current design, read from its
// stored document.
func designPoints(ctx context.Context, tx pgx.Tx, r issueRow) ([]DesignDecision, error) {
	if r.designText == 0 {
		return nil, nil
	}
	var doc string
	if err := tx.QueryRow(ctx, `SELECT body FROM agentrt.issue_text WHERE issue_text_id = $1`, r.designText).Scan(&doc); err != nil {
		return nil, err
	}
	ds, _, ok, err := parseDesign(doc)
	if err == nil && !ok {
		err = fmt.Errorf("store: %s's design %d is no design document", r.ID, r.designText)
	}
	return ds, err
}

// PutDesign records a design for the current, confirmed brief, stored as
// its canonical document (D covers the decision table and the text). First
// batch: not while the issue's work is live (a design is not changed under
// work in progress either; the design does not settle this, so the
// stricter way).
func PutDesign(ctx context.Context, db DB, w Writer, key string, d IssueDesign) (map[string]any, error) {
	if err := checkText("the design", d.Text, 0); err != nil {
		return nil, err
	}
	given, err := canonicalDecisions(d.Decisions)
	if err != nil {
		return nil, err
	}
	own, text, carries, err := parseDesign(d.Text)
	if err != nil {
		return nil, err
	}
	points := given
	if carries {
		if len(d.Decisions) > 0 && designDoc("", own) != designDoc("", given) {
			return nil, invalid("the design's own decision table says %v; the request says %v", own, given)
		}
		points = own
	}
	if err := checkText("the design's text", text, 0); err != nil {
		return nil, err
	}
	doc := designDoc(text, points)
	decisions := make([]any, 0, len(points))
	for _, p := range points {
		decisions = append(decisions, map[string]any{"key": p.Key, "proceed": p.Proceed})
	}
	req := map[string]any{"issue_id": d.IssueID, "expected_version": d.ExpectedVersion, "design": doc}
	return issueOp(ctx, db, w, "issue-design", key, req, func(tx pgx.Tx, channel string) (map[string]any, error) {
		r, err := lockIssue(ctx, tx, d.IssueID, d.ExpectedVersion)
		if err != nil {
			return nil, err
		}
		if err := r.open(); err != nil {
			return nil, err
		}
		if !r.BriefConfirmed {
			return nil, fmt.Errorf("%w: %s has no confirmed brief: no design before the person confirms what is asked", ErrState, r.ID)
		}
		if m, err := liveWork(ctx, tx, r.ID); err != nil || m != "" {
			return nil, orState(err, "%s has live work (%s): its design changes once that work has ended", r.ID, m)
		}
		ids, err := nextIDs(ctx, tx, "issue_text", 1)
		if err != nil {
			return nil, err
		}
		sum := textSHA(doc) // D: the decision table and the text together
		body := map[string]any{"text_id": ids[0], "design_sha256": sum}
		if len(decisions) > 0 {
			body["decisions"] = decisions
		}
		if _, err := appendIssue(ctx, tx, "issue_design_put", w.Actor, r.ID, "", body); err != nil {
			return nil, err
		}
		if _, err := insertText(ctx, tx, r.ID, w.Actor, issueText{id: ids[0], kind: "design", channel: channel, body: doc}); err != nil {
			return nil, err
		}
		state := r.State
		if slices.Contains([]string{"brief_confirmed", "classified", "in_review", "owner_deciding"}, state) {
			state = "designing"
		}
		if _, err := tx.Exec(ctx, `UPDATE agentrt.issue SET design_text_id = $2, state = $3, version = version + 1 WHERE issue_id = $1`,
			r.ID, ids[0], state); err != nil {
			return nil, err
		}
		return written(ctx, tx, r.ID, map[string]any{"design_sha256": sum})
	})
}

// IssueReview is record_review: the reviewer's report, whole, which tool
// reviewed and which wrote the design, and the round.
type IssueReview struct {
	IssueID         string
	ExpectedVersion int
	Report          string
	ReviewerTool    string
	AuthorTool      string
	Round           int
}

// ReviewResult reads a review's conclusion from its report (§2.2: from the
// last line, not from what a thread says): the last line that is not blank
// and not the report's own end mark ("END OF REPORT"), starting 通过 (pass)
// or 先修 (fix). Anything else is no conclusion.
func ReviewResult(report string) (string, error) {
	lines := strings.Split(strings.TrimSpace(report), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l == "" || l == "END OF REPORT" {
			continue
		}
		switch {
		case strings.HasPrefix(l, "通过"):
			return "pass", nil
		case strings.HasPrefix(l, "先修"):
			return "fix", nil
		}
		return "", invalid("the report's last line says neither 通过 nor 先修: %q", cutBytes(l, 80))
	}
	return "", invalid("the report is empty")
}

// Review rounds (§7.2; review 522 A7): an issue's reviews are numbered from
// 1, each the round after the one before; after two rounds the Owner
// decides (review_dispute): proceed, stop, or another_round, which alone
// lets a further round be recorded and count. The database holds the same
// sequence (0009 guard_issue_check).
const (
	MaxReviewRounds = 2
	AnotherRound    = "another_round"
)

// latestReview is the issue's newest review's round and id (0, 0: none).
func latestReview(ctx context.Context, tx pgx.Tx, issue string, before int64) (int, int64, error) {
	var round int
	var id int64
	err := tx.QueryRow(ctx, `SELECT review_round, issue_check_id FROM agentrt.issue_check WHERE issue_id = $1 AND check_kind = 'review'
	  AND ($2 = 0 OR issue_check_id < $2) ORDER BY issue_check_id DESC LIMIT 1`, issue, before).Scan(&round, &id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, nil
	}
	return round, id, err
}

// anotherRoundAllowed says whether the Owner's newest review_dispute
// decision on (B, D) after review since (and before until, 0 for none)
// chose another round, his own.
func anotherRoundAllowed(ctx context.Context, tx pgx.Tx, issue, B, D string, since, until int64) (bool, error) {
	var result, by, question, channel string
	err := tx.QueryRow(ctx, `SELECT result, created_by_actor_id, COALESCE(question_id, ''), COALESCE(answer_channel, '')
	  FROM agentrt.issue_check WHERE issue_id = $1 AND check_kind = 'owner_decision' AND category = 'review_dispute'
	    AND brief_sha256 = $2 AND design_sha256 = $3 AND issue_check_id > $4 AND ($5 = 0 OR issue_check_id < $5)
	  ORDER BY issue_check_id DESC LIMIT 1`, issue, B, D, since, until).Scan(&result, &by, &question, &channel)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil && result == AnotherRound && ownersOwn(by, question, channel), err
}

// RecordReview records an independent review of the current brief and
// design: the reviewer is another tool than the author, and the round the
// one after the issue's last (a third and later one only on the Owner's
// another_round; 0009 checks both too).
func RecordReview(ctx context.Context, db DB, w Writer, key string, rv IssueReview) (map[string]any, error) {
	if err := checkText("the report", rv.Report, 0); err != nil {
		return nil, err
	}
	result, err := ReviewResult(rv.Report)
	if err != nil {
		return nil, err
	}
	switch {
	case !toolRE.MatchString(rv.ReviewerTool) || !toolRE.MatchString(rv.AuthorTool):
		return nil, invalid("a review names the reviewer's tool and the author's (codex, claude, …)")
	case rv.ReviewerTool == rv.AuthorTool:
		return nil, invalid("a review is independent: %s does not review its own design", rv.ReviewerTool)
	case rv.Round < 1:
		return nil, invalid("a review names its round, from 1")
	}
	req := map[string]any{"issue_id": rv.IssueID, "expected_version": rv.ExpectedVersion, "report": rv.Report,
		"reviewer_tool": rv.ReviewerTool, "author_tool": rv.AuthorTool, "round": rv.Round}
	return issueOp(ctx, db, w, "issue-review", key, req, func(tx pgx.Tx, channel string) (map[string]any, error) {
		r, err := lockIssue(ctx, tx, rv.IssueID, rv.ExpectedVersion)
		if err != nil {
			return nil, err
		}
		if err := r.open(); err != nil {
			return nil, err
		}
		if r.designSHA == "" {
			return nil, fmt.Errorf("%w: %s has no design to review", ErrState, r.ID)
		}
		prev, prevID, err := latestReview(ctx, tx, r.ID, 0)
		if err != nil {
			return nil, err
		}
		if rv.Round != prev+1 {
			return nil, fmt.Errorf("%w: the next review of %s is round %d, not %d", ErrState, r.ID, prev+1, rv.Round)
		}
		if rv.Round > MaxReviewRounds {
			ok, err := anotherRoundAllowed(ctx, tx, r.ID, r.briefSHA, r.designSHA, prevID, 0)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, fmt.Errorf("%w: %s had %d rounds: round %d needs the Owner's another_round on this design (review_dispute)",
					ErrState, r.ID, prev, rv.Round)
			}
		}
		ids, err := nextIDs(ctx, tx, "issue_text", 1)
		if err != nil {
			return nil, err
		}
		checks, err := nextIDs(ctx, tx, "issue_check", 1)
		if err != nil {
			return nil, err
		}
		if _, err := appendIssue(ctx, tx, "issue_reviewed", w.Actor, r.ID, "", map[string]any{"check_id": checks[0], "check_kind": checkReview,
			"result": result, "brief_sha256": r.briefSHA, "design_sha256": r.designSHA, "text_id": ids[0], "round": rv.Round,
			"reviewer_tool": rv.ReviewerTool, "author_tool": rv.AuthorTool}); err != nil {
			return nil, err
		}
		if _, err := insertText(ctx, tx, r.ID, w.Actor, issueText{id: ids[0], kind: "review", channel: channel, body: rv.Report}); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO agentrt.issue_check (issue_check_id, issue_id, check_kind, brief_sha256, design_sha256, result,
		    report_text_id, reviewer_tool, author_tool, review_round, created_by_actor_id)
		  OVERRIDING SYSTEM VALUE VALUES ($1, $2, 'review', $3, $4, $5, $6, $7, $8, $9, $10)`,
			checks[0], r.ID, r.briefSHA, r.designSHA, result, ids[0], rv.ReviewerTool, rv.AuthorTool, rv.Round, w.Actor); err != nil {
			return nil, err
		}
		state := r.State
		if slices.Contains([]string{"brief_confirmed", "classified", "designing", "in_review"}, state) {
			state = "in_review"
		}
		if _, err := tx.Exec(ctx, `UPDATE agentrt.issue SET state = $2, version = version + 1 WHERE issue_id = $1`, r.ID, state); err != nil {
			return nil, err
		}
		return written(ctx, tx, r.ID, map[string]any{"check_id": checks[0], "result": result})
	})
}
