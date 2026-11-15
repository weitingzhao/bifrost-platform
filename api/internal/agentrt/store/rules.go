package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
)

// The runtime's rules (design v5 §5.2 「规则」, Owner 2026-10-14 「规则 A」):
// rows of agentrt.rule, append only, one version after another. What a rule
// says is what rules.Parse reads from the text stored with it; body_json is
// derived from that text and checked again on every read. A rule that needs
// the Owner's signature takes effect only with one made by the compiled-in
// key in the runtime's namespace; a tightening takes effect unsigned once the
// comparison with the version before shows it grants nothing more.

// ErrUnsigned is a rule that needs the Owner's signature and has none that
// verifies: anything but a shown tightening without one, a signature by
// another key or in another namespace, or no trust anchor compiled in.
var ErrUnsigned = errors.New("store: the rule needs the Owner's signature")

// ruleLock serializes the versions of one rule.
const ruleLock = 742111

// RuleStream is the event stream of one rule's versions and the rows they
// write.
func RuleStream(id string) string { return "rule:" + id }

// StoredRule is a version of a rule as the table has it, read again from its
// text.
type StoredRule struct {
	rules.Rule
	SignedBy string
}

// errBadRule is a stored rule that does not read: the runtime acts as if
// there were none.
var errBadRule = errors.New("store: a stored rule does not read")

// readRule parses a stored rule's text and checks the stored body_json is the
// canonical JSON of what the text says: a row whose two disagree is no rule.
func readRule(id string, version int, direction string, effective, expires time.Time, text string, body []byte,
	signedBy *string) (*StoredRule, error) {
	r, err := rules.Parse([]byte(text))
	if err != nil {
		return nil, fmt.Errorf("%w: rule %s v%d does not parse: %v", errBadRule, id, version, err)
	}
	want, err := r.BodyJSON()
	if err != nil {
		return nil, fmt.Errorf("%w: rule %s v%d: %v", errBadRule, id, version, err)
	}
	got, err := canon.Marshal(body)
	if err != nil || !bytes.Equal(got, want) || r.ID != id || r.Version != version {
		return nil, fmt.Errorf("%w: rule %s v%d: body_json is not what its text says", errBadRule, id, version)
	}
	if r.Direction != direction || !r.Effective.Equal(effective) || !r.Expires.Equal(expires) {
		return nil, fmt.Errorf("%w: rule %s v%d: its columns are not what its text says", errBadRule, id, version)
	}
	s := &StoredRule{Rule: r}
	if signedBy != nil {
		s.SignedBy = *signedBy
	}
	return s, nil
}

