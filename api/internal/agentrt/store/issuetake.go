package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// Taking an issue (DESIGN-crew-intake §4.3 take predicate, §4.4): the only
// way an issue gets work. In one transaction take_issue takes the product
// lock, the issue row, checks the version and the take predicate against the
// evidence of the current brief B and design D, counts the product's live
// work, makes the mission (work id BI-n, proposeMissionTx), binds it as the
// issue's work for B and writes issue_taken; anything that fails rolls all
// of it back, mission included. First batch (B2): one live work per issue.

// productLock serializes the takes of one product (§4.3): two issues never
// both take its last slot.
const productLock = 742122

// IssueTake is take_issue: the issue at the version read, the commitment's
// text and the mission to make for it (its work id is the issue's).
type IssueTake struct {
	IssueID         string
	ExpectedVersion int
	Commitment      string
	Mission         MissionProposal
}

// TakeIssue takes an issue: the take predicate holds, by the intake-gates
// rule in force, or nothing is written. A mission inside the standing
// envelope starts at once; one outside it waits for the Owner (ask, as any
// mission's card), and either way the work holds the product's slot until
// the mission ends. The standing exception for a small fault takes no
// priority from the Owner, and then only a mission inside
// standing-envelope:dev (§4.3).
func TakeIssue(ctx context.Context, db DB, w Writer, key string, t IssueTake, ask Ask) (map[string]any, error) {
	if _, err := crewWriter(w); err != nil {
		return nil, err
	}
	if !issueIDRE.MatchString(t.IssueID) {
		return nil, invalid("%q is not an issue id (BI-<n>)", t.IssueID)
	}
	if t.Mission.WorkID != "" && t.Mission.WorkID != t.IssueID {
		return nil, invalid("the mission of %s has its work id, not %s", t.IssueID, t.Mission.WorkID)
	}
	p := t.Mission
	p.WorkID, p.issueTake = t.IssueID, true
	p, err := p.check()
	if err != nil {
		return nil, err
	}
	if err := p.bounded(); err != nil {
		return nil, err
	}
	if err := checkText("the commitment", t.Commitment, 0); err != nil {
		return nil, err
	}
	req := map[string]any{"issue_id": t.IssueID, "expected_version": t.ExpectedVersion, "commitment": t.Commitment, "mission": p.request()}
	return issueOp(ctx, db, w, "issue-take", key, req, func(tx pgx.Tx, channel string) (map[string]any, error) {
		var product string
		err := tx.QueryRow(ctx, `SELECT product FROM agentrt.issue WHERE issue_id = $1`, t.IssueID).Scan(&product)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, invalid("there is no issue %s", t.IssueID)
		}
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, hashtext($2))`, productLock, product); err != nil {
			return nil, err
		}
		r, err := lockIssue(ctx, tx, t.IssueID, t.ExpectedVersion)
		if err != nil {
			return nil, err
		}
		g, err := takePredicate(ctx, tx, r)
		if err != nil {
			return nil, err
		}
		if g.exception != "" && p.Tenant != "dev" {
			return nil, fmt.Errorf("%w: %s is taken without the Owner's priority only inside standing-envelope:dev", ErrState, r.ID)
		}
		hook("taking", r.ID)
		missionAsk := ask
		if g.exception != "" {
			missionAsk = nil // inside the envelope or not at all
		}
		m, err := proposeMissionTx(ctx, tx, w, p, key, missionAsk)
		if err != nil {
			return nil, err
		}
		mission, _ := m["mission_id"].(string)
		state, _ := m["state"].(string)
		if g.exception != "" && state != "active" {
			return nil, fmt.Errorf("%w: %s has no priority from the Owner, and its mission is not inside standing-envelope:dev (%v): "+
				"a small fault is taken without him only inside it", ErrState, r.ID, m["waiting"])
		}
		var commitment int
		if err := tx.QueryRow(ctx, `SELECT COALESCE(max(commitment), 0) + 1 FROM agentrt.issue_work WHERE issue_id = $1`, r.ID).Scan(&commitment); err != nil {
			return nil, err
		}
		ids, err := nextIDs(ctx, tx, "issue_text", 1)
		if err != nil {
			return nil, err
		}
		body := map[string]any{"commitment": commitment, "brief_sha256": r.briefSHA, "rule_id": g.rule.ID, "rule_version": g.rule.Version,
			"risk_class": r.RiskClass, "mission_state": state, "live": g.live, "limit": g.limit, "text_id": ids[0]}
		if r.designSHA != "" {
			body["design_sha256"] = r.designSHA
		}
		if g.exception != "" {
			body["exception"] = g.exception
		}
		if _, err := appendIssue(ctx, tx, "issue_taken", w.Actor, r.ID, mission, body); err != nil {
			return nil, err
		}
		if _, err := insertText(ctx, tx, r.ID, w.Actor, issueText{id: ids[0], kind: "note", channel: channel, body: t.Commitment}); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO agentrt.issue_work (issue_id, commitment, mission_id, brief_sha256) VALUES ($1, $2, $3, $4)`,
			r.ID, commitment, mission, r.briefSHA); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE agentrt.issue SET state = 'active', waiting_on = 'crew', build_owner = $2, version = version + 1
		  WHERE issue_id = $1`, r.ID, w.Actor); err != nil {
			return nil, err
		}
		extra := map[string]any{"mission_id": mission, "mission_state": state, "commitment": commitment,
			"rule": map[string]any{"rule_id": g.rule.ID, "rule_version": g.rule.Version}}
		if q, ok := m["question_id"]; ok {
			extra["question_id"] = q
		}
		if g.exception != "" {
			extra["exception"] = g.exception
		}
		return written(ctx, tx, r.ID, extra)
	})
}

// takeGates is what the take predicate found: the rule it read, the
// exception it used, the product's live work and its bound.
type takeGates struct {
	rule        *StoredRule
	exception   string
	live, limit int
}

// takePredicate is §4.3's take predicate on a locked issue: every item the
// rule's class lists, and always 1, 2, 7 and 8. It names everything missing
// at once.
func takePredicate(ctx context.Context, tx pgx.Tx, r issueRow) (takeGates, error) {
	var g takeGates
	if err := r.open(); err != nil {
		return g, err
	}
	if m, err := liveWork(ctx, tx, r.ID); err != nil || m != "" {
		return g, orState(err, "%s already has live work (%s): one at a time", r.ID, m)
	}
	rule, err := inForce(ctx, tx, rules.KindIntake)
	if err != nil {
		return g, err
	}
	if rule == nil || rule.Intake == nil {
		return g, fmt.Errorf("%w: no intake-gates rule is in force and reads (item 8): nothing is taken", ErrState)
	}
	g.rule = rule
	gates := rule.Intake
	if r.RiskClass == "" {
		return g, fmt.Errorf("%w: %s is not classified (item 2)", ErrState, r.ID)
	}
	need := func(item string) bool { return gates.Requires(r.RiskClass, item) }
	var missing []string
	ev, err := issueEvidence(ctx, tx, r)
	if err != nil {
		return g, err
	}
	// 1 and 2, always (§4.3: the exceptions skip only 3).
	if !ev.briefConfirmed {
		missing = append(missing, "1 brief_confirmed: the current brief has no confirmation from "+r.AsksPerson)
	}
	if !ev.classified {
		missing = append(missing, "2 classified: the newest classification is not of the current brief at class "+r.RiskClass)
	}
	// 3, or the standing exception for a small fault.
	if need(rules.IntakeOwnerPriority) && !ev.prioritized {
		switch {
		case ev.priorityGiven:
			missing = append(missing, "3 owner_priority: the Owner set "+ev.priority+" for the current brief")
		case gates.Allows(rules.IntakeFaultSmall) && r.RiskClass == "small" && ev.category == "fault":
			g.exception = rules.IntakeFaultSmall
		default:
			missing = append(missing, "3 owner_priority: the Owner has set no priority for the current brief")
		}
	}
	if need(rules.IntakeDesign) && r.designSHA == "" {
		missing = append(missing, "4 design: there is no design")
	}
	if need(rules.IntakeReview) && !ev.reviewed {
		missing = append(missing, "5 review: no independent review of the current brief and design passed (or, after two rounds, was let through by the Owner)")
	}
	// 6: by the rule, and whenever the design lists points (a design with
	// points the Owner has not decided is not taken at any class; the
	// stricter reading where the design does not settle it).
	if need(rules.IntakeOwnerDecisions) || len(ev.undecided) > 0 {
		if len(ev.undecided) > 0 {
			missing = append(missing, "6 owner_decisions: the Owner has not chosen to go on at "+strings.Join(ev.undecided, ", "))
		}
	}
	// 7, always: the product's live work, counted in its lock.
	g.limit = gates.MaxLivePerProduct
	if err := tx.QueryRow(ctx, `SELECT count(DISTINCT w.issue_id) FROM agentrt.issue_work w JOIN agentrt.issue i ON i.issue_id = w.issue_id
	  WHERE i.product = $1 AND w.ended_at IS NULL`, r.Product).Scan(&g.live); err != nil {
		return g, err
	}
	if g.live >= g.limit {
		missing = append(missing, fmt.Sprintf("7 quota: %s has live work for %d issues, its limit %d", r.Product, g.live, g.limit))
	}
	if len(missing) > 0 {
		return g, fmt.Errorf("%w: %s is not taken (class %s, %s v%d): %s", ErrState, r.ID, r.RiskClass, rule.ID, rule.Version,
			strings.Join(missing, "; "))
	}
	return g, nil
}

// evidence is the newest evidence of each kind, read against the current
// brief and design.
type evidence struct {
	briefConfirmed, classified bool
	category                   string
	prioritized, priorityGiven bool
	priority                   string
	reviewed                   bool
	undecided                  []string
}

// ownersOwn says whether a decision is the Owner's own (§4.3): by his token,
// or on his card answered other than by a rule (0009 holds the writer to
// that too).
func ownersOwn(by, question, channel string) bool {
	return (by == "owner" && question == "") || (by == "controller" && question != "" && channel != "" && channel != "rule")
}

// latestCheck is the newest check of a kind for an issue (and, when cat is
// not "", of that category): ok false when there is none.
type checkRow struct {
	ok                         bool
	brief, design, result, cat string
	by, question, channel      string
	round                      int
}

func latestCheck(ctx context.Context, tx pgx.Tx, issue, kind, cat string) (checkRow, error) {
	var c checkRow
	err := tx.QueryRow(ctx, `SELECT brief_sha256, COALESCE(design_sha256, ''), result, COALESCE(category, ''), created_by_actor_id,
	  COALESCE(question_id, ''), COALESCE(answer_channel, ''), COALESCE(review_round, 0)
	  FROM agentrt.issue_check WHERE issue_id = $1 AND check_kind = $2 AND ($3 = '' OR category = $3)
	  ORDER BY issue_check_id DESC LIMIT 1`, issue, kind, cat).Scan(&c.brief, &c.design, &c.result, &c.cat, &c.by, &c.question, &c.channel, &c.round)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, nil
	}
	c.ok = err == nil
	return c, err
}

func issueEvidence(ctx context.Context, tx pgx.Tx, r issueRow) (evidence, error) {
	var ev evidence
	B, D := r.briefSHA, r.designSHA
	if B == "" {
		return ev, nil
	}
	// 1: the confirmation names a confirm_brief ask of this brief, answered
	// confirmed, by the person asked (0009 held the answerer to that).
	if r.ask != 0 {
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.issue_ask WHERE issue_ask_id = $1 AND issue_id = $2
		  AND purpose = 'confirm_brief' AND verdict = 'confirmed' AND bound_sha256 = $3 AND brief_sha256 = $3 AND person = $4
		  AND superseded_at IS NULL)`, r.ask, r.ID, B, r.AsksPerson).Scan(&ev.briefConfirmed); err != nil {
			return ev, err
		}
	}
	// 2: the newest classification, of B, at the issue's class.
	c, err := latestCheck(ctx, tx, r.ID, checkClassification, "")
	if err != nil {
		return ev, err
	}
	ev.classified = c.ok && c.brief == B && c.result == r.RiskClass
	if ev.classified {
		ev.category = c.cat
	}
	// 3: the newest priority, of B, the Owner's own, to do it.
	p, err := latestCheck(ctx, tx, r.ID, checkPriority, "")
	if err != nil {
		return ev, err
	}
	ev.priorityGiven = p.ok && p.brief == B && ownersOwn(p.by, p.question, p.channel)
	ev.priority = p.result
	ev.prioritized = ev.priorityGiven && slices.Contains([]string{"p1", "p2", "p3"}, p.result)
	if D == "" {
		return ev, nil
	}
	// 5: the newest review, of (B, D): passed; or asking for fixes after two
	// rounds, and the Owner let the work go on (review_dispute: proceed).
	rv, err := latestCheck(ctx, tx, r.ID, checkReview, "")
	if err != nil {
		return ev, err
	}
	if rv.ok && rv.brief == B && rv.design == D {
		switch {
		case rv.result == "pass":
			ev.reviewed = true
		case rv.result == "fix" && rv.round >= 2:
			d, err := latestCheck(ctx, tx, r.ID, checkOwnerDecision, "review_dispute")
			if err != nil {
				return ev, err
			}
			ev.reviewed = d.ok && d.brief == B && d.design == D && d.result == "proceed" && ownersOwn(d.by, d.question, d.channel)
		}
	}
	// 6: each point the design lists, the Owner's newest choice of it, of
	// (B, D), one the design says the work goes on under.
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT COALESCE(body_json->'decisions', '[]') FROM agentrt.event WHERE event_stream_id = $1
	  AND event_type = 'issue_design_put' AND (body_json->>'text_id')::bigint = $2 ORDER BY event_id DESC LIMIT 1`,
		IssueStream(r.ID), r.designText).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return ev, fmt.Errorf("store: %s's design %d has no issue_design_put", r.ID, r.designText)
	}
	if err != nil {
		return ev, err
	}
	var points []struct {
		Key     string   `json:"key"`
		Proceed []string `json:"proceed"`
	}
	if err := json.Unmarshal(raw, &points); err != nil {
		return ev, err
	}
	for _, pt := range points {
		d, err := latestCheck(ctx, tx, r.ID, checkOwnerDecision, pt.Key)
		if err != nil {
			return ev, err
		}
		if !(d.ok && d.brief == B && d.design == D && slices.Contains(pt.Proceed, d.result) && ownersOwn(d.by, d.question, d.channel)) {
			ev.undecided = append(ev.undecided, pt.Key)
		}
	}
	return ev, nil
}
