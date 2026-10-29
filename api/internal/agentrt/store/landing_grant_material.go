package store

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// A grant's material (coordinator v3 §5.6 item 3, §12.2): what the Owner is
// shown, and its two digests. candidate_digest covers only what is fixed for
// the operation and owes nothing to the rules (the gate compares it at
// consumption); material_digest adds the rule identity and the soft set at
// request (the request and the executor compare it, so the Owner never
// approves a soft set that has since changed).

// GiteaWebBase is where the Owner opens Gitea itself (its NodePort,
// AGENT_FACTS §8c): the material's independent check (§12.2 item 4).
var GiteaWebBase = "http://192.168.10.73:30300"

// GrantPatch is the Git half (landinggit.CandidatePatch).
type GrantPatch struct {
	SHA256    string
	Shown     string
	Truncated bool
	Size      int
	Match     bool
}

// GrantSources are the external reads of a grant's material, made outside
// every transaction except the final gate's (which reads Git under its
// locks today, §5.9).
// Facts carries the Git and CI evidence readers (LandingSource.GrantPatch,
// GrantCI): the final gate reads the same ones.
type GrantSources struct {
	Facts LandingSource
	Main  func(ctx context.Context) (string, error)
}

func (s GrantSources) complete() bool {
	return s.Facts.Result != nil && s.Facts.History != nil && s.Facts.GrantPatch != nil && s.Facts.GrantCI != nil && s.Main != nil
}

// GrantParams are the approval's parameters, closed (§5.6 item 1).
type GrantParams struct {
	Env            string `json:"env"`
	Repo           string `json:"repo"`
	Task           string `json:"task"`
	Op             string `json:"op"`
	R              string `json:"R"`
	MaterialDigest string `json:"material_digest"`
	STGRelease     bool   `json:"stg_release"`
}

// DecodeGrantParams reads params as exactly the seven keys, each present.
func DecodeGrantParams(params map[string]any) (GrantParams, error) {
	var p GrantParams
	if err := closedDecode(params, &p, "env", "repo", "task", "op", "R", "material_digest", "stg_release"); err != nil {
		return p, invalid("grant parameters: %v", err)
	}
	m := grantOp.FindStringSubmatch(p.Op)
	switch {
	case p.Env != "prod":
		return p, invalid("grant parameters: env is prod (a grant is PROD only)")
	case !rules.GrantSupported(p.Repo):
		return p, grantRefused("grant_unsupported", "repository %q takes no grant", p.Repo)
	case !landingRoot.MatchString(p.Task) || m == nil || m[1] != p.Task:
		return p, invalid("grant parameters: op is land:<task>:<n> of task")
	case !grantSHA.MatchString(p.R) || !grantDigest.MatchString(p.MaterialDigest):
		return p, invalid("grant parameters: R and material_digest")
	}
	return p, nil
}

// grantRefused is a refusal with its code (the create check and executor
// answer with it; C-6 shows it).
func grantRefused(code, format string, args ...any) error {
	return fmt.Errorf("%w: %s: %s", ErrInvalid, code, fmt.Sprintf(format, args...))
}

type GrantChange struct {
	Old     string `json:"old"`
	New     string `json:"new"`
	OldMode string `json:"old_mode"`
	NewMode string `json:"new_mode"`
	Added   int    `json:"added"`
	Deleted int    `json:"deleted"`
}

type GrantAcceptance struct {
	VerificationID string `json:"verification_id"`
	Result         string `json:"result"`
}

type GrantReview struct {
	Task       string `json:"task"`
	Attempt    string `json:"attempt"`
	Executor   string `json:"executor"`
	Provider   string `json:"provider"`
	Conclusion string `json:"conclusion"`
	Artifact   string `json:"artifact"`
}

type GrantHardPath struct {
	Path  string `json:"path"`
	Entry string `json:"entry"`
}

// GrantCandidateCI is the strict CI evidence in candidate_digest: the
// verified pipeline, the run, and the key's siblings.
type GrantCandidateCI struct {
	Pipeline  string            `json:"pipeline"`
	Run       string            `json:"run"`
	UID       string            `json:"uid"`
	CreatedAt string            `json:"created_at"`
	Status    string            `json:"status"`
	SHA       string            `json:"sha"`
	Siblings  map[string]string `json:"siblings"`
}

