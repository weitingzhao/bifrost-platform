package store

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// A batch of core changes lands on one card (R5:5.7, batch of cores). The
// material of an operation that leads a batch is the batch's: its patch is
// already the whole change M0..R; its soft checks are the union of every
// member's, each read as the member's own would be; its action is the one
// that union takes; and its digest covers the members and their soft
// checks, so a different batch is a different card. The grant written on
// it names the batch (LandingGrant.Batch). At the batch gate the lead
// consumes it as any grant; every other member whose only refusals are
// soft checks the grant waives is authorized by it, its main_intent
// carrying landing_batch_grant with the grant's id.

// LandingGrantBatch is the batch a lead's grant covers: its id and its
// members in order, the lead first. Its M0 and R are the grant's own.
type LandingGrantBatch struct {
	Batch   string   `json:"batch"`
	Members []string `json:"members"`
}

// grantBatchMate is a member of the batch an operation leads, other than
// the lead, with its facts as the database has them.
type grantBatchMate struct {
	op    string
	o     landing.Operation
	facts landing.Facts
}

// grantMateExt is a mate's result, read from Git.
type grantMateExt struct {
	single  bool
	changes []rules.LandingChange
}

// readGrantBatch names the batch op is in, and when op leads it reads
// every other member's facts.
func readGrantBatch(ctx context.Context, tx pgx.Tx, src LandingSource, s landing.State, stream, op string, now time.Time) (*GrantBatch, []grantBatchMate, error) {
	batches, _, err := readLandingBatches(ctx, tx, stream)
	if err != nil {
		return nil, nil, err
	}
	var batch *GrantBatch
	var mates []grantBatchMate
	for _, b := range batches {
		if !slices.Contains(b.Members, op) {
			continue
		}
		batch, mates = &GrantBatch{Batch: b.Batch, Members: []GrantBatchMember{}}, nil
		for _, m := range b.Members {
			o := s.Operations[m]
			batch.Members = append(batch.Members, GrantBatchMember{Op: m, Task: o.Task, A: o.A})
			if b.Lead != op || m == op {
				continue
			}
			f, _, err := landingFactsDB(ctx, tx, src, "prod", "live", o.Task, m, now)
			if err != nil {
				return nil, nil, err
			}
			mates = append(mates, grantBatchMate{op: m, o: o, facts: f})
		}
	}
	return batch, mates, nil
}

// readGrantBatchGit reads each mate's result.
func readGrantBatchGit(ctx context.Context, src GrantSources, mates []grantBatchMate) (map[string]grantMateExt, error) {
	out := map[string]grantMateExt{}
	for _, m := range mates {
		f := m.facts
		single, changes, err := src.Facts.Result(ctx, f.Repo, f.Task, f.Attempt, f.Base, f.A)
		if err != nil {
			return nil, err
		}
		out[m.op] = grantMateExt{single: single, changes: changes}
	}
	return out, nil
}

// batchSoft is the soft set of a material: soft, the operation's own, and
// when it leads a batch the union of every member's, each the member's own
// checks on its result under its rule now (shown on the batch's members).
// A member those checks refuse, or whose result is not one commit, is a
// problem of the material: no grant lands it.
func batchSoft(m *LandingGrantMaterial, d grantDB, x grantExt, soft []rules.LandingRefusal, now time.Time, problem func(string)) []rules.LandingRefusal {
	if len(d.mates) == 0 || m.Batch == nil {
		return soft
	}
	m.Batch = &GrantBatch{Batch: m.Batch.Batch, Members: slices.Clone(m.Batch.Members)}
	union := slices.Clone(soft)
	m.Batch.Members[0].Soft = refusalSet(soft)
	for _, mate := range d.mates {
		f := mate.facts
		ext := x.mates[mate.op]
		f.SingleCommit, f.Changes = ext.single, ext.changes
		reason, own := landing.PolicyChecks(f, now)
		if reason != "" {
			problem(reason)
		}
		if !f.SingleCommit {
			problem("result")
		}
		union = append(union, own...)
		for i := range m.Batch.Members {
			if m.Batch.Members[i].Op == mate.op {
				m.Batch.Members[i].Soft = refusalSet(own)
			}
		}
	}
	return union
}

