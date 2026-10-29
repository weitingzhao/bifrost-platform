package store

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// A grant's CI evidence (coordinator v3 §3.5, review 694 B-01). An
// operation that needs a grant runs CI pinned to the sibling commits fixed
// at its first kick; the key is recorded with that kick (landing_ci_key on
// the ci_kick record) and every grant read — material, request, executor,
// final gate — reads that run strictly (landingci.Client.GrantCI): the
// newest run for R, its pipeline and its results all as the key says.

// GrantCIKey is a prepared candidate's CI key: the repository, R, M0 and
// the sibling repositories' commits it is built with.
type GrantCIKey struct {
	Repo     string            `json:"repo"`
	Revision string            `json:"revision"`
	Base     string            `json:"base"`
	Siblings map[string]string `json:"siblings"`
}

// GrantCIEvidence is the strict read of a key's newest run. Status is
// Succeeded only when the run is R's newest, its pipeline the repository's
// own (Pipeline) and every result as the key says; Reason says otherwise.
type GrantCIEvidence struct {
	Pipeline, Run, UID string
	CreatedAt          time.Time
	Status, Reason     string
	Candidate          string
	Siblings           map[string]string
}

// grantSiblings are the sibling checkouts of a repository's CI (landingci's
// repository table; S6 adds infra's).
var grantSiblings = map[string][]string{"bifrost-platform": {"bifrost-trade-infra", "bifrost-ui"}}

func (k GrantCIKey) valid() bool {
	want, ok := grantSiblings[k.Repo]
	if !ok || !grantSHA.MatchString(k.Revision) || !grantSHA.MatchString(k.Base) || len(k.Siblings) != len(want) {
		return false
	}
	for _, s := range want {
		if !grantSHA.MatchString(k.Siblings[s]) {
			return false
		}
	}
	return true
}

func (k GrantCIKey) equal(o GrantCIKey) bool {
	return k.Repo == o.Repo && k.Revision == o.Revision && k.Base == o.Base && maps.Equal(k.Siblings, o.Siblings)
}

type landingCIKeyRecord struct {
	V  string `json:"v"`
	Op string `json:"op"`
	GrantCIKey
}

func parseLandingCIKey(v any) (landingCIKeyRecord, error) {
	var r landingCIKeyRecord
	if err := closedDecode(v, &r, "v", "op", "repo", "revision", "base", "siblings"); err != nil {
		return r, fmt.Errorf("landing_ci_key: %w", err)
	}
	if r.V != LandingGrantVersion || grantOp.FindStringSubmatch(r.Op) == nil || !r.GrantCIKey.valid() {
		return r, fmt.Errorf("landing_ci_key: invalid")
	}
	return r, nil
}

// checkLandingCIKeyAppend: the key rides only on the controller's ci_kick
// or ci_rerun of its operation, on the repository stream, with that
// operation's R and M0; once recorded it never changes.
func checkLandingCIKeyAppend(ctx context.Context, tx pgx.Tx, ev Event) error {
	v, has := ev.Body["landing_ci_key"]
	if !has {
		return nil
	}
	k, err := parseLandingCIKey(v)
	if err != nil {
		return invalid("%v", err)
	}
	r, _ := ev.Body["landing"].(map[string]any)
	if ev.Type != "note" || ev.Actor != "controller" || ev.Stream != landingRepoStream || r == nil || r["op"] != k.Op ||
		(r["kind"] != "ci_kick" && r["kind"] != "ci_rerun") {
		return invalid("landing_ci_key rides on the controller's ci_kick or ci_rerun of its operation")
	}
	s, err := LandingState(ctx, tx, ev.Stream)
	if err != nil {
		return err
	}
	o := s.Operations[k.Op]
	if o.DryRun || o.R != k.Revision || o.M0 != k.Base || k.Repo != "bifrost-platform" {
		return invalid("landing_ci_key is its operation's R and M0")
	}
	prior, err := landingCIKeyOf(ctx, tx, k.Op)
	if err != nil {
		return err
	}
	if prior != nil && !prior.equal(k.GrantCIKey) {
		return invalid("landing_ci_key of %s is fixed at its first kick", k.Op)
	}
	// The first key may ride on a rerun: the first run was the webhook's or
	// v1's, not pinned (review 698 B-01); from then on it never changes.
	if prior == nil && r["kind"] == "ci_rerun" && o.Rerun {
		return invalid("landing_ci_key is fixed at the operation's first pinned run")
	}
	return nil
}