// GrantCandidate is every input of candidate_digest (§5.6 item 3, §12.2
// item 2). The core-level fields are always present: empty for a guardrail
// change.
type GrantCandidate struct {
	Env              string            `json:"env"`
	Tenant           string            `json:"tenant"`
	Repo             string            `json:"repo"`
	Mission          string            `json:"mission"`
	PlanStep         string            `json:"plan_step"`
	Task             string            `json:"task"`
	Attempt          string            `json:"attempt"`
	Op               string            `json:"op"`
	A                string            `json:"A"`
	Base             string            `json:"base"`
	R                string            `json:"R"`
	M0               string            `json:"M0"`
	CommitterDate    string            `json:"committer_date"`
	Changes          []GrantChange     `json:"changes"`
	PatchSHA256      string            `json:"patch_sha256"`
	Files            int               `json:"files"`
	Lines            int               `json:"lines"`
	Acceptance       []GrantAcceptance `json:"acceptance"`
	Review           GrantReview       `json:"review"`
	ReviewPatchMatch bool              `json:"review_patch_match"`
	CI               GrantCandidateCI  `json:"ci"`
	STGRelease       bool              `json:"stg_release"`
	HardCorePaths    []GrantHardPath   `json:"hard_core_paths"`
	DecisionChain    bool              `json:"decision_chain"`
	TrustReview      bool              `json:"trust_review"`
}

func sumOf(v any) (string, error) {
	sum, err := canon.Sum(toBody(v))
	if err != nil {
		return "", err
	}
	return "sha256:" + sum, nil
}

// Digest is candidate_digest.
func (c GrantCandidate) Digest() (string, error) {
	return sumOf(struct {
		V string `json:"v"`
		GrantCandidate
	}{LandingGrantVersion, c})
}

// materialDigest = Sum({v, candidate_digest, soft_at_request, rule, checks_version}).
func materialDigest(candidate string, soft []string, rule GrantRule, checks int64) (string, error) {
	return sumOf(map[string]any{"v": LandingGrantVersion, "candidate_digest": candidate, "soft_at_request": soft, "rule": rule, "checks_version": checks})
}

// LandingGrantMaterial is the material route's answer and what the create
// check and the executor compare (§5.6 items 2–4). Problems lists every
// reason a request would be refused now; empty means a request may be made.
type LandingGrantMaterial struct {
	Candidate       GrantCandidate       `json:"candidate"`
	CandidateDigest string               `json:"candidate_digest"`
	MaterialDigest  string               `json:"material_digest"`
	SoftAtRequest   []string             `json:"soft_at_request"`
	WaitingSoft     []string             `json:"waiting_soft"`
	Rule            GrantRule            `json:"rule"`
	ChecksVersion   int64                `json:"checks_version"`
	Action          string               `json:"action"`
	Title           string               `json:"title"`
	FirstLine       string               `json:"first_line,omitempty"`
	Phase           string               `json:"phase"`
	Latest          bool                 `json:"latest"`
	Pinned          bool                 `json:"pinned"`
	GrantEnabled    bool                 `json:"grant_enabled"`
	Patch           string               `json:"patch,omitempty"`
	PatchTruncated  bool                 `json:"patch_truncated"`
	PatchSize       int                  `json:"patch_size"`
	CompareURL      string               `json:"compare_url"`
	GiteaNotice     string               `json:"gitea_notice"`
	QuotaReserved   int                  `json:"quota_reserved_today"`
	QuotaMax        int                  `json:"quota_max_per_day"`
	QuotaNote       string               `json:"quota_note"`
	STGNotice       string               `json:"stg_notice,omitempty"`
	Grants          []LandingGrantStatus `json:"grants"`
	Problems        []string             `json:"problems"`
	ComputedAt      time.Time            `json:"computed_at"`
}

// grantDB is the database half of the material, read in one transaction.
type grantDB struct {
	facts      landing.Facts
	opID, step string
	op         landing.Operation
	latest     bool
	acceptance []GrantAcceptance
	review     GrantReview
	reviewBody []byte
	rule       GrantRule
	enabled    bool
	grants     []*grantView
	ciKey      *GrantCIKey
}