// batchDigest folds a lead's batch into its material digest: the members in
// order, each with its soft checks, and the batch's M0 and R.
func (m *LandingGrantMaterial) batchDigest(d grantDB) error {
	if len(d.mates) == 0 || m.Batch == nil {
		return nil
	}
	sum, err := sumOf(map[string]any{"v": LandingGrantVersion, "material_digest": m.MaterialDigest, "batch": m.Batch,
		"M0": m.Candidate.M0, "R": m.Candidate.R})
	m.MaterialDigest = sum
	return err
}

// grantBatchOf is the batch a grant on m covers, or nil when its operation
// leads none.
func grantBatchOf(m *LandingGrantMaterial, d grantDB) *LandingGrantBatch {
	if len(d.mates) == 0 || m.Batch == nil {
		return nil
	}
	b := &LandingGrantBatch{Batch: m.Batch.Batch}
	for _, member := range m.Batch.Members {
		b.Members = append(b.Members, member.Op)
	}
	return b
}

// grantBatchKeys is the batch key of a landing_grant that has one.
func grantBatchKeys(v any) []string {
	if m, ok := v.(map[string]any); ok && m["batch"] != nil {
		return []string{"batch"}
	}
	return nil
}

// badGrantBatch is why a grant's batch is refused, or "": exactly its two
// keys, the batch the grant's operation leads, and two members or more (at
// most BatchMax), the lead first, each an operation once.
func badGrantBatch(v any, g LandingGrant) string {
	m, _ := v.(map[string]any)
	if err := closedDecode(m["batch"], &LandingGrantBatch{}, "batch", "members"); err != nil {
		return err.Error()
	}
	b := g.Batch
	if b.Batch != landing.BatchID(g.Op) || len(b.Members) < 2 || len(b.Members) > landing.BatchMax || b.Members[0] != g.Op {
		return "the batch its operation leads, two members or more, the lead first"
	}
	for i, op := range b.Members {
		if grantOp.FindStringSubmatch(op) == nil || slices.Contains(b.Members[:i], op) {
			return "members are operations, each once"
		}
	}
	return ""
}

// batchCover is the lead's grant as the batch gate lets the other members
// rest on it.
type batchCover struct {
	grant *grantView
	batch LandingBatch
}

// leadGrantCover is the grant the lead's main_intent (lead) consumed in this
// gate, when its material named batch b: the same members in the same
// order, the same M0 and R. Nil otherwise: no member rests on it.
func leadGrantCover(ctx context.Context, tx pgx.Tx, stream string, b LandingBatch, lead Authorization) (*batchCover, error) {
	_, order, err := readLandingGrants(ctx, tx, stream)
	if err != nil {
		return nil, err
	}
	for _, g := range order {
		if g.ConsumedSeq != lead.IntentSeq || g.ConsumedOp != lead.Op {
			continue
		}
		if !grantNamesBatch(g, b) || !g.RevokedAt.IsZero() || !g.RefusedAt.IsZero() {
			return nil, nil
		}
		return &batchCover{grant: g, batch: b}, nil
	}
	return nil, nil
}

// grantNamesBatch: g is b's lead's grant and its material was b: the same
// members in the same order, the same M0 and the same R.
func grantNamesBatch(g *grantView, b LandingBatch) bool {
	return g.Batch != nil && g.Op == b.Lead && g.Batch.Batch == b.Batch && slices.Equal(g.Batch.Members, b.Members) && g.M0 == b.M0 && g.R == b.R
}

// covers: member op, pushed on the batch's M0 and R (o its operation, f its
// facts at the decision at), fails only soft checks the lead's grant
// waives, before the grant's expiry and with grants still enabled.
func (c *batchCover) covers(op string, o landing.Operation, f landing.Facts, soft []rules.LandingRefusal, at time.Time) bool {
	return c != nil && slices.Contains(c.batch.Members[1:], op) && o.Phase == "branch_pushed" && o.R == c.batch.R && o.M0 == c.batch.M0 &&
		f.GrantEnabled && GrantActionSupported(c.grant.Action, f.Repo) && at.Before(c.grant.exp) &&
		rules.LandingWaivesCover(soft, refusalsOf(c.grant.Waives))
}

