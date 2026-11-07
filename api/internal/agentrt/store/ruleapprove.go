package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// A capacity change approved on a card (R5 phase 2 step 3b; the Owner
// 2026-11-06 Q1 A, Q2 A, Q5 A). A seat or a thread proposes the next version
// of a rule (rule_proposed, ProposeRule); when that draft changes only
// capacity fields (rules.CapacityOnly), the Owner may approve it on the PROD
// Console through the tier D action agentrt_rule_approve instead of signing
// it. The approval's executor calls ApproveRule, which writes the version
// with approval_id and its event rule_approved under the same locks as a
// signed or tightened version (writeRule). The signed and tighten routes
// are unchanged.

// RuleApproval names the draft a card approves: the rule, the version and
// the artifact its text was proposed in (rule_proposed), and, when it is
// written, the approval (appr_<16 hex>).
type RuleApproval struct {
	RuleID         string
	Version        int
	TextArtifactID string
	ID             string
}

// ruleApproval is a RuleApproval checked against its draft: what the card
// changes, old and new.
type ruleApproval struct {
	RuleApproval
	changes []rules.CapacityChange
}

// RuleApprovalCheck is what a card shows and what its creation checks: the
// draft's rule, version and artifact, the version in force before it, and
// each capacity field it changes with its old and new value.
type RuleApprovalCheck struct {
	RuleID          string                 `json:"rule_id"`
	Version         int                    `json:"rule_version"`
	PreviousVersion int                    `json:"previous_version"`
	TextArtifactID  string                 `json:"text_artifact_id"`
	Changes         []rules.CapacityChange `json:"changes"`
}

func (a RuleApproval) check() error {
	switch {
	case a.RuleID == "":
		return invalid("rule_id is required")
	case a.Version < 2:
		return invalid("rule_version is the next version of a rule in force, 2 or more")
	case !artifactIDRE.MatchString(a.TextArtifactID):
		return invalid("text_artifact_id is the draft's artifact (sha256:<64 hex>), not %q", a.TextArtifactID)
	}
	if _, _, err := rules.KindOf(a.RuleID); err != nil {
		return invalid("%v", err)
	}
	return nil
}