// readGrantDB reads every database input of an operation's material. It
// opens no network; facts.Changes is left to the external half.
func readGrantDB(ctx context.Context, tx pgx.Tx, src LandingSource, op string, now time.Time) (grantDB, error) {
	var d grantDB
	d.opID = op
	s, err := LandingState(ctx, tx, landingRepoStream)
	if err != nil {
		return d, err
	}
	o, ok := s.Operations[op]
	if !ok || o.Task == "" {
		return d, grantRefused("not_waiting", "no landing operation %s", op)
	}
	d.op, d.latest = o, s.LatestOperation(o.Task) == op
	if d.facts, _, err = landingFactsDB(ctx, tx, src, "prod", "live", o.Task, op, now); err != nil {
		return d, err
	}
	if err = tx.QueryRow(ctx, `SELECT COALESCE(plan_node_id,'') FROM agentrt.task WHERE task_id=$1`, o.Task).Scan(&d.step); err != nil {
		return d, err
	}
	if d.acceptance, d.review, d.reviewBody, err = readGrantChecks(ctx, tx, o.Task, o.Attempt); err != nil {
		return d, err
	}
	if r := d.facts.Rule; r != nil {
		d.rule = GrantRule{ID: r.ID, Version: int64(r.Version)}
		if err = tx.QueryRow(ctx, `SELECT encode(sha256(convert_to(body_text,'UTF8')),'hex') FROM agentrt.rule WHERE rule_id=$1 AND rule_version=$2`,
			r.ID, r.Version).Scan(&d.rule.SHA256); err != nil {
			return d, err
		}
	}
	d.enabled = d.facts.GrantEnabled
	if d.ciKey, err = landingCIKeyOf(ctx, tx, op); err != nil {
		return d, err
	}
	_, d.grants, err = readLandingGrants(ctx, tx, landingRepoStream)
	return d, err
}

// grantEnabled: the Owner's signed standing envelope in force names the
// coordinator's external host for repo (§2.1). Until he signs that entry no
// grant is requested, written or consumed: C-5 changes nothing before it.
// Dropping the entry (a tightening) stops grants at once.
func grantEnabled(ctx context.Context, tx pgx.Tx, tenant, repo string) (bool, error) {
	if tenant == "" {
		return false, nil
	}
	env, err := inForce(ctx, tx, rules.KindEnvelope+":"+tenant)
	if err != nil || env == nil || env.SignedBy == "" || env.Envelope == nil {
		return false, err
	}
	h, ok := env.Envelope.Pinned(rules.PinCoordinator)
	return ok && slices.Contains(h.Repos, repo), nil
}

// readGrantChecks: the accepted attempt's checks (acceptance) and its latest
// passing review, with the review's evidence body for the trust questions.
func readGrantChecks(ctx context.Context, tx pgx.Tx, task, attempt string) ([]GrantAcceptance, GrantReview, []byte, error) {
	acc := []GrantAcceptance{}
	var rev GrantReview
	if attempt == "" {
		return acc, rev, nil, nil
	}
	rows, err := tx.Query(ctx, `SELECT verification_id, verdict FROM agentrt.verification WHERE task_id=$1 AND attempt_id=$2 AND method<>'review' ORDER BY verification_id`, task, attempt)
	if err != nil {
		return nil, rev, nil, err
	}
	acc, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (GrantAcceptance, error) {
		var a GrantAcceptance
		return a, r.Scan(&a.VerificationID, &a.Result)
	})
	if err != nil {
		return nil, rev, nil, err
	}
	var body []byte
	err = tx.QueryRow(ctx, `SELECT a.task_id, a.attempt_id, a.executor_actor_id, COALESCE(a.model_provider,''), v.verdict, v.evidence_artifact_id, COALESCE(b.body,''::bytea)
	  FROM agentrt.verification v JOIN agentrt.attempt a ON a.attempt_id=v.verifier_attempt_id
	  LEFT JOIN agentrt.artifact_blob b ON b.artifact_id=v.evidence_artifact_id
	  WHERE v.task_id=$1 AND v.attempt_id=$2 AND v.method='review' AND v.verdict='pass'
	  ORDER BY v.created_at DESC, v.verification_id DESC LIMIT 1`, task, attempt).Scan(&rev.Task, &rev.Attempt, &rev.Executor, &rev.Provider, &rev.Conclusion, &rev.Artifact, &body)
	if errors.Is(err, pgx.ErrNoRows) {
		return acc, GrantReview{}, nil, nil
	}
	return acc, rev, body, err
}