// record puts on a covered member's main_intent body the grant it rests on.
func (c *batchCover) record(body map[string]any, op string, soft []rules.LandingRefusal, at time.Time, ruleVersion int) {
	if c == nil {
		return
	}
	body["landing_batch_grant"] = toBody(LandingBatchGrant{V: LandingGrantVersion, GrantID: c.grant.GrantID, Batch: c.batch.Batch, Op: op,
		SoftNow: refusalSet(soft), DecidedAt: at.UTC().Format(time.RFC3339Nano)})
	body["text"] = fmt.Sprintf("Landing main_intent on batch %s's grant %s; second gate rule version %d", c.batch.Batch, c.grant.GrantID, ruleVersion)
}

// LandingBatchGrant rides on the main_intent of a batch member other than
// its lead that rests on the lead's grant: the grant, the batch, the member
// and the soft checks the grant waived for it.
type LandingBatchGrant struct {
	V         string   `json:"v"`
	GrantID   string   `json:"grant_id"`
	Batch     string   `json:"batch"`
	Op        string   `json:"op"`
	SoftNow   []string `json:"soft_now"`
	DecidedAt string   `json:"decided_at"`
}

func parseLandingBatchGrant(v any) (LandingBatchGrant, error) {
	var u LandingBatchGrant
	if err := closedDecode(v, &u, "v", "grant_id", "batch", "op", "soft_now", "decided_at"); err != nil {
		return u, fmt.Errorf("landing_batch_grant: %w", err)
	}
	if _, ok := grantTime(u.DecidedAt); !ok || u.V != LandingGrantVersion || !landingRequestID.MatchString(u.GrantID) ||
		!strings.HasPrefix(u.Batch, "batch:") || grantOp.FindStringSubmatch(u.Op) == nil || !validCategories(u.SoftNow, false) {
		return u, fmt.Errorf("landing_batch_grant: invalid")
	}
	return u, nil
}

// badLandingBatchGrant is checkBody's closed-schema check of the record.
func badLandingBatchGrant(v any) string {
	if _, err := parseLandingBatchGrant(v); err != nil {
		return err.Error()
	}
	return ""
}

// checkLandingBatchGrantAppend: landing_batch_grant rides alone on the
// controller's main_intent of a member, not the lead, of the batch its
// grant names; the lead has consumed that grant, unrevoked and unrefused,
// and it had not expired at the decision; the member is pushed on the
// grant's M0 and R; and the grant waives the soft checks recorded.
func checkLandingBatchGrantAppend(ctx context.Context, tx pgx.Tx, ev Event, at time.Time) error {
	v, has := ev.Body["landing_batch_grant"]
	if !has {
		return nil
	}
	u, err := parseLandingBatchGrant(v)
	if err != nil {
		return invalid("%v", err)
	}
	r, _ := ev.Body["landing"].(map[string]any)
	_, use := ev.Body["landing_grant_use"]
	_, clear := ev.Body["landing_rule_clear"]
	decided, _ := grantTime(u.DecidedAt)
	if ev.Type != "note" || ev.Actor != "controller" || ev.Stream != landingRepoStream || r == nil || r["kind"] != "main_intent" || r["op"] != u.Op ||
		use || clear || decided.After(at) {
		return invalid("landing_batch_grant rides on the controller's main_intent of its member, alone")
	}
	grants, _, err := readLandingGrants(ctx, tx, ev.Stream)
	if err != nil {
		return err
	}
	s, err := LandingState(ctx, tx, ev.Stream)
	if err != nil {
		return err
	}
	g, o := grants[u.GrantID], s.Operations[u.Op]
	switch {
	case g == nil || g.Batch == nil || g.Batch.Batch != u.Batch || !slices.Contains(g.Batch.Members[1:], u.Op):
		return invalid("landing_batch_grant names a grant of its member's batch")
	case g.ConsumedSeq == 0 || g.ConsumedOp != g.Op || !g.RevokedAt.IsZero() || !g.RefusedAt.IsZero() || !decided.Before(g.exp):
		return invalid("the batch's grant %s is not its lead's, used", g.GrantID)
	case o.Phase != "branch_pushed" || o.R != g.R || o.M0 != g.M0:
		return invalid("the member is not pushed on the grant's M0 and R")
	case !rules.LandingWaivesCover(refusalsOf(u.SoftNow), refusalsOf(g.Waives)):
		return invalid("the grant does not waive %v", u.SoftNow)
	}
	return nil
}