// landingCIKeyOf is an operation's recorded key, nil when its CI is not
// pinned.
func landingCIKeyOf(ctx context.Context, tx pgx.Tx, op string) (*GrantCIKey, error) {
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT body_json->'landing_ci_key' FROM agentrt.event WHERE event_stream_id=$1 AND event_type='note'
	  AND body_json->'landing_ci_key'->>'op'=$2 ORDER BY seq LIMIT 1`, landingRepoStream, op).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	v, err := canon.Parse(raw)
	if err != nil {
		return nil, err
	}
	k, err := parseLandingCIKey(v)
	if err != nil {
		return nil, err
	}
	return &k.GrantCIKey, nil
}

// LandingCIKey is landingCIKeyOf for the controller (outside a gate).
func LandingCIKey(ctx context.Context, db DB, op string) (*GrantCIKey, error) {
	var k *GrantCIKey
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		var e error
		k, e = landingCIKeyOf(ctx, tx, op)
		return e
	})
	return k, err
}

// AppendCIKick records ci_kick (or ci_rerun) with its pinned key, in one
// event, before the pinned run is created.
func AppendCIKick(ctx context.Context, db DB, op, kind string, try int, key GrantCIKey, text string) error {
	if kind != "ci_kick" && kind != "ci_rerun" {
		return invalid("a pinned key rides on ci_kick or ci_rerun")
	}
	return InTx(ctx, db, func(tx pgx.Tx) error {
		_, err := Append(ctx, tx, Event{Stream: landingRepoStream, Type: "note", Actor: "controller", Body: map[string]any{"text": text,
			"landing":        map[string]any{"v": landing.Version, "kind": kind, "op": op, "try": try},
			"landing_ci_key": toBody(landingCIKeyRecord{V: LandingGrantVersion, Op: op, GrantCIKey: key})}})
		return err
	})
}

// grantCIProblem: the strict read is the success a grant may rest on: R's
// newest run, its own pipeline, results as the key, after preparation.
func grantCIProblem(o landing.Operation, key *GrantCIKey, ci GrantCIEvidence, now time.Time) bool {
	return key == nil || ci.Status != "Succeeded" || ci.Reason != "" || ci.Pipeline == "" || ci.Run == "" || ci.UID == "" ||
		ci.Candidate != o.R || !maps.Equal(ci.Siblings, key.Siblings) || !ci.CreatedAt.After(o.PreparedAt) || ci.CreatedAt.After(now)
}

// grantCIRead reads a key's evidence, empty when there is no key.
func grantCIRead(ctx context.Context, src LandingSource, key *GrantCIKey, o landing.Operation) (GrantCIEvidence, error) {
	if key == nil {
		return GrantCIEvidence{}, nil
	}
	if src.GrantCI == nil {
		return GrantCIEvidence{}, errors.New("landing grant CI reader unavailable in this process")
	}
	return src.GrantCI(ctx, *key, o.PreparedAt)
}

// LandingRuleClear rides on the main_intent of a waiting operation that the
// rule now in force no longer fails (review 694 R-06): its authority is that
// rule, as the final gate decided it.
type LandingRuleClear struct {
	V             string    `json:"v"`
	Op            string    `json:"op"`
	Rule          GrantRule `json:"rule"`
	ChecksVersion int64     `json:"checks_version"`
	DecidedAt     string    `json:"decided_at"`
}

func parseLandingRuleClear(v any) (LandingRuleClear, error) {
	var c LandingRuleClear
	if err := closedDecode(v, &c, "v", "op", "rule", "checks_version", "decided_at"); err != nil {
		return c, fmt.Errorf("landing_rule_clear: %w", err)
	}
	var rule map[string]any
	if m, ok := v.(map[string]any); ok {
		rule, _ = m["rule"].(map[string]any)
	}
	if err := closedDecode(rule, &GrantRule{}, "id", "version", "sha256"); err != nil {
		return c, fmt.Errorf("landing_rule_clear rule: %w", err)
	}
	if _, ok := grantTime(c.DecidedAt); !ok || c.V != LandingGrantVersion || grantOp.FindStringSubmatch(c.Op) == nil || !parseGrantRule(c.Rule) || c.ChecksVersion < 1 {
		return c, fmt.Errorf("landing_rule_clear: invalid")
	}
	return c, nil
}

// checkLandingRuleClearAppend: only on the controller's main_intent of a
// waiting operation, never with a grant's use.
func checkLandingRuleClearAppend(ctx context.Context, tx pgx.Tx, ev Event, at time.Time) error {
	v, has := ev.Body["landing_rule_clear"]
	if !has {
		return nil
	}
	c, err := parseLandingRuleClear(v)
	if err != nil {
		return invalid("%v", err)
	}
	r, _ := ev.Body["landing"].(map[string]any)
	_, use := ev.Body["landing_grant_use"]
	decided, _ := grantTime(c.DecidedAt)
	if ev.Type != "note" || ev.Actor != "controller" || ev.Stream != landingRepoStream || r == nil || r["kind"] != "main_intent" || r["op"] != c.Op || use || decided.After(at) {
		return invalid("landing_rule_clear rides on the controller's main_intent of its operation, alone")
	}
	s, err := LandingState(ctx, tx, ev.Stream)
	if err != nil {
		return err
	}
	if s.Operations[c.Op].Phase != "awaiting_grant" {
		return invalid("landing_rule_clear ends a waiting operation only")
	}
	// Review 698 N-03: the certificate is the final gate's own, on the rule
	// that exists: its landing_gate in the same event names the same rule
	// version and decision, the checks are this binary's, and the rule's
	// text is the one its sha256 says.
	gate, _ := ev.Body["landing_gate"].(map[string]any)
	if gate == nil || gate["decided_at"] != c.DecidedAt || fmt.Sprint(gate["rule_version"]) != fmt.Sprint(c.Rule.Version) ||
		c.ChecksVersion != rules.LandingChecksVersion {
		return invalid("landing_rule_clear rides with its final gate's landing_gate, on the same rule and decision")
	}
	var sum string
	err = tx.QueryRow(ctx, `SELECT encode(sha256(convert_to(body_text,'UTF8')),'hex') FROM agentrt.rule WHERE rule_id=$1 AND rule_version=$2`,
		c.Rule.ID, c.Rule.Version).Scan(&sum)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && sum != c.Rule.SHA256 {
		return invalid("landing_rule_clear names a rule that does not exist")
	}
	return err
}

// refuseGrant records the final gate's refusal of a grant for good, in the
// gate's transaction (review 694 R-05).
func refuseGrant(ctx context.Context, tx pgx.Tx, g *grantView, reason string, at time.Time, mission string) error {
	_, err := Append(ctx, tx, Event{Stream: landingRepoStream, Type: "note", Actor: "controller", TaskID: g.Task, MissionID: mission,
		Body: map[string]any{"text": "Landing grant " + g.GrantID + " refused at the final gate: " + reason + "; a new request is needed",
			"landing_grant_refused": toBody(LandingGrantRefused{V: LandingGrantVersion, GrantID: g.GrantID, Reason: reason,
				DecidedAt: at.UTC().Format(time.RFC3339Nano)})}})
	return err
}

// grantEnvelopeRow is the standing envelope in force as landingFactsDB
// reads it beside the task, all null when there is none.
type grantEnvelopeRow struct {
	version            *int
	direction, text    *string
	effective, expires *time.Time
	body               []byte
	signedBy           *string
}

// enables is grantEnabled on the row: signed, unexpired, naming the
// coordinator's external host for repo. An envelope whose text has no
// external_hosts is not parsed.
func (e grantEnvelopeRow) enables(tenant, repo string, now time.Time) bool {
	if e.version == nil || e.text == nil || e.direction == nil || e.effective == nil || e.expires == nil || !e.expires.After(now) ||
		!strings.Contains(*e.text, "external_hosts") {
		return false
	}
	r, err := readRule(rules.KindEnvelope+":"+tenant, *e.version, *e.direction, *e.effective, *e.expires, *e.text, e.body, e.signedBy)
	if err != nil || r == nil || r.SignedBy == "" || r.Envelope == nil {
		return false
	}
	h, ok := r.Envelope.Pinned(rules.PinCoordinator)
	return ok && slices.Contains(h.Repos, repo)
}

// LandingRuleVersion is the version of rule id in force at now, 0 for none.
func LandingRuleVersion(ctx context.Context, db DB, id string, now time.Time) (int, error) {
	version := 0
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		r, err := CurrentRule(ctx, tx, id, now)
		if r != nil {
			version = r.Version
		}
		if errors.Is(err, errBadRule) {
			return nil
		}
		return err
	})
	return version, err
}