var (
	trustHeading = regexp.MustCompile(`(?i)^#{2,3}\s*trust chain answers\s*$`)
	trustLine    = regexp.MustCompile(`^(?:[-*]\s+)?\**T([1-4])\**\s*:\s*(.*)$`)
	trustAnswer  = regexp.MustCompile(`(?i)^\**(yes|no)\b\**[\s,.;:\x{2014}\x{2013}-]+(\S.{11,})$`)
)

// trustAnswered: the review's evidence ends in the trust chain's answer
// section (store.TrustReviewText): under the heading "## Trust chain
// answers", exactly one line for each of T1-T4, each Yes or No followed by
// at least a dozen characters of explanation that is not a question and
// does not copy a question (§12.2 item 3, review 694 B-02). It rejects the
// question text alone, an unrelated numbered list, a missing or empty
// answer. Its boundary, stated: it proves four structured answers exist,
// not that they are true, nor that the reviewer read the diff; that rests
// on the reviewer being another vendor and on the Owner's own check.
func trustAnswered(body []byte) bool {
	lines := strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n")
	start := -1
	for i, l := range lines {
		if trustHeading.MatchString(strings.TrimSpace(l)) {
			start = i + 1
		}
	}
	if start < 0 {
		return false
	}
	seen := map[string]bool{}
	for _, l := range lines[start:] {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "#") {
			break
		}
		m := trustLine.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		if seen[m[1]] || !trustAnswerValid(m[2]) {
			return false
		}
		seen[m[1]] = true
	}
	return len(seen) == 4
}

func trustAnswerValid(answer string) bool {
	m := trustAnswer.FindStringSubmatch(strings.TrimSpace(answer))
	if m == nil {
		return false
	}
	why := strings.ToLower(strings.TrimSpace(m[2]))
	if strings.HasSuffix(why, "?") || strings.HasPrefix(why, "<") || strings.HasPrefix(why, "...") {
		return false
	}
	for _, q := range trustQuestions {
		if strings.Contains(why, q) {
			return false
		}
	}
	// Review 698 N-02: obvious placeholders are not answers — a bracketed
	// or angled blank, an ellipsis, TBD/TODO-like fillers, the prompt's own
	// example, an explanation of fewer than three distinct words.
	if strings.ContainsAny(why, "[]<>…") || strings.Contains(why, "...") || strings.Contains(why, trustExample) {
		return false
	}
	words := map[string]bool{}
	for _, w := range strings.FieldsFunc(why, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if slices.Contains(trustFillers, w) {
			return false
		}
		words[w] = true
	}
	return len(words) >= 3
}

// trustExample is the example answer TrustReviewText gives, which is no
// answer; trustFillers are words that only stand in for one.
const trustExample = "the token is still read from"

var trustFillers = []string{"tbd", "todo", "tba", "fixme", "xxx", "placeholder", "lorem", "ipsum"}

// grantExt is the external half: the result's diff, R's patch and R's CI.
type grantExt struct {
	single  bool
	changes []rules.LandingChange
	patch   GrantPatch
	ci      GrantCIEvidence
}

func readGrantExt(ctx context.Context, src GrantSources, d grantDB) (grantExt, error) {
	var x grantExt
	if !src.complete() {
		return x, errors.New("landing grant evidence unavailable: no Git or CI reader in this process")
	}
	f, o := d.facts, d.op
	if !landing.ValidSHA(f.A) || !landing.ValidSHA(f.Base) || !landing.ValidSHA(o.R) || !landing.ValidSHA(o.M0) {
		return x, grantRefused("not_waiting", "operation %s is not prepared", d.opID)
	}
	var err error
	if x.single, x.changes, err = src.Facts.Result(ctx, f.Repo, f.Task, f.Attempt, f.Base, f.A); err != nil {
		return x, err
	}
	if x.patch, err = src.Facts.GrantPatch(ctx, o.A, o.M0, o.R); err != nil {
		return x, err
	}
	x.ci, err = grantCIRead(ctx, src.Facts, d.ciKey, o)
	return x, err
}