// latestRule is the last version of a rule, whether in force or not; nil
// when there is none.
func latestRule(ctx context.Context, q pgx.Tx, id string) (*StoredRule, error) {
	var version int
	var direction, text string
	var body []byte
	var signedBy *string
	var effective, expires time.Time
	err := q.QueryRow(ctx, `SELECT rule_version, direction, effective_at, expires_at, body_text, body_json, signed_by FROM agentrt.rule
	  WHERE rule_id = $1 ORDER BY rule_version DESC LIMIT 1`, id).Scan(&version, &direction, &effective, &expires, &text, &body, &signedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return readRule(id, version, direction, effective, expires, text, body, signedBy)
}

// CurrentRule is the rule in force at now: the last version that has taken
// effect, unless it has expired. nil means none is in force and the caller
// keeps the strictest default (proposal §2): an expired tightening never
// brings back the looser version before it.
func CurrentRule(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, id string, now time.Time) (*StoredRule, error) {
	return effectiveRule(ctx, q, id, now, false)
}

// effectiveRule preserves the latest effective configuration even after
// expiry, solely for controllers observing already-started work. It never
// grants authorization; CurrentRule is still the authorization entry point.
func effectiveRule(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, id string, now time.Time, includeExpired bool) (*StoredRule, error) {
	var version int
	var direction, text string
	var body []byte
	var signedBy *string
	var effective, expires time.Time
	err := q.QueryRow(ctx, `SELECT rule_version, direction, effective_at, expires_at, body_text, body_json, signed_by FROM agentrt.rule
	  WHERE rule_id = $1 AND effective_at <= $2 ORDER BY rule_version DESC LIMIT 1`, id, now).Scan(&version, &direction, &effective,
		&expires, &text, &body, &signedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !includeExpired && !expires.After(now) {
		return nil, nil
	}
	return readRule(id, version, direction, effective, expires, text, body, signedBy)
}

// ruleRequest parses a rule's text for the route that names id.
func ruleRequest(id string, text []byte) (rules.Rule, error) {
	if rules.Retired(id) {
		return rules.Rule{}, invalid("rule %s is of a kind retired with the dispatch lane: it is no longer checked, proposed, signed or tightened", id)
	}
	r, err := rules.Parse(text)
	if err != nil {
		return rules.Rule{}, invalid("%v", err)
	}
	if r.ID != id {
		return rules.Rule{}, invalid("the text is rule %s, not %s", r.ID, id)
	}
	return r, nil
}

// follows checks r is the version after prev (or the first) and whether it
// only tightens prev.
func follows(prev *StoredRule, r rules.Rule) (tightens error, err error) {
	if prev == nil {
		if r.Version != 1 {
			return nil, invalid("rule %s has no version yet; this is version %d", r.ID, r.Version)
		}
		return errors.New("there is no version to tighten"), nil
	}
	if r.Version != prev.Version+1 {
		return nil, invalid("rule %s is at version %d; this is version %d", r.ID, prev.Version, r.Version)
	}
	return rules.Tightens(prev.Rule, r), nil
}

// CheckRule reads a rule's text as the sign and tighten routes would, writes
// nothing, and says what it is: the body that would be stored, the version
// before it, and whether it only tightens that one (so needs no signature).
func CheckRule(ctx context.Context, db DB, id string, text []byte) (map[string]any, error) {
	r, err := ruleRequest(id, text)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	err = readOnly(ctx, db, func(tx pgx.Tx) error {
		prev, lerr := latestRule(ctx, tx, id)
		if lerr != nil {
			return lerr
		}
		why, ferr := follows(prev, r)
		if ferr != nil {
			return ferr
		}
		out = map[string]any{"rule_id": r.ID, "rule_version": r.Version, "direction": r.Direction, "body": r.Doc(),
			"tightens": why == nil, "needs_signature": why != nil}
		out["proposed_landing_effects"] = landingBoundaryRows(r)
		if why != nil {
			out["why_not_tighter"] = why.Error()
		}
		if prev != nil {
			out["previous_version"] = prev.Version
		}
		changes, capacityErr := approvable(prev, r)
		out["capacity_approvable"] = capacityErr == nil
		if capacityErr == nil {
			out["capacity_changes"] = changes
		} else {
			out["why_not_capacity"] = capacityErr.Error()
		}
		// The browser presents this result; it never classifies authority.
		out["apply_via"] = "signature"
		if why == nil && r.Direction == rules.Tighten {
			out["apply_via"] = "tighten"
		} else if capacityErr == nil {
			out["apply_via"] = "capacity_approval"
		}
		return nil
	})
	return out, err
}

// readOnly runs f in a read-only transaction.
func readOnly(ctx context.Context, db DB, f func(pgx.Tx) error) error {
	tx, err := db.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	return f(tx)
}

// SignRule takes a rule the Owner signed (`ssh-keygen -Y sign -n
// bifrost-agentrt-rule`): the signature verifies against anchor (the
// compiled-in key) over the exact text, the text parses, its header is the
// next version, and a rule marked tighten really is one. The row, its event
// and the rows the rule writes (jobs, candidates, capability facts) go in one
// transaction.
func SignRule(ctx context.Context, db DB, w Writer, id string, text []byte, signature, anchor string) (map[string]any, error) {
	r, err := ruleRequest(id, text)
	if err != nil {
		return nil, err
	}
	if err := s5SigningOrder(ctx, r); err != nil {
		return nil, err
	}
	if err := releasepolicy.VerifyAnchored([]byte(signature), text, rules.Namespace, anchor); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsigned, err)
	}
	return writeRule(ctx, db, w, r, text, func(_ *StoredRule, why error) error {
		if r.Direction == rules.Tighten && why != nil {
			return invalid("the rule is marked tighten but %v", why)
		}
		return nil
	}, ruleRecord{signature: signature, signedBy: anchor})
}

