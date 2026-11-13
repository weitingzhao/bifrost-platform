package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// Landing grants (W-31 round 4, core block C-5; DESIGN-coordinator-landing-
// v3-2026-10-29.md §5.5–§5.9, §12). A grant is the Owner's D-tier approval of
// one waiting operation, written on the repository stream by the approval's
// executor alone. The final gate consumes it with main_intent, in the same
// event; nothing else turns a grant into a push. Three note keys carry the
// records; the landing protocol's Fold ignores them.

const (
	// ActionGuardrailLanding waives soft failures; it never waives core.
	ActionGuardrailLanding = "approve_guardrail_landing"
	// ActionCoreLanding is the only grant a hard_core change can consume
	// (§12.1); it also waives the soft failures of the same change.
	ActionCoreLanding = "approve_core_landing"
	// ActionFloorLanding waives floor_grant (and rule_paths) for exactly the
	// floor-grantable files it pins (rules.FloorEntry), each at its blob
	// sha256 at R; never-grantable paths and core stay refused. It is
	// infra's only grant; on platform it also names the pinned CI run.
	ActionFloorLanding = "approve_floor_landing"
	// LandingGrantPurpose is the only purpose of this version.
	LandingGrantPurpose = "landing_main"
	// LandingGrantTTL counts from the approval's decision (E2 A).
	LandingGrantTTL = 24 * time.Hour
	// LandingGrantVersion is the version of all three grant records.
	LandingGrantVersion = "1"
	landingRepoStream   = "landing:bifrost-platform"
)

// GrantActions are the D-tier actions whose approval writes a grant.
var GrantActions = []string{ActionGuardrailLanding, ActionCoreLanding, ActionFloorLanding}

// grantStream is a repository's landing stream, where its grants live.
func grantStream(repo string) string { return "landing:" + repo }

// GrantActionSupported: action's grant may be written for repo. The two
// platform grants stay platform's (rules.GrantSupported); the floor grant
// is every repository's whose floor-grantable paths take it
// (rules.FloorGrantSupported: infra and platform).
func GrantActionSupported(action, repo string) bool {
	switch action {
	case ActionFloorLanding:
		return rules.FloorGrantSupported(repo)
	case ActionGuardrailLanding, ActionCoreLanding:
		return rules.GrantSupported(repo)
	}
	return false
}

// grantRepoOf is the repository of a landing operation's task, from the
// database: an operation id names its task, never its repository.
func grantRepoOf(ctx context.Context, tx pgx.Tx, op string) (string, error) {
	m := grantOp.FindStringSubmatch(op)
	if m == nil {
		return "", invalid("not a landing operation: %s", op)
	}
	var repo string
	err := tx.QueryRow(ctx, `SELECT COALESCE(NULLIF(repo,''),'bifrost-platform') FROM agentrt.task WHERE task_id=$1`, m[1]).Scan(&repo)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", grantRefused("not_waiting", "no landing operation %s", op)
	}
	if err == nil && !landing.GrantSupported(repo) {
		return "", grantRefused("grant_unsupported", "repository %q takes no grant", repo)
	}
	return repo, err
}

// GrantFloorPath is one file a floor grant pins: its path, the floor entry
// it matched and the lowercase sha256 of its bytes at R.
type GrantFloorPath struct {
	Path       string `json:"path"`
	Entry      string `json:"entry"`
	BlobSHA256 string `json:"blob_sha256"`
}

// validFloorPaths: sorted by path, unique, each floor-grantable in repo
// with its own entry and a sha256; non-empty.
func validFloorPaths(repo string, paths []GrantFloorPath) bool {
	if len(paths) == 0 {
		return false
	}
	last := ""
	for _, p := range paths {
		if p.Path <= last || rules.FloorEntry(repo, p.Path) != p.Entry || p.Entry == "" || !grantHex64.MatchString(p.BlobSHA256) {
			return false
		}
		last = p.Path
	}
	return true
}

// floorWaives: a floor grant waives floor_grant, with rule_paths, and
// nothing else.
func floorWaives(set []string) bool {
	if !slices.Contains(set, string(rules.LandingFloorGrant)) {
		return false
	}
	for _, s := range set {
		if s != string(rules.LandingFloorGrant) && s != string(rules.LandingRulePaths) {
			return false
		}
	}
	return true
}