// buildCandidate composes candidate_digest's inputs.
func buildCandidate(f landing.Facts, step, op string, o landing.Operation, acc []GrantAcceptance, rev GrantReview, trust bool,
	patch GrantPatch, ci GrantCIEvidence, key *GrantCIKey, stg bool) GrantCandidate {
	siblings := map[string]string{}
	if key != nil {
		siblings = maps.Clone(key.Siblings)
	}
	c := GrantCandidate{Env: "prod", Tenant: f.Tenant, Repo: f.Repo, Mission: f.Mission, PlanStep: step, Task: f.Task, Attempt: o.Attempt,
		Op: op, A: o.A, Base: f.Base, R: o.R, M0: o.M0, CommitterDate: o.CommitterDate.UTC().Format(time.RFC3339Nano),
		Changes: []GrantChange{}, PatchSHA256: patch.SHA256, Files: len(f.Changes), Acceptance: acc, Review: rev,
		ReviewPatchMatch: patch.Match, STGRelease: stg, HardCorePaths: []GrantHardPath{}, TrustReview: trust,
		CI: GrantCandidateCI{Pipeline: ci.Pipeline, Run: ci.Run, UID: ci.UID, CreatedAt: ci.CreatedAt.UTC().Format(time.RFC3339Nano),
			Status: ci.Status, SHA: ci.Candidate, Siblings: siblings}}
	if c.Acceptance == nil {
		c.Acceptance = []GrantAcceptance{}
	}
	seen := map[string]bool{}
	for _, ch := range f.Changes {
		c.Changes = append(c.Changes, GrantChange{Old: ch.OldPath, New: ch.NewPath, OldMode: ch.OldMode, NewMode: ch.NewMode, Added: ch.Added, Deleted: ch.Deleted})
		c.Lines += max(ch.Added, 0) + max(ch.Deleted, 0)
		for _, p := range ch.Paths() {
			if entry := rules.LandingHardCoreEntry(f.Repo, p); entry != "" && !seen[p] {
				seen[p] = true
				c.HardCorePaths = append(c.HardCorePaths, GrantHardPath{Path: p, Entry: entry})
			}
			c.DecisionChain = c.DecisionChain || rules.LandingDecisionChain(f.Repo, p)
		}
	}
	slices.SortFunc(c.HardCorePaths, func(a, b GrantHardPath) int {
		if a.Path < b.Path {
			return -1
		}
		if a.Path > b.Path {
			return 1
		}
		return 0
	})
	return c
}

// composeMaterial joins the two halves at now, for stg as requested.
func composeMaterial(d grantDB, x grantExt, stg bool, now time.Time) (*LandingGrantMaterial, error) {
	f := d.facts
	f.SingleCommit, f.Changes = x.single, x.changes
	m := &LandingGrantMaterial{Phase: d.op.Phase, Latest: d.latest, Pinned: f.ExecutorPin != "", GrantEnabled: d.enabled,
		Rule: d.rule, ChecksVersion: rules.LandingChecksVersion, WaitingSoft: slices.Clone(d.op.Soft), Problems: []string{}, ComputedAt: now,
		Patch: x.patch.Shown, PatchTruncated: x.patch.Truncated, PatchSize: x.patch.Size, Grants: []LandingGrantStatus{}}
	if m.WaitingSoft == nil {
		m.WaitingSoft = []string{}
	}
	problem := func(code string) {
		if !slices.Contains(m.Problems, code) {
			m.Problems = append(m.Problems, code)
		}
	}
	reason, soft := landing.PolicyChecks(f, now)
	m.SoftAtRequest = refusalSet(soft)
	if reason != "" {
		problem(reason) // floor (structural or absolute) or rule: no grant waives these
	}
	if len(m.SoftAtRequest) == 0 {
		problem("no_soft_failure")
	}
	m.Action = GrantActionFor(m.SoftAtRequest)
	trust := m.Action == ActionCoreLanding && trustAnswered(d.reviewBody)
	m.Candidate = buildCandidate(f, d.step, d.opID, d.op, d.acceptance, d.review, trust, x.patch, x.ci, d.ciKey, stg)
	var err error
	if m.CandidateDigest, err = m.Candidate.Digest(); err != nil {
		return nil, err
	}
	if m.MaterialDigest, err = materialDigest(m.CandidateDigest, m.SoftAtRequest, m.Rule, m.ChecksVersion); err != nil {
		return nil, err
	}
	switch {
	case d.op.Phase != "awaiting_grant" || !d.latest:
		problem("not_waiting")
	case !f.SingleCommit:
		problem("result")
	}
	if !d.enabled {
		problem("grant_not_enabled")
	}
	if !rules.GrantSupported(f.Repo) {
		problem("grant_unsupported")
	}
	if m.Action == ActionGuardrailLanding && !m.Pinned {
		problem("not_pinned") // §3.4: only a pinned root waits on soft failures alone
	}
	if d.review.Task == "" {
		problem("review_missing")
	}
	if m.Action == ActionCoreLanding && !trust {
		problem("trust_review_missing")
	}
	if !x.patch.Match {
		problem("patch_changed")
	}
	switch {
	case d.ciKey == nil:
		problem("ci_not_pinned") // its CI was not pinned to sibling commits: no grant rests on it
	case grantCIProblem(d.op, d.ciKey, x.ci, now):
		problem("ci_not_green")
	}
	if f.Rule != nil && f.Rule.Landing != nil {
		m.QuotaMax = f.Rule.Landing.Limits.MaxLandingsPerDay
	}
	m.QuotaReserved = f.ReservedToday
	m.QuotaNote = "today's count as of this read; the push checks it again"
	m.CompareURL = fmt.Sprintf("%s/bifrost/%s/compare/%s...%s", GiteaWebBase, f.Repo, d.op.M0, d.op.R)
	m.GiteaNotice = "Gitea's own page is not rendered by this platform's code"
	m.Title = "Soft guardrail change"
	if m.Action == ActionCoreLanding {
		m.Title = "Trusted core change"
		m.FirstLine = "Approving lets the lander push this commit to main. It changes approval, identity or landing-gate code; later approvals are handled by the new code."
	}
	if stg {
		m.STGNotice = "Approving also authorizes this commit to go to STG with the B5 batch."
	}
	for _, g := range d.grants {
		if g.Op == d.opID {
			m.Grants = append(m.Grants, g.status(now, d.op, d.latest))
		}
	}
	return m, nil
}