// S5SigningReady reports whether the deployment's trusted S5 contract is
// usable: checker image built, materials pinned, profile the reviewed one.
// The server wires landingci's embedded trust root; unset, it refuses.
var S5SigningReady func(context.Context) error

// s5SigningOrder enforces the S5 design §9 order mechanically: checker and
// trusted CI first, the rule last. A landing rule whose allow enrolls an S5
// path, signed while the trust root is UNBUILT or unpinned, would route
// every platform TS/TSX change (the display lane included) to an S5 lane
// that can never produce evidence. Signing is the only way such an allow
// enters (an unsigned tightening grants nothing new), so it is checked here,
// before the signature, and nowhere a stored rule is read: an already
// stored rule keeps failing closed per operation, never unreadable.
func s5SigningOrder(ctx context.Context, r rules.Rule) error {
	paths := rules.S5Enrolled(r)
	if len(paths) == 0 {
		return nil
	}
	ready := S5SigningReady
	if ready == nil {
		return invalid("rule %s enrolls S5 paths %v but no trusted S5 contract is wired", r.ID, paths)
	}
	if err := ready(ctx); err != nil {
		return invalid("rule %s enrolls S5 paths %v before the trusted S5 contract is deployable: %v", r.ID, paths, err)
	}
	return nil
}

// TightenRule takes a rule without a signature: only a version that the
// comparison with the one before shows grants nothing more, and that says so
// (direction tighten). Anything else needs the Owner's signature.
func TightenRule(ctx context.Context, db DB, w Writer, id string, text []byte) (map[string]any, error) {
	r, err := ruleRequest(id, text)
	if err != nil {
		return nil, err
	}
	if r.Direction != rules.Tighten {
		return nil, fmt.Errorf("%w: a rule marked %s is never taken unsigned", ErrUnsigned, r.Direction)
	}
	return writeRule(ctx, db, w, r, text, func(_ *StoredRule, why error) error {
		if why != nil {
			return fmt.Errorf("%w: it is not a tightening: %v", ErrUnsigned, why)
		}
		return nil
	}, ruleRecord{})
}

// ruleRecord is what lets a rule version take effect besides a shown
// tightening: the Owner's signature and the key it verified against, or the
// approval of a card for a capacity change (ruleapprove.go), with what the
// card showed.
type ruleRecord struct {
	signature, signedBy string
	approval            *ruleApproval
}

