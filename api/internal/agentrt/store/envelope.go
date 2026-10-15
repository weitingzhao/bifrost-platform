package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// The rules the runtime acts on (design v5 §5.2; proposal §2), each read at
// the database's clock in the transaction that acts on it. Without one in
// force, or with one whose text does not read, the strictest default holds:
// no concurrency rule dispatches nothing, no standing envelope starts no
// mission without the Owner.

// inForce is CurrentRule at the database's clock; a stored rule that does
// not read counts as none.
func inForce(ctx context.Context, tx pgx.Tx, id string) (*StoredRule, error) {
	var now time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return nil, err
	}
	r, err := CurrentRule(ctx, tx, id, now)
	if errors.Is(err, errBadRule) {
		return nil, nil
	}
	return r, err
}

// perHost is how many live attempts a host may hold now: its own slots, no
// more than the concurrency rule allows, and none without the rule.
func perHost(ctx context.Context, tx pgx.Tx, slots int) (int, error) {
	r, err := inForce(ctx, tx, rules.KindConcurrency)
	if err != nil || r == nil {
		return 0, err
	}
	return min(slots, *r.PerHost), nil
}

// envelopeFit says whether a mission falls inside the standing envelope in
// force, and why not when it does not: an action above the envelope's, or a
// budget it does not state or the envelope does not cover. Which executors
// and hosts may work it is checked when its tasks are claimed.
func envelopeFit(env *StoredRule, p MissionProposal) (bool, string) {
	switch e := env; {
	case e == nil:
		return false, EnvelopePending
	case actionRank(p.MaxAction) > actionRank(e.Envelope.MaxAction):
		return false, fmt.Sprintf("max_action %s is above the envelope's %s", p.MaxAction, e.Envelope.MaxAction)
	case p.MaxAttempts == 0 || p.MaxWallHours == 0:
		return false, "the mission states no budget and no budget:" + p.Tenant + " rule gives one"
	case p.MaxAttempts > e.Envelope.Budget.MaxAttempts || p.MaxWallHours > e.Envelope.Budget.MaxWallHours:
		return false, fmt.Sprintf("its budget (%d attempts, %d hours) is above the envelope's (%d, %d)", p.MaxAttempts, p.MaxWallHours,
			e.Envelope.Budget.MaxAttempts, e.Envelope.Budget.MaxWallHours)
	}
	return true, ""
}

func actionRank(a string) int { return slices.Index([]string{"none", "branch"}, a) }

// envelopeFor is the standing envelope in force that a mission started
// under, read once per claim; nil when the mission started without one, or
// when it did and none is in force now (its tasks then wait).
type envelopes map[string]*StoredRule

func (es envelopes) get(ctx context.Context, tx pgx.Tx, id string) (*StoredRule, error) {
	if r, ok := es[id]; ok {
		return r, nil
	}
	r, err := inForce(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	es[id] = r
	return r, nil
}

// envelopeTools narrows a claim to the envelope a mission started under: the
// host must be one it lists, and only the tools of its executors may take
// the task. ok is false when nothing here may.
func envelopeTools(env *StoredRule, r ClaimRequest) (ClaimRequest, bool) {
	if env == nil || !slices.Contains(env.Envelope.Hosts, r.HostID) {
		return r, false
	}
	var tools []Tool
	for _, tl := range r.Tools {
		if slices.Contains(env.Envelope.Executors, tl.Executor) {
			tools = append(tools, tl)
		}
	}
	r.Tools = tools
	return r, len(tools) > 0
}