// ReadLandingGrantMaterial computes an operation's material afresh (the
// material route, the create check). It writes nothing.
func ReadLandingGrantMaterial(ctx context.Context, db DB, src GrantSources, op string, stg bool) (*LandingGrantMaterial, error) {
	var d grantDB
	if err := InTx(ctx, db, func(tx pgx.Tx) error {
		var e error
		d, e = readGrantDB(ctx, tx, src.Facts, op, DecisionAt())
		return e
	}); err != nil {
		return nil, err
	}
	x, err := readGrantExt(ctx, src, d)
	if err != nil {
		return nil, err
	}
	return composeMaterial(d, x, stg, DecisionAt())
}

// CheckLandingGrantRequest is the create check's store half (§5.6 item 2):
// the material recomputed now matches the request exactly, the action is
// the one its soft set takes, the request names the latest waiting
// operation and its R, nothing refuses it, and main is still M0. The
// approval service's half (one open request per operation) is the caller's.
func CheckLandingGrantRequest(ctx context.Context, db DB, src GrantSources, action string, p GrantParams) (*LandingGrantMaterial, error) {
	m, err := ReadLandingGrantMaterial(ctx, db, src, p.Op, p.STGRelease)
	if err != nil {
		return nil, err
	}
	if err := requestMatches(m, action, p); err != nil {
		return m, err
	}
	head, err := src.Main(ctx)
	if err != nil || !landing.ValidSHA(head) {
		return m, grantRefused("main_unreadable", "main could not be read: %v", err)
	}
	if head != m.Candidate.M0 {
		return m, grantRefused("main_changed", "main is %s, not M0 %s: refresh the operation first", head, m.Candidate.M0)
	}
	return m, nil
}

func requestMatches(m *LandingGrantMaterial, action string, p GrantParams) error {
	switch {
	case len(m.Problems) > 0:
		return grantRefused(m.Problems[0], "the operation cannot be granted now: %v", m.Problems)
	case m.Candidate.Task != p.Task || m.Candidate.Repo != p.Repo:
		return grantRefused("not_waiting", "op %s is not task %s's in %s", p.Op, p.Task, p.Repo)
	case m.Candidate.R != p.R:
		return grantRefused("r_mismatch", "the operation's R is %s, not %s", m.Candidate.R, p.R)
	case action != m.Action:
		return grantRefused("action_mismatch", "soft %v takes %s, not %s", m.SoftAtRequest, m.Action, action)
	case m.MaterialDigest != p.MaterialDigest:
		return grantRefused("material_mismatch", "the material is now %s, not %s: request again with handin.sh status", m.MaterialDigest, p.MaterialDigest)
	}
	return nil
}