// writeRule writes r as the next version of its rule once allow, given the
// version before and whether r only tightens it, lets it. For a card's
// version (rec.approval) it asks first, under the same locks, whether that
// approval already wrote it (its executor retried) and answers what it wrote.
func writeRule(ctx context.Context, db DB, w Writer, r rules.Rule, text []byte, allow func(prev *StoredRule, why error) error,
	rec ruleRecord) (map[string]any, error) {
	var out map[string]any
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		if err := ensureWriter(ctx, tx, w, false); err != nil {
			return err
		}
		hook("rule locked", r.ID)
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, hashtext($2))`, ruleLock, r.ID); err != nil {
			return err
		}
		if rec.approval != nil {
			done, err := rec.approval.done(ctx, tx, r)
			if err != nil || done != nil {
				out = done
				return err
			}
		}
		prev, err := latestRule(ctx, tx, r.ID)
		if err != nil {
			return err
		}
		why, err := follows(prev, r)
		if err != nil {
			return err
		}
		if err = allow(prev, why); err != nil {
			return err
		}
		out, err = insertRule(ctx, tx, w, r, text, rec)
		return err
	})
	return out, err
}

func insertRule(ctx context.Context, tx pgx.Tx, w Writer, r rules.Rule, text []byte, rec ruleRecord) (map[string]any, error) {
	body, err := r.BodyJSON()
	if err != nil {
		return nil, err
	}
	signature, signedBy := rec.signature, rec.signedBy
	signed := signature != ""
	eventType := "rule_signed"
	switch {
	case rec.approval != nil:
		eventType = "rule_approved"
	case !signed:
		eventType = "rule_tightened"
	}
	evBody := map[string]any{"rule_id": r.ID, "rule_version": r.Version}
	var approvalID *string
	if a := rec.approval; a != nil {
		evBody["approval_id"], evBody["text_artifact_id"], evBody["changes"] = a.ID, a.TextArtifactID, capacityDoc(a.changes)
		approvalID = &a.ID
	}
	ev, err := Append(ctx, tx, Event{Stream: RuleStream(r.ID), Type: eventType, Actor: w.Actor, Body: evBody})
	if err != nil {
		return nil, err
	}
	if approvalID == nil {
		_, err = tx.Exec(ctx, `INSERT INTO agentrt.rule (rule_id, rule_version, body_json, body_text, direction, signed_by, signature,
		    effective_at, expires_at, proposed_by_actor_id) VALUES ($1, $2, $3::jsonb, $4, $5, $6, $7, $8, $9, $10)`,
			r.ID, r.Version, string(body), string(text), r.Direction, nullable(signedBy), nullable(signature), r.Effective, r.Expires, w.Actor)
	} else {
		// Only a card's version names approval_id (0013): the signed and
		// tighten routes write what they wrote before.
		_, err = tx.Exec(ctx, `INSERT INTO agentrt.rule (rule_id, rule_version, body_json, body_text, direction,
		    effective_at, expires_at, proposed_by_actor_id, approval_id) VALUES ($1, $2, $3::jsonb, $4, $5, $6, $7, $8, $9)`,
			r.ID, r.Version, string(body), string(text), r.Direction, r.Effective, r.Expires, w.Actor, *approvalID)
	}
	if err != nil {
		return nil, err
	}
	var doc any
	if err = json.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	out := map[string]any{"rule_id": r.ID, "rule_version": r.Version, "direction": r.Direction, "signed": signed,
		"effective_at": r.Effective.Format(time.RFC3339), "expires_at": r.Expires.Format(time.RFC3339), "event_id": ev.EventID,
		"body": doc}
	if a := rec.approval; a != nil {
		out["approval_id"], out["changes"] = a.ID, capacityDoc(a.changes)
	}
	return out, nil
}

// RuleInForce is CurrentRule at the database's clock, in a read-only
// transaction of its own.
func RuleInForce(ctx context.Context, db DB, id string) (*StoredRule, error) {
	if rules.Retired(id) {
		return nil, invalid("rule %s is of a kind retired with the dispatch lane; its signed versions stay in the record and nothing reads them", id)
	}
	var out *StoredRule
	err := readOnly(ctx, db, func(tx pgx.Tx) error {
		var now time.Time
		if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			return err
		}
		var err error
		out, err = CurrentRule(ctx, tx, id, now)
		return err
	})
	return out, err
}

// ProposeRule records a rule a seat or a thread drafts for the Owner
// (rule_proposed): the text is kept as an artifact and nothing takes effect.
// It says whether the text would need his signature.
func ProposeRule(ctx context.Context, db DB, w Writer, key, id string, text []byte, reason string) (map[string]any, error) {
	if key == "" {
		return nil, invalid("an idempotency key is required")
	}
	if strings.TrimSpace(reason) == "" {
		return nil, invalid("a reason is required")
	}
	r, err := ruleRequest(id, text)
	if err != nil {
		return nil, err
	}
	request := map[string]any{"rule_id": id, "text": string(text), "reason": reason}
	return idempotentAs(ctx, db, w, "rules/proposals", key, request, nil, func(tx pgx.Tx) (map[string]any, error) {
		if err := ensureWriter(ctx, tx, w, true); err != nil {
			return nil, err
		}
		prev, err := latestRule(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		why, err := follows(prev, r)
		if err != nil {
			return nil, err
		}
		art, err := PutArtifact(ctx, tx, w.Actor, "other", "application/yaml", text)
		if err != nil {
			return nil, err
		}
		body := map[string]any{"rule_id": id, "rule_version": r.Version, "direction": r.Direction, "text_artifact_id": art, "reason": reason}
		if w.Token != "" {
			body["token"] = w.Token
		}
		ev, err := Append(ctx, tx, Event{Stream: RuleStream(id), Type: "rule_proposed", Actor: w.Actor, Body: body})
		if err != nil {
			return nil, err
		}
		return map[string]any{"rule_id": id, "rule_version": r.Version, "text_artifact_id": art, "event_id": ev.EventID,
			"needs_signature": why != nil}, nil
	})
}