// ruleDraft reads the draft a names: a rule_proposed event of its rule for
// that version and artifact, and the text stored in the artifact.
func ruleDraft(ctx context.Context, tx pgx.Tx, a RuleApproval) (rules.Rule, []byte, error) {
	var proposed bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'rule_proposed'
	    AND body_json->>'rule_id' = $2 AND (body_json->>'rule_version')::int = $3 AND body_json->>'text_artifact_id' = $4)`,
		RuleStream(a.RuleID), a.RuleID, a.Version, a.TextArtifactID).Scan(&proposed); err != nil {
		return rules.Rule{}, nil, err
	}
	if !proposed {
		return rules.Rule{}, nil, invalid("no draft of rule %s v%d was proposed with artifact %s", a.RuleID, a.Version, a.TextArtifactID)
	}
	var text []byte
	if err := tx.QueryRow(ctx, `SELECT body FROM agentrt.artifact_blob WHERE artifact_id = $1`, a.TextArtifactID).Scan(&text); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return rules.Rule{}, nil, invalid("artifact %s is not stored", a.TextArtifactID)
		}
		return rules.Rule{}, nil, err
	}
	r, err := ruleRequest(a.RuleID, text)
	if err != nil {
		return rules.Rule{}, nil, err
	}
	if r.Version != a.Version {
		return rules.Rule{}, nil, invalid("artifact %s is version %d of rule %s, not %d", a.TextArtifactID, r.Version, a.RuleID, a.Version)
	}
	return r, text, nil
}

// approvable checks r may go on a card after prev: the next version of a
// rule that has one, changing only capacity fields.
func approvable(prev *StoredRule, r rules.Rule) ([]rules.CapacityChange, error) {
	if prev == nil {
		return nil, invalid("rule %s has no version yet: its first version is signed", r.ID)
	}
	if r.Version != prev.Version+1 {
		return nil, invalid("rule %s is at version %d; the draft is version %d, not the next", r.ID, prev.Version, r.Version)
	}
	if err := rules.CapacityOnly(prev.Rule, r); err != nil {
		return nil, fmt.Errorf("%w: rule %s v%d is not a capacity change: %v", ErrUnsigned, r.ID, r.Version, err)
	}
	changes, _, err := rules.CapacityChanges(prev.Rule, r)
	return changes, err
}

// CheckRuleApproval checks, writing nothing, that a card may be created for
// a (agentrt_rule_approve's creation check): the draft was proposed, is the
// next version of its rule and changes only capacity fields. It answers the
// changes the card shows.
func CheckRuleApproval(ctx context.Context, db DB, a RuleApproval) (RuleApprovalCheck, error) {
	if err := a.check(); err != nil {
		return RuleApprovalCheck{}, err
	}
	var out RuleApprovalCheck
	err := readOnly(ctx, db, func(tx pgx.Tx) error {
		r, _, err := ruleDraft(ctx, tx, a)
		if err != nil {
			return err
		}
		prev, err := latestRule(ctx, tx, a.RuleID)
		if err != nil {
			return err
		}
		changes, err := approvable(prev, r)
		if err != nil {
			return err
		}
		out = RuleApprovalCheck{RuleID: a.RuleID, Version: a.Version, PreviousVersion: prev.Version, TextArtifactID: a.TextArtifactID, Changes: changes}
		return nil
	})
	return out, err
}

// ApproveRule writes the draft a names as the next version of its rule, by
// the approval a.ID (Q1 A): under the same locks as writeRule, it requires
// the draft be the next version and change only capacity fields, then
// writes the row with approval_id and rule_approved, whose body names the
// approval, the artifact and the changes. The same approval again (its
// executor retried) answers what it wrote; another approval of a version
// already written is refused.
func ApproveRule(ctx context.Context, db DB, w Writer, a RuleApproval) (map[string]any, error) {
	if err := a.check(); err != nil {
		return nil, err
	}
	if !approvalIDRE.MatchString(a.ID) {
		return nil, invalid("a rule is approved by an approval (appr_<16 hex>), not %q", a.ID)
	}
	var r rules.Rule
	var text []byte
	if err := readOnly(ctx, db, func(tx pgx.Tx) error {
		var err error
		r, text, err = ruleDraft(ctx, tx, a)
		return err
	}); err != nil {
		return nil, err
	}
	rec := &ruleApproval{RuleApproval: a}
	return writeRule(ctx, db, w, r, text, func(prev *StoredRule, _ error) error {
		changes, err := approvable(prev, r)
		rec.changes = changes
		return err
	}, ruleRecord{approval: rec})
}

// done is the version this approval already wrote, read under writeRule's
// locks: nil when it wrote none; refused when another approval or route
// wrote that version.
func (a *ruleApproval) done(ctx context.Context, tx pgx.Tx, r rules.Rule) (map[string]any, error) {
	var approval *string
	var event int64
	err := tx.QueryRow(ctx, `SELECT r.approval_id, COALESCE((SELECT e.event_id FROM agentrt.event e WHERE e.event_stream_id = $3
	    AND e.event_type = 'rule_approved' AND e.body_json->>'approval_id' = r.approval_id
	    AND (e.body_json->>'rule_version')::int = r.rule_version ORDER BY e.seq LIMIT 1), 0)
	  FROM agentrt.rule r WHERE r.rule_id = $1 AND r.rule_version = $2`, r.ID, r.Version, RuleStream(r.ID)).Scan(&approval, &event)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if approval == nil || *approval != a.ID {
		return nil, fmt.Errorf("%w: rule %s v%d is already written, not by approval %s", ErrState, r.ID, r.Version, a.ID)
	}
	return map[string]any{"rule_id": r.ID, "rule_version": r.Version, "direction": r.Direction, "signed": false,
		"approval_id": a.ID, "event_id": event, "duplicate": true}, nil
}

// capacityDoc is the changes as an event body or a result holds them.
func capacityDoc(changes []rules.CapacityChange) []any {
	out := make([]any, 0, len(changes))
	for _, c := range changes {
		out = append(out, map[string]any{"field": c.Field, "old": c.Old, "new": c.New})
	}
	return out
}