// GrantApproval is the approval record the executor read back from the
// approval service and verified (server/agentrt_grant.go): the store writes
// only what that record says.
type GrantApproval struct {
	ID, Action, ParamsHash string
	Number                 int64
	ApprovedAt             time.Time
}

// WriteLandingGrant is the approval executor's store half (§5.6 item 4).
// Outside any transaction: an existing grant of this approval answers at
// once (idempotent, its expiry unchanged); main is read, and if it moved
// the operation is discarded{main_changed} and the execution fails; the
// Git and CI evidence is read. Then in one transaction, in the global lock
// order (rule shared → step → task → mission FOR SHARE → repository stream),
// the database half is read again and the material must still be the
// approved one; only then is landing_grant appended. A COMMIT transport
// error is read back behind the same stream lock (§5.7 table).
func WriteLandingGrant(ctx context.Context, db DB, src GrantSources, a GrantApproval, p GrantParams) (LandingGrant, error) {
	if a.ID == "" || a.Number < 1 || !slices.Contains(GrantActions, a.Action) || !grantHex64.MatchString(a.ParamsHash) || a.ApprovedAt.IsZero() {
		return LandingGrant{}, invalid("grant executor: an approval record is required")
	}
	if !src.complete() {
		return LandingGrant{}, errors.New("landing grant evidence unavailable: no Git or CI reader in this process")
	}
	grantID := "lg_" + a.ID
	var pre grantDB
	var existing *grantView
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		var e error
		pre, e = readGrantDB(ctx, tx, src.Facts, p.Op, DecisionAt())
		if e != nil {
			return e
		}
		for _, g := range pre.grants {
			if g.GrantID == grantID {
				existing = g
			}
		}
		return nil
	})
	if err != nil {
		return LandingGrant{}, err
	}
	if existing != nil {
		return sameGrant(existing, a, p)
	}
	head, err := src.Main(ctx)
	if err != nil || !landing.ValidSHA(head) {
		return LandingGrant{}, grantRefused("main_unreadable", "main could not be read: %v", err)
	}
	if head != pre.op.M0 {
		// §5.4 / §5.6 item 4: main moved; the approval is stale.
		if pre.op.Phase == "awaiting_grant" && pre.latest {
			if werr := InTx(ctx, db, func(tx pgx.Tx) error {
				mission, e := lockWaitingRoot(ctx, tx, pre.op.Task)
				if e != nil {
					return e
				}
				_, e = refreshWaitingLocked(ctx, tx, pre.op.Task, p.Op, mission, head)
				return e
			}); werr != nil && !errors.Is(werr, ErrInvalid) {
				return LandingGrant{}, werr
			}
		}
		return LandingGrant{}, grantRefused("main_changed", "main advanced to %s since M0 %s; the operation is refreshed and needs a new request", head, pre.op.M0)
	}
	x, err := readGrantExt(ctx, src, pre)
	if err != nil {
		return LandingGrant{}, err
	}
	var g LandingGrant
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return LandingGrant{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var tenant, step, mission string
	if err = tx.QueryRow(ctx, `SELECT m.tenant, COALESCE(t.plan_node_id,''), m.mission_id FROM agentrt.task t JOIN agentrt.mission m USING (mission_id) WHERE t.task_id=$1`,
		pre.op.Task).Scan(&tenant, &step, &mission); err != nil {
		return LandingGrant{}, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared($1, hashtext($2))`, ruleLock, "landing:"+tenant); err != nil {
		return LandingGrant{}, err
	}
	hook("landing grant: rule locked", p.Op)
	if err = lockLandingBinding(ctx, tx, pre.op.Task, tenant, step, mission); err != nil {
		return LandingGrant{}, err
	}
	hook("landing grant: binding locked", p.Op)
	if err = lockStream(ctx, tx, landingRepoStream); err != nil {
		return LandingGrant{}, err
	}
	now := DecisionAt()
	d, err := readGrantDB(ctx, tx, src.Facts, p.Op, now)
	if err != nil {
		return LandingGrant{}, err
	}
	for _, other := range d.grants {
		if other.GrantID == grantID {
			return sameGrant(other, a, p)
		}
	}
	if d.op.A != pre.op.A || d.op.R != pre.op.R || d.op.M0 != pre.op.M0 || d.facts.Base != pre.facts.Base || d.facts.Attempt != pre.facts.Attempt {
		return LandingGrant{}, grantRefused("not_waiting", "the operation changed while its evidence was read")
	}
	m, err := composeMaterial(d, x, p.STGRelease, now)
	if err != nil {
		return LandingGrant{}, err
	}
	if err = requestMatches(m, a.Action, p); err != nil {
		return LandingGrant{}, err
	}
	for _, other := range d.grants {
		if other.Op == p.Op && other.usableAt(now) {
			return LandingGrant{}, grantRefused("grant_exists", "operation %s already has the valid grant %s", p.Op, other.GrantID)
		}
	}
	approved := a.ApprovedAt.UTC()
	if !now.Before(approved.Add(LandingGrantTTL)) {
		return LandingGrant{}, grantRefused("approval_too_old", "approved %s, more than 24 hours ago", approved.Format(time.RFC3339))
	}
	granted := now.UTC()
	if granted.Before(approved) {
		granted = approved
	}
	g = LandingGrant{V: LandingGrantVersion, GrantID: grantID, ApprovalID: a.ID, ApprovalNumber: a.Number, Action: a.Action, ParamsHash: a.ParamsHash,
		Home: "prod", Env: "prod", Tenant: m.Candidate.Tenant, Repo: m.Candidate.Repo, Task: m.Candidate.Task, Attempt: m.Candidate.Attempt, Op: p.Op,
		A: m.Candidate.A, Base: m.Candidate.Base, R: m.Candidate.R, M0: m.Candidate.M0, CommitterDate: m.Candidate.CommitterDate,
		CandidateDigest: m.CandidateDigest, MaterialDigest: m.MaterialDigest, Waives: slices.Clone(m.SoftAtRequest), SoftAtRequest: slices.Clone(m.SoftAtRequest),
		Files: int64(m.Candidate.Files), Lines: int64(m.Candidate.Lines), Rule: m.Rule, ChecksVersion: m.ChecksVersion,
		CI: GrantCI{Pipeline: m.Candidate.CI.Pipeline, Run: m.Candidate.CI.Run, UID: m.Candidate.CI.UID, CreatedAt: m.Candidate.CI.CreatedAt,
			Status: m.Candidate.CI.Status, SHA: m.Candidate.CI.SHA},
		Purpose: LandingGrantPurpose, STGRelease: p.STGRelease, ApprovedAt: approved.Format(time.RFC3339Nano),
		GrantedAt: granted.Format(time.RFC3339Nano), ExpiresAt: approved.Add(LandingGrantTTL).Format(time.RFC3339Nano)}
	if _, err = Append(ctx, tx, Event{Stream: landingRepoStream, Type: "note", Actor: "controller", TaskID: g.Task, MissionID: mission,
		Body: map[string]any{"text": fmt.Sprintf("Landing grant from approval #%d (%s)", a.Number, a.Action), "landing_grant": toBody(g)}}); err != nil {
		return LandingGrant{}, err
	}
	hook("landing grant: before commit", p.Op)
	commitErr := tx.Commit(ctx)
	if commitErr == nil {
		return g, nil
	}
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	var back *grantView
	rerr := InTx(rctx, db, func(tx pgx.Tx) error {
		if e := lockStream(rctx, tx, landingRepoStream); e != nil {
			return e
		}
		grants, _, e := readLandingGrants(rctx, tx, landingRepoStream)
		back = grants[grantID]
		return e
	})
	switch {
	case rerr != nil:
		return LandingGrant{}, fmt.Errorf("%w: commit: %v; readback: %v", ErrGrantUncertain, commitErr, rerr)
	case back != nil:
		return back.LandingGrant, nil
	}
	return LandingGrant{}, fmt.Errorf("landing grant not written (read back): %w", commitErr)
}

// sameGrant answers a repeated execution with the grant already written,
// unchanged (T28), when it is this approval's for these parameters.
func sameGrant(g *grantView, a GrantApproval, p GrantParams) (LandingGrant, error) {
	if g.Action != a.Action || g.ParamsHash != a.ParamsHash || g.ApprovalNumber != a.Number || g.Op != p.Op || g.R != p.R ||
		g.MaterialDigest != p.MaterialDigest || g.STGRelease != p.STGRelease {
		return LandingGrant{}, grantRefused("grant_conflict", "approval %s already has a different grant", a.ID)
	}
	return g.LandingGrant, nil
}