// ErrGrantUncertain: a grant or revocation was committed or not, and the
// read back could not tell. The stream decides; nothing is retried here.
var ErrGrantUncertain = errors.New("store: landing grant outcome unknown; the repository stream decides")

// GrantRule is the rule identity a grant or its use was decided under.
type GrantRule struct {
	ID      string `json:"id"`
	Version int64  `json:"version"`
	SHA256  string `json:"sha256"`
}

// GrantCI is the CI run a grant names (§5.5): R's latest run, as decided.
type GrantCI struct {
	Pipeline  string `json:"pipeline"`
	Run       string `json:"run"`
	UID       string `json:"uid"`
	CreatedAt string `json:"created_at"`
	Status    string `json:"status"`
	SHA       string `json:"sha"`
}

// LandingGrant is the closed landing_grant record (§5.5).
type LandingGrant struct {
	V               string    `json:"v"`
	GrantID         string    `json:"grant_id"`
	ApprovalID      string    `json:"approval_id"`
	ApprovalNumber  int64     `json:"approval_number"`
	Action          string    `json:"action"`
	ParamsHash      string    `json:"params_hash"`
	Home            string    `json:"home"`
	Env             string    `json:"env"`
	Tenant          string    `json:"tenant"`
	Repo            string    `json:"repo"`
	Task            string    `json:"task"`
	Attempt         string    `json:"attempt"`
	Op              string    `json:"op"`
	A               string    `json:"A"`
	Base            string    `json:"base"`
	R               string    `json:"R"`
	M0              string    `json:"M0"`
	CommitterDate   string    `json:"committer_date"`
	CandidateDigest string    `json:"candidate_digest"`
	MaterialDigest  string    `json:"material_digest"`
	Waives          []string  `json:"waives"`
	SoftAtRequest   []string  `json:"soft_at_request"`
	Files           int64     `json:"files"`
	Lines           int64     `json:"lines"`
	Rule            GrantRule `json:"rule"`
	ChecksVersion   int64     `json:"checks_version"`
	CI              GrantCI   `json:"ci"`
	Purpose         string    `json:"purpose"`
	STGRelease      bool      `json:"stg_release"`
	ApprovedAt      string    `json:"approved_at"`
	GrantedAt       string    `json:"granted_at"`
	ExpiresAt       string    `json:"expires_at"`
	// FloorPaths: a floor grant's pinned files; absent on the other grants.
	FloorPaths []GrantFloorPath `json:"floor_paths,omitempty"`
	// Batch: on a batch lead's grant, the batch its material covered.
	Batch *LandingGrantBatch `json:"batch,omitempty"`
}

// LandingGrantRevoked is the closed landing_grant_revoked record.
type LandingGrantRevoked struct {
	V       string `json:"v"`
	GrantID string `json:"grant_id"`
	By      string `json:"by"`
	At      string `json:"at"`
}

// LandingGrantUse rides on the main_intent that consumes the grant.
type LandingGrantUse struct {
	V               string    `json:"v"`
	GrantID         string    `json:"grant_id"`
	CandidateDigest string    `json:"candidate_digest"`
	SoftNow         []string  `json:"soft_now"`
	Rule            GrantRule `json:"rule"`
	ChecksVersion   int64     `json:"checks_version"`
	DecidedAt       string    `json:"decided_at"`
}

// LandingGrantRefused is the final gate's record that it refused a grant
// for good (review 694 R-05): the rule now in force fails more than it
// waives (grant_insufficient), or the candidate is no longer the one
// approved (grant_stale). The grant is spent: it pauses W no more, the
// status says a new request is needed, and a new approval may be granted.
type LandingGrantRefused struct {
	V         string `json:"v"`
	GrantID   string `json:"grant_id"`
	Reason    string `json:"reason"`
	DecidedAt string `json:"decided_at"`
}

// GrantRefusals are the gate's refusals that spend a grant. Transient ones
// (window, CI running, main unreadable) leave it usable.
var GrantRefusals = []string{"grant_insufficient", "grant_stale"}

var (
	grantDigest  = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	grantHex64   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	grantSHA     = regexp.MustCompile(`^[0-9a-f]{40}$`)
	grantAttempt = regexp.MustCompile(`^a_[0-9A-HJKMNP-TV-Z]{26}$`)
	grantOp      = regexp.MustCompile(`^land:(t_[0-9A-HJKMNP-TV-Z]{26}):[1-9][0-9]*$`)
	grantTenant  = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	grantRuleID  = regexp.MustCompile(`^landing:[a-z][a-z0-9_-]{0,31}$`)
	grantActor   = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_:./-]{0,199}$`)
)

// closedDecode decodes v into out after checking its keys are exactly keys,
// each present: an absent field is not a zero value (§5.5 "全部必填，闭合").
func closedDecode(v any, out any, keys ...string) error {
	raw, err := canon.Encode(v)
	if err != nil {
		return err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return err
	}
	if len(m) != len(keys) {
		return fmt.Errorf("has %d fields, needs exactly %v", len(m), keys)
	}
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			return fmt.Errorf("needs %s", k)
		}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(out)
}

// toBody is a record as a canonical event body value.
func toBody(v any) map[string]any {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	x, err := canon.Parse(raw)
	if err != nil {
		panic(err)
	}
	return x.(map[string]any)
}

func grantTime(s string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339Nano, s)
	return t, err == nil && !t.IsZero()
}

// validCategories: a sorted, duplicate-free set of known refusal categories.
// empty says whether the empty set is valid (soft_now may be empty).
func validCategories(set []string, empty bool) bool {
	if len(set) == 0 {
		return empty
	}
	// "size": grants recorded before the size gate was removed name it; they
	// stay readable (see landing.validSoft). No check produces it now.
	known := []string{string(rules.LandingCore), string(rules.LandingFloorGrant), string(rules.LandingFloorSoft), string(rules.LandingPresentation), string(rules.LandingRulePaths), "size"}
	last := ""
	for _, s := range set {
		if s <= last || !slices.Contains(known, s) {
			return false
		}
		last = s
	}
	return true
}

func refusalSet(set []rules.LandingRefusal) []string {
	out := make([]string, 0, len(set))
	for _, r := range set {
		out = append(out, string(r))
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func refusalsOf(set []string) []rules.LandingRefusal {
	out := make([]rules.LandingRefusal, 0, len(set))
	for _, s := range set {
		out = append(out, rules.LandingRefusal(s))
	}
	return out
}

// GrantActionFor is the one action that may waive repo's soft (§12.1): a
// core failure takes the core-level approval (on platform it waives a
// floor_grant beside core too), a floor_grant failure the floor one, and
// anything else the guardrail one. Infra has no core, so its floor_grant
// always takes the floor grant. The actions' waivable categories never
// overlap on core.
func GrantActionFor(repo string, soft []string) string {
	if slices.Contains(soft, string(rules.LandingCore)) {
		return ActionCoreLanding
	}
	if slices.Contains(soft, string(rules.LandingFloorGrant)) {
		return ActionFloorLanding
	}
	return ActionGuardrailLanding
}

func parseGrantRule(r GrantRule) bool {
	return grantRuleID.MatchString(r.ID) && r.Version >= 1 && grantHex64.MatchString(r.SHA256)
}

// parseLandingGrant checks a landing_grant record against §5.5 and §12.
func parseLandingGrant(v any) (LandingGrant, error) {
	var g LandingGrant
	keys := []string{"v", "grant_id", "approval_id", "approval_number", "action", "params_hash", "home", "env", "tenant", "repo",
		"task", "attempt", "op", "A", "base", "R", "M0", "committer_date", "candidate_digest", "material_digest", "waives",
		"soft_at_request", "files", "lines", "rule", "checks_version", "ci", "purpose", "stg_release", "approved_at", "granted_at", "expires_at"}
	// A floor grant carries floor_paths; no other grant does.
	if m, ok := v.(map[string]any); ok && m["action"] == ActionFloorLanding {
		keys = append(keys, "floor_paths")
	}
	keys = append(keys, grantBatchKeys(v)...)
	if err := closedDecode(v, &g, keys...); err != nil {
		return g, fmt.Errorf("landing_grant: %w", err)
	}
	if m, ok := v.(map[string]any); ok && g.Action == ActionFloorLanding {
		list, _ := m["floor_paths"].([]any)
		for _, item := range list {
			if err := closedDecode(item, &GrantFloorPath{}, "path", "entry", "blob_sha256"); err != nil {
				return g, fmt.Errorf("landing_grant floor_paths: %w", err)
			}
		}
	}
	var rule, ci map[string]any
	if m, ok := v.(map[string]any); ok {
		rule, _ = m["rule"].(map[string]any)
		ci, _ = m["ci"].(map[string]any)
	}
	if err := closedDecode(rule, &GrantRule{}, "id", "version", "sha256"); err != nil {
		return g, fmt.Errorf("landing_grant rule: %w", err)
	}
	if err := closedDecode(ci, &GrantCI{}, "pipeline", "run", "uid", "created_at", "status", "sha"); err != nil {
		return g, fmt.Errorf("landing_grant ci: %w", err)
	}
	approved, okA := grantTime(g.ApprovedAt)
	granted, okG := grantTime(g.GrantedAt)
	expires, okE := grantTime(g.ExpiresAt)
	_, okC := grantTime(g.CommitterDate)
	_, okCI := grantTime(g.CI.CreatedAt)
	m := grantOp.FindStringSubmatch(g.Op)
	switch {
	case g.V != LandingGrantVersion:
		return g, fmt.Errorf("landing_grant version is not %s", LandingGrantVersion)
	case !landingRequestID.MatchString(g.ApprovalID) || g.GrantID != "lg_"+g.ApprovalID || g.ApprovalNumber < 1:
		return g, fmt.Errorf("landing_grant identity: grant_id is lg_<approval_id> and the approval has a number")
	case !slices.Contains(GrantActions, g.Action) || !grantHex64.MatchString(g.ParamsHash):
		return g, fmt.Errorf("landing_grant action or params_hash")
	case g.Home != "prod" || g.Env != "prod" || !grantTenant.MatchString(g.Tenant) || !GrantActionSupported(g.Action, g.Repo):
		return g, fmt.Errorf("landing_grant is PROD only, for a repository that supports its action")
	case !landingRoot.MatchString(g.Task) || m == nil || m[1] != g.Task || !grantAttempt.MatchString(g.Attempt):
		return g, fmt.Errorf("landing_grant task, attempt or op")
	case !grantSHA.MatchString(g.A) || !grantSHA.MatchString(g.Base) || !grantSHA.MatchString(g.R) || !grantSHA.MatchString(g.M0):
		return g, fmt.Errorf("landing_grant commits")
	case !grantDigest.MatchString(g.CandidateDigest) || !grantDigest.MatchString(g.MaterialDigest):
		return g, fmt.Errorf("landing_grant digests")
	case !validCategories(g.SoftAtRequest, false) || !slices.Equal(g.Waives, g.SoftAtRequest):
		return g, fmt.Errorf("landing_grant waives exactly its soft_at_request, a non-empty set")
	case GrantActionFor(g.Repo, g.Waives) != g.Action || g.Action == ActionFloorLanding && !floorWaives(g.Waives):
		// §12.1: the two grants' categories never overlap on core; the floor
		// grant waives floor_grant and rule_paths alone.
		return g, fmt.Errorf("landing_grant: %s does not waive %v", g.Action, g.Waives)
	case (g.Action == ActionFloorLanding) != (g.FloorPaths != nil) || g.FloorPaths != nil && !validFloorPaths(g.Repo, g.FloorPaths):
		return g, fmt.Errorf("landing_grant: floor_paths are a floor grant's, sorted, each floor-grantable with its blob sha256")
	case g.Batch != nil && badGrantBatch(v, g) != "":
		return g, fmt.Errorf("landing_grant batch: %s", badGrantBatch(v, g))
	case g.Files < 0 || g.Lines < 0 || g.ChecksVersion < 1 || !parseGrantRule(g.Rule) || g.Rule.ID != "landing:"+g.Tenant:
		return g, fmt.Errorf("landing_grant counts, rule or checks_version")
	case g.CI.Pipeline == "" || !landingRequestID.MatchString(g.CI.Run) || g.CI.UID == "" || !okCI || g.CI.Status != "Succeeded" || g.CI.SHA != g.R:
		return g, fmt.Errorf("landing_grant ci is R's successful run")
	case g.Purpose != LandingGrantPurpose:
		return g, fmt.Errorf("landing_grant purpose is %s", LandingGrantPurpose)
	case !okA || !okG || !okE || !okC || !expires.Equal(approved.Add(LandingGrantTTL)) || granted.Before(approved) || !granted.Before(expires):
		return g, fmt.Errorf("landing_grant times: expires 24h after approval, granted between")
	}
	return g, nil
}

func parseLandingGrantRevoked(v any) (LandingGrantRevoked, error) {
	var r LandingGrantRevoked
	if err := closedDecode(v, &r, "v", "grant_id", "by", "at"); err != nil {
		return r, fmt.Errorf("landing_grant_revoked: %w", err)
	}
	if _, ok := grantTime(r.At); !ok || r.V != LandingGrantVersion || !landingRequestID.MatchString(r.GrantID) || !grantActor.MatchString(r.By) {
		return r, fmt.Errorf("landing_grant_revoked: invalid")
	}
	return r, nil
}

func parseLandingGrantUse(v any) (LandingGrantUse, error) {
	var u LandingGrantUse
	if err := closedDecode(v, &u, "v", "grant_id", "candidate_digest", "soft_now", "rule", "checks_version", "decided_at"); err != nil {
		return u, fmt.Errorf("landing_grant_use: %w", err)
	}
	var rule map[string]any
	if m, ok := v.(map[string]any); ok {
		rule, _ = m["rule"].(map[string]any)
	}
	if err := closedDecode(rule, &GrantRule{}, "id", "version", "sha256"); err != nil {
		return u, fmt.Errorf("landing_grant_use rule: %w", err)
	}
	if u.SoftNow == nil {
		u.SoftNow = []string{}
	}
	if _, ok := grantTime(u.DecidedAt); !ok || u.V != LandingGrantVersion || !landingRequestID.MatchString(u.GrantID) || !grantDigest.MatchString(u.CandidateDigest) ||
		!validCategories(u.SoftNow, true) || !parseGrantRule(u.Rule) || u.ChecksVersion < 1 {
		return u, fmt.Errorf("landing_grant_use: invalid")
	}
	return u, nil
}

func parseLandingGrantRefused(v any) (LandingGrantRefused, error) {
	var r LandingGrantRefused
	if err := closedDecode(v, &r, "v", "grant_id", "reason", "decided_at"); err != nil {
		return r, fmt.Errorf("landing_grant_refused: %w", err)
	}
	if _, ok := grantTime(r.DecidedAt); !ok || r.V != LandingGrantVersion || !landingRequestID.MatchString(r.GrantID) || !slices.Contains(GrantRefusals, r.Reason) {
		return r, fmt.Errorf("landing_grant_refused: invalid")
	}
	return r, nil
}

// badGrantRecord is checkBody's closed-schema check of the grant keys.
func badGrantRecord(name string, v any) string {
	var err error
	switch name {
	case "landing_grant":
		_, err = parseLandingGrant(v)
	case "landing_grant_revoked":
		_, err = parseLandingGrantRevoked(v)
	case "landing_grant_use":
		_, err = parseLandingGrantUse(v)
	case "landing_grant_refused":
		_, err = parseLandingGrantRefused(v)
	case "landing_ci_key":
		_, err = parseLandingCIKey(v)
	case "landing_rule_clear":
		_, err = parseLandingRuleClear(v)
	}
	if err != nil {
		return err.Error()
	}
	return ""
}

// grantView is one grant as the stream has it now.
type grantView struct {
	recoveryPeriods []landing.Interval
	LandingGrant
	Seq                    int64
	At                     time.Time
	RevokedAt              time.Time
	RevokedBy              string
	ConsumedSeq            int64
	ConsumedAt             time.Time
	ConsumedOp             string
	RefusedAt              time.Time
	RefusedWhy             string
	approved, granted, exp time.Time
}

// usableAt: not revoked, not consumed, before expiry (§5.5 可用 grant,
// the stream half; the gate adds the operation and digest checks).
func (g *grantView) usableAt(t time.Time) bool {
	return g.RevokedAt.IsZero() && g.ConsumedSeq == 0 && g.RefusedAt.IsZero() && t.Before(g.exp)
}

// period is when the grant was available: from its writing to the first of
// expiry, revocation and consumption (§5.3: a valid grant ends the wait).
func (g *grantView) period() landing.Interval {
	end := g.exp
	for _, t := range []time.Time{g.RevokedAt, g.ConsumedAt, g.RefusedAt} {
		if !t.IsZero() && t.Before(end) {
			end = t
		}
	}
	return landing.Interval{Start: g.granted, End: end}
}

// grantPeriods is the projection RootTiming reads (§5.3): per operation, the
// intervals a valid grant was available. It is fixed by the stream alone, so
// a restart folds the same W (T43).
func grantPeriods(order []*grantView) map[string][]landing.Interval {
	out := map[string][]landing.Interval{}
	for _, g := range order {
		out[g.Op] = append(out[g.Op], g.recoveryPeriods...)
		out[g.Op] = append(out[g.Op], g.period())
	}
	return out
}

// candidateGrant is the operation's grant the gate may consume, from the
// stream alone (§5.5): same op, R, M0, A and attempt, unrevoked, unconsumed,
// before expiry at now. The digest and the waiver are checked by the gate.
func candidateGrant(order []*grantView, op string, o landing.Operation, now time.Time) *grantView {
	var found *grantView
	for _, g := range order {
		if g.Op == op && g.R == o.R && g.M0 == o.M0 && g.A == o.A && g.Attempt == o.Attempt && g.Task == o.Task && g.usableAt(now) {
			found = g
		}
	}
	return found
}

// checkLandingGrantAppend enforces who writes the three grant records and
// what they must agree with, under the stream lock Append holds. No route
// writes a grant: only the approval executor's store call does (T30).
func checkLandingGrantAppend(ctx context.Context, tx pgx.Tx, ev Event, at time.Time) error {
	_, hasGrant := ev.Body["landing_grant"]
	_, hasRevoked := ev.Body["landing_grant_revoked"]
	useRaw, hasUse := ev.Body["landing_grant_use"]
	_, hasRefused := ev.Body["landing_grant_refused"]
	if !hasGrant && !hasRevoked && !hasUse && !hasRefused {
		return nil
	}
	if ev.Type != "note" || !strings.HasPrefix(ev.Stream, "landing:") || !landing.GrantSupported(strings.TrimPrefix(ev.Stream, "landing:")) {
		return invalid("landing grant records live on the repository stream")
	}
	_, hasLanding := ev.Body["landing"]
	n := 0
	for _, b := range []bool{hasGrant, hasRevoked, hasUse, hasRefused} {
		if b {
			n++
		}
	}
	if n != 1 {
		return invalid("one landing grant record per event")
	}
	grants, order, err := readLandingGrants(ctx, tx, ev.Stream)
	if err != nil {
		return err
	}
	state, err := LandingState(ctx, tx, ev.Stream)
	if err != nil {
		return err
	}
	switch {
	case hasGrant:
		g, err := parseLandingGrant(ev.Body["landing_grant"])
		if err != nil {
			return invalid("%v", err)
		}
		if ev.Actor != "controller" || hasLanding || ev.TaskID != g.Task {
			return invalid("a landing grant is written by the approval executor (controller), alone, for its task")
		}
		if ev.Stream != grantStream(g.Repo) {
			return invalid("a landing grant lives on its own repository's stream")
		}
		if _, exists := grants[g.GrantID]; exists {
			return invalid("approval %s already has its grant", g.ApprovalID)
		}
		granted, _ := grantTime(g.GrantedAt)
		if granted.After(at) {
			return invalid("landing grant granted in the future")
		}
		o, ok := state.Operations[g.Op]
		if !ok || state.LatestOperation(g.Task) != g.Op || o.Phase != "awaiting_grant" || o.Task != g.Task ||
			o.R != g.R || o.M0 != g.M0 || o.A != g.A || o.Attempt != g.Attempt || o.CommitterDate.UTC().Format(time.RFC3339Nano) != g.CommitterDate {
			return invalid("a landing grant needs its operation, latest and waiting, with the same R, M0, A and attempt")
		}
		for _, other := range order {
			if other.Op == g.Op && other.usableAt(at) {
				return invalid("operation %s already has a valid grant %s", g.Op, other.GrantID)
			}
		}
	case hasRevoked:
		r, err := parseLandingGrantRevoked(ev.Body["landing_grant_revoked"])
		if err != nil {
			return invalid("%v", err)
		}
		g := grants[r.GrantID]
		if ev.Actor != "owner" || r.By != "owner" || hasLanding || g == nil || ev.TaskID != g.Task {
			return invalid("only the Owner revokes an existing grant")
		}
		if !g.RevokedAt.IsZero() || g.ConsumedSeq != 0 {
			return invalid("grant %s is already revoked or handed to the push", r.GrantID)
		}
		if t, _ := grantTime(r.At); t.After(at) {
			return invalid("revocation dated in the future")
		}
	case hasRefused:
		r, err := parseLandingGrantRefused(ev.Body["landing_grant_refused"])
		if err != nil {
			return invalid("%v", err)
		}
		g := grants[r.GrantID]
		decided, _ := grantTime(r.DecidedAt)
		if ev.Actor != "controller" || hasLanding || g == nil || ev.TaskID != g.Task || decided.After(at) || !g.usableAt(decided) {
			return invalid("only the final gate refuses a grant still usable, alone, for its task")
		}
	case hasUse:
		u, err := parseLandingGrantUse(useRaw)
		if err != nil {
			return invalid("%v", err)
		}
		r, _ := ev.Body["landing"].(map[string]any)
		g := grants[u.GrantID]
		if ev.Actor != "controller" || r == nil || r["kind"] != "main_intent" || g == nil || r["op"] != g.Op {
			return invalid("landing_grant_use rides on the controller's main_intent of the grant's operation")
		}
		o := state.Operations[g.Op]
		decided, _ := grantTime(u.DecidedAt)
		switch {
		case o.Phase != "awaiting_grant" || o.R != g.R || o.M0 != g.M0 || o.A != g.A || o.Attempt != g.Attempt:
			return invalid("the grant's operation is not waiting on the same candidate")
		case !g.RevokedAt.IsZero() || g.ConsumedSeq != 0 || !g.RefusedAt.IsZero():
			return invalid("grant %s is revoked, refused or already used", g.GrantID)
		case decided.After(at) || !decided.Before(g.exp):
			return invalid("grant %s expired before its use", g.GrantID)
		case u.CandidateDigest != g.CandidateDigest:
			return invalid("the grant's candidate differs from the one pushed")
		case !rules.LandingWaivesCover(refusalsOf(u.SoftNow), refusalsOf(g.Waives)):
			return invalid("the grant does not waive %v", u.SoftNow)
		}
	}
	return nil
}

// UsableLandingGrant tells the controller, from the database alone, which
// grant of a waiting operation is worth taking to the final gate ("" for
// none): usable on the stream, and grants still enabled (§2.1).
func UsableLandingGrant(ctx context.Context, db DB, op string) (string, error) {
	usable := ""
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		repo, err := grantRepoOf(ctx, tx, op)
		if err != nil {
			if errors.Is(err, ErrInvalid) {
				return nil
			}
			return err
		}
		s, err := LandingState(ctx, tx, grantStream(repo))
		if err != nil {
			return err
		}
		o, ok := s.Operations[op]
		if !ok || o.Phase != "awaiting_grant" || s.LatestOperation(o.Task) != op {
			return nil
		}
		_, order, err := readLandingGrants(ctx, tx, grantStream(repo))
		if err != nil {
			return err
		}
		g := candidateGrant(order, op, o, DecisionAt(ctx))
		if g == nil {
			return nil
		}
		enabled, err := grantEnabled(ctx, tx, g.Tenant, g.Repo)
		if enabled {
			usable = g.GrantID
		}
		return err
	})
	return usable, err
}

// HasUsableLandingGrant is UsableLandingGrant's yes or no.
func HasUsableLandingGrant(ctx context.Context, db DB, op string) (bool, error) {
	g, err := UsableLandingGrant(ctx, db, op)
	return g != "", err
}

// RevokeResult is what a revocation did (§5.7).
type RevokeResult struct {
	GrantID   string `json:"grant_id"`
	Op        string `json:"op"`
	Revoked   bool   `json:"revoked"`
	Already   bool   `json:"already"`
	Consumed  bool   `json:"handed_to_push"`
	IntentSeq int64  `json:"main_intent_seq,omitempty"`
	Message   string `json:"message"`
}

// RevokeLandingGrant revokes an unconsumed grant (F2 A), under the stream
// lock: a grant the final gate has consumed is not revoked, and the answer
// says it was handed to the push. A transport error on COMMIT is read back
// in a fresh transaction behind the same lock.
func RevokeLandingGrant(ctx context.Context, db DB, grantID string) (RevokeResult, error) {
	out := RevokeResult{GrantID: grantID}
	if !landingRequestID.MatchString(grantID) {
		return out, invalid("not a grant id")
	}
	// read takes its own context: the read back runs on a fresh one, never
	// the request's (review 694 R-01).
	stream := ""
	read := func(ctx context.Context, tx pgx.Tx) (*grantView, error) {
		if stream == "" {
			// The grant's repository stream, found before its lock is taken:
			// a grant never moves between streams.
			for _, repo := range rules.LandingRepoNames() {
				if !landing.GrantSupported(repo) {
					continue
				}
				grants, _, err := readLandingGrants(ctx, tx, grantStream(repo))
				if err != nil {
					return nil, err
				}
				if grants[grantID] != nil {
					stream = grantStream(repo)
					break
				}
			}
			if stream == "" {
				return nil, invalid("no grant %s", grantID)
			}
		}
		if err := lockStream(ctx, tx, stream); err != nil {
			return nil, err
		}
		grants, _, err := readLandingGrants(ctx, tx, stream)
		if err != nil {
			return nil, err
		}
		g := grants[grantID]
		if g == nil {
			return nil, invalid("no grant %s", grantID)
		}
		return g, nil
	}
	settle := func(g *grantView) RevokeResult {
		r := RevokeResult{GrantID: grantID, Op: g.Op}
		switch {
		case g.ConsumedSeq != 0:
			r.Consumed, r.IntentSeq = true, g.ConsumedSeq
			r.Message = fmt.Sprintf("handed to the push (op %s, main_intent seq %d): revocation cannot be guaranteed; after it lands it is reconciled as usual, and taking it back needs a revert", g.Op, g.ConsumedSeq)
		case !g.RevokedAt.IsZero():
			r.Revoked, r.Already = true, true
			r.Message = "already revoked"
		}
		return r
	}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	g, err := read(ctx, tx)
	if err != nil {
		return out, err
	}
	if r := settle(g); r.Consumed || r.Revoked {
		return r, nil
	}
	if _, err = Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "owner", TaskID: g.Task,
		Body: map[string]any{"text": "Landing grant revoked by the Owner", "landing_grant_revoked": toBody(LandingGrantRevoked{
			V: LandingGrantVersion, GrantID: grantID, By: "owner", At: DecisionAt(ctx).UTC().Format(time.RFC3339Nano)})}}); err != nil {
		return out, err
	}
	hook("landing grant: revoke before commit", grantID)
	commitErr := tx.Commit(ctx)
	if commitErr == nil {
		return RevokeResult{GrantID: grantID, Op: g.Op, Revoked: true, Message: "revoked"}, nil
	}
	// Read back (§5.7 table): revoked → revoked; consumed → handed to the
	// push; neither → not written, the caller may retry; unreadable → 503.
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	var back *grantView
	rerr := InTx(rctx, db, func(tx pgx.Tx) error {
		var e error
		back, e = read(rctx, tx)
		return e
	})
	if rerr != nil {
		return out, fmt.Errorf("%w: commit: %v; readback: %v", ErrGrantUncertain, commitErr, rerr)
	}
	if !back.RevokedAt.IsZero() {
		return RevokeResult{GrantID: grantID, Op: back.Op, Revoked: true, Message: "revoked"}, nil
	}
	if back.ConsumedSeq != 0 {
		return settle(back), nil
	}
	return out, fmt.Errorf("revocation not written (read back): %w", commitErr)
}
