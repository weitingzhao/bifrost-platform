package landing

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// S5Policy is deployment-owned material supplied by the trusted CI adapter.
// It is never loaded from R, a rule extension, or an uploaded artifact.
type S5Policy struct {
	ProfileDigest      string `json:"profileDigest"`
	CheckerImageDigest string `json:"checkerImageDigest"`
	ToolchainDigest    string `json:"toolchainDigest"`
	CallersDigest      string `json:"callersDigest"`
	TypesDigest        string `json:"typesDigest"`
	TestsDigest        string `json:"testsDigest"`
	UISHA              string `json:"uiSHA"`
}

func S5Hash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func S5Digest(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	b, err = canon.Marshal(b)
	if err != nil {
		return "", err
	}
	return S5Hash(b), nil
}
func s5Digest(s string) bool {
	_, e := hex.DecodeString(s)
	return len(s) == 64 && strings.ToLower(s) == s && e == nil
}
func (p S5Policy) Valid() bool {
	for _, d := range []string{p.ProfileDigest, p.ToolchainDigest, p.CallersDigest, p.TypesDigest, p.TestsDigest} {
		if !s5Digest(d) {
			return false
		}
	}
	return builtImage(p.CheckerImageDigest) && ValidSHA(p.UISHA)
}

// builtImage refuses the all-zero UNBUILT placeholder digest.
func builtImage(d string) bool {
	return digest.MatchString(d) && d != "sha256:"+strings.Repeat("0", 64)
}

type PreparationRequirements struct {
	CandidatePaths   []string `json:"candidatePaths"`
	RelationRequired bool     `json:"relationRequired"`
	NeedsS5Evidence  bool     `json:"needsS5Evidence"`
	PolicyDigest     string   `json:"policyDigest"`
	RuleDigest       string   `json:"ruleDigest"`
	Reasons          []string `json:"reasons"`
}

type PrepareDecision struct {
	Decision
	Requirements PreparationRequirements
}

func s5Landing(f Facts) *rules.Landing {
	if f.Rule != nil {
		return f.Rule.Landing
	}
	return nil
}
func S5RuleDigest(f Facts) string {
	if f.Rule == nil {
		return ""
	}
	b, e := f.Rule.BodyJSON()
	if e != nil {
		return ""
	}
	return S5Hash(b)
}
func ClassifyS5PathsAndRelations(f Facts, p S5Policy) PreparationRequirements {
	q := PreparationRequirements{CandidatePaths: []string{}, Reasons: []string{}, PolicyDigest: p.ProfileDigest, RuleDigest: S5RuleDigest(f)}
	active := rules.S5Active(s5Landing(f), f.Repo)
	for _, c := range f.Changes {
		candidate := false
		for _, path := range c.Paths() {
			if rules.S5Candidate(f.Repo, path) {
				candidate = true
				if !slices.Contains(q.CandidatePaths, path) {
					q.CandidatePaths = append(q.CandidatePaths, path)
				}
				if !rules.S5Allowed(s5Landing(f), f.Repo, path) {
					q.Reasons = append(q.Reasons, "S5_PATH_NOT_ENROLLED")
				}
			}
			if active && (strings.HasSuffix(path, ".ts") || strings.HasSuffix(path, ".tsx")) {
				q.RelationRequired = true
			}
		}
		if candidate && (c.Kind != "M" || c.OldPath != c.NewPath || c.OldMode != "100644" || c.NewMode != "100644" || c.Binary) {
			q.Reasons = append(q.Reasons, "S5_CHANGE_KIND")
		}
	}
	// Inactive rules preserve legacy routing; persisted S5 operations never fall back.
	q.NeedsS5Evidence = active && (len(q.CandidatePaths) > 0 || q.RelationRequired) || f.Operation.S5.Required
	slices.Sort(q.CandidatePaths)
	slices.Sort(q.Reasons)
	q.Reasons = slices.Compact(q.Reasons)
	return q
}

// MayPrepareS5 defers only the enrolled change's floor/display. It does not
// edit Facts, persistent refusals, or daily quota, and cannot authorize main.
func MayPrepareS5(f Facts, q PreparationRequirements, now time.Time) PrepareDecision {
	d := PrepareDecision{Requirements: q}
	if !q.NeedsS5Evidence {
		d.Decision = EligibleToClaim(f, now)
		return d
	}
	if why, soft := PolicyChecks(f, now); why != "" {
		d.Decision = refuse(why)
		return d
	} else if slices.Contains(soft, rules.LandingCore) {
		d.Decision = refuse("floor")
		return d
	}
	if len(q.Reasons) > 0 {
		d.Decision = refuse(q.Reasons[0])
		return d
	}
	if !f.S5Policy.Valid() {
		d.Decision = refuse("S5_EVIDENCE_MISSING")
		return d
	}
	if why := ValidateS5Review(f, f.S5Review); why != "" {
		d.Decision = refuse(why)
		return d
	}
	if why := ValidateS5Patch(f, f.S5Patch, f.Operation.R != "" && f.S5Patch.R != "" || f.Operation.Phase == "branch_pushed"); why != "" {
		d.Decision = refuse(why)
		return d
	}
	d.Decision = eligibleToClaim(f, now, true)
	return d
}

// S5AutomaticRoute selects the no-grant lane. A change already requiring
// the original D lane for core/size/path/ordinary display failures keeps that
// lane's preparation and pinned CI. A persisted S5 operation never silently
// downgrades, including after a checker failure or a rule tightening.
func S5AutomaticRoute(f Facts) bool {
	q := ClassifyS5PathsAndRelations(f, f.S5Policy)
	if !q.NeedsS5Evidence || f.Operation.S5.Required {
		return q.NeedsS5Evidence
	}
	if f.Rule != nil && f.Rule.Landing != nil {
		soft := rules.LandingSoftRefusals(f.Rule.Landing, f.Repo, f.Changes)
		if len(rules.S5Refusals(f.Rule.Landing, f.Repo, f.Changes)) > 0 && MayAwaitGrant(f, soft) {
			return false
		}
	}
	return true
}

func PreparationDecision(f Facts, now time.Time) Decision {
	if !S5AutomaticRoute(f) {
		return EligibleToClaim(f, now)
	}
	return MayPrepareS5(f, ClassifyS5PathsAndRelations(f, f.S5Policy), now).Decision
}

type ReviewBinding struct {
	Repo                     string `json:"repo"`
	Task                     string `json:"task"`
	AcceptedAttempt          string `json:"acceptedAttempt"`
	A                        string `json:"A"`
	Base                     string `json:"base"`
	AcceptanceVerificationID string `json:"acceptanceVerificationID"`
	ImplementerAttempt       string `json:"implementerAttempt"`
	ImplementerExecutor      string `json:"implementerExecutor"`
	ImplementerProvider      string `json:"implementerProvider"`
	ReviewVerificationID     string `json:"reviewVerificationID"`
	ReviewerTask             string `json:"reviewerTask"`
	ReviewerAttempt          string `json:"reviewerAttempt"`
	ReviewerExecutor         string `json:"reviewerExecutor"`
	ReviewerProvider         string `json:"reviewerProvider"`
	Verdict                  string `json:"verdict"`
	ReviewArtifactID         string `json:"reviewArtifactID"`
	ReviewArtifactSHA256     string `json:"reviewArtifactSHA256"`
	ReviewedPatchSHA256      string `json:"reviewedPatchSHA256"`
	ReviewedStablePatchID    string `json:"reviewedStablePatchID"`
	// Internal reader's refusal survives empty/invalid rows; never candidate input.
	Problem string `json:"-"`
}

func ValidateS5Review(f Facts, b ReviewBinding) string {
	if b.Problem != "" {
		return b.Problem
	}
	if b.ReviewVerificationID == "" || b.ReviewArtifactID == "" || !s5Digest(b.ReviewArtifactSHA256) {
		return "S5_REVIEW_MISSING"
	}
	if b.ImplementerProvider == "" || b.ReviewerProvider == "" || b.ImplementerExecutor == "" || b.ReviewerExecutor == "" || b.ImplementerProvider == b.ReviewerProvider || b.ImplementerExecutor == b.ReviewerExecutor {
		return "S5_REVIEW_PROVIDER"
	}
	if b.Repo != f.Repo || b.Task != f.Task || b.AcceptedAttempt != f.Attempt || b.ImplementerAttempt != f.Attempt || b.A != f.A || b.Base != f.Base || b.AcceptanceVerificationID == "" || b.ReviewerAttempt == "" || b.ReviewerAttempt == f.Attempt || b.ReviewerTask == "" || b.ReviewerTask == f.Task || b.Verdict != "pass" {
		return "S5_REVIEW_BINDING"
	}
	if b.ReviewedPatchSHA256 != f.S5Patch.SourcePatchSHA256 || b.ReviewedStablePatchID != f.S5Patch.SourceStablePatchID {
		return "S5_REVIEW_BINDING"
	}
	return ""
}

type S5PathMode struct {
	OldPath string `json:"oldPath"`
	NewPath string `json:"newPath"`
	OldMode string `json:"oldMode"`
	NewMode string `json:"newMode"`
	Kind    string `json:"kind"`
	Binary  bool   `json:"binary"`
}
type PatchEvidence struct {
	A                    string       `json:"A"`
	ParentA              string       `json:"parentA"`
	Base                 string       `json:"base"`
	M0                   string       `json:"M0"`
	R                    string       `json:"R"`
	ParentR              string       `json:"parentR"`
	SourcePatchSHA256    string       `json:"sourcePatchSHA256"`
	ReplayPatchSHA256    string       `json:"replayPatchSHA256"`
	SourceStablePatchID  string       `json:"sourceStablePatchID"`
	ReplayStablePatchID  string       `json:"replayStablePatchID"`
	OldNewPathsAndModes  []S5PathMode `json:"oldNewPathsAndModes"`
	ReplayMatch          bool         `json:"replayMatch"`
	DiffAlgorithmVersion string       `json:"diffAlgorithmVersion"`
	BaselineTree         string       `json:"baselineTree"`
	ResultTree           string       `json:"resultTree"`
}

func ValidateS5Patch(f Facts, p PatchEvidence, replay bool) string {
	if p.A != f.A || p.Base != f.Base || p.ParentA != f.Base || !s5Digest(p.SourcePatchSHA256) || !ValidSHA(p.SourceStablePatchID) || p.DiffAlgorithmVersion != "git-stable-v1" || len(p.OldNewPathsAndModes) == 0 {
		return "S5_PATCH_MISMATCH"
	}
	paths := make([]S5PathMode, 0, len(f.Changes))
	for _, c := range f.Changes {
		paths = append(paths, S5PathMode{c.OldPath, c.NewPath, c.OldMode, c.NewMode, c.Kind, c.Binary})
	}
	a, _ := S5Digest(paths)
	b, _ := S5Digest(p.OldNewPathsAndModes)
	if a != b {
		return "S5_PATCH_MISMATCH"
	}
	if replay && (p.M0 != f.Operation.M0 || p.R != f.Operation.R || p.ParentR != p.M0 || !p.ReplayMatch || p.SourceStablePatchID != p.ReplayStablePatchID || !s5Digest(p.ReplayPatchSHA256) || !ValidSHA(p.BaselineTree) || !ValidSHA(p.ResultTree)) {
		return "S5_PATCH_MISMATCH"
	}
	return ""
}

// S5CIKey is the ONLY definition of the complete S5 preparation key (§5.1).
// landingci aliases it, so the persisted key, its digest/run name and the CI
// transport cannot drift into two encodings.
type S5CIKey struct {
	Schema              int    `json:"schema"`
	Env                 string `json:"env"`
	Tenant              string `json:"tenant"`
	Repo                string `json:"repo"`
	Task                string `json:"task"`
	Attempt             string `json:"attempt"`
	Op                  string `json:"op"`
	Base                string `json:"base"`
	A                   string `json:"A"`
	M0                  string `json:"M0"`
	R                   string `json:"R"`
	BaselineTree        string `json:"baselineTree"`
	ResultTree          string `json:"resultTree"`
	UISHA               string `json:"uiSHA"`
	RuleVersion         int    `json:"ruleVersion"`
	RuleDigest          string `json:"ruleDigest"`
	ProfileDigest       string `json:"profileDigest"`
	CheckerImageDigest  string `json:"checkerImageDigest"`
	ToolchainDigest     string `json:"toolchainDigest"`
	CallersDigest       string `json:"callersDigest"`
	TypesDigest         string `json:"typesDigest"`
	TestsDigest         string `json:"testsDigest"`
	ReviewBindingDigest string `json:"reviewBindingDigest"`
	PatchEvidenceDigest string `json:"patchEvidenceDigest"`
	Generation          uint64 `json:"generation"`
}

func (k S5CIKey) Valid() bool {
	if k.Schema != 2 || (k.Env != "prod" && k.Env != "stg") || (k.Tenant != "dev" && k.Tenant != "business") || k.Repo != "bifrost-platform" || !task.MatchString(k.Task) || !attempt.MatchString(k.Attempt) || !strings.HasPrefix(k.Op, "land:"+k.Task+":") || k.RuleVersion < 1 || k.Generation == 0 || k.Generation > math.MaxInt64 {
		return false
	}
	for _, s := range []string{k.Base, k.A, k.M0, k.R, k.BaselineTree, k.ResultTree, k.UISHA} {
		if !ValidSHA(s) {
			return false
		}
	}
	for _, s := range []string{k.RuleDigest, k.ProfileDigest, k.ToolchainDigest, k.CallersDigest, k.TypesDigest, k.TestsDigest, k.ReviewBindingDigest, k.PatchEvidenceDigest} {
		if !s5Digest(s) {
			return false
		}
	}
	return builtImage(k.CheckerImageDigest)
}

// Canonical is the single key encoding shared by the persisted keyDigest,
// the Tekton "key" parameter and the CI read-back (canon = the Python
// recorder/runner form). Digest is SHA-256 of exactly these bytes.
func (k S5CIKey) Canonical() ([]byte, error) {
	if !k.Valid() {
		return nil, fmt.Errorf("S5_BINDING: invalid key")
	}
	b, err := json.Marshal(k)
	if err != nil {
		return nil, err
	}
	return canon.Marshal(b)
}
func (k S5CIKey) Digest() (string, error) {
	b, err := k.Canonical()
	if err != nil {
		return "", err
	}
	return S5Hash(b), nil
}
func (k S5CIKey) RunName() (string, error) {
	d, e := k.Digest()
	if e != nil {
		return "", e
	}
	return "s5-" + d[:40] + "-g" + strconv.FormatUint(k.Generation, 36), nil
}
func NewS5CIKey(f Facts, op string, generation uint64) (S5CIKey, string) {
	if why := ValidateS5Review(f, f.S5Review); why != "" {
		return S5CIKey{}, why
	}
	if why := ValidateS5Patch(f, f.S5Patch, true); why != "" {
		return S5CIKey{}, why
	}
	if f.Rule == nil || !f.S5Policy.Valid() {
		return S5CIKey{}, "S5_EVIDENCE_MISSING"
	}
	p := f.S5Policy
	review, _ := S5Digest(f.S5Review)
	patch, _ := S5Digest(f.S5Patch)
	k := S5CIKey{2, f.Environment, f.Tenant, f.Repo, f.Task, f.Attempt, op, f.Base, f.A, f.Operation.M0, f.Operation.R, f.S5Patch.BaselineTree, f.S5Patch.ResultTree, p.UISHA, f.Rule.Version, S5RuleDigest(f), p.ProfileDigest, p.CheckerImageDigest, p.ToolchainDigest, p.CallersDigest, p.TypesDigest, p.TestsDigest, review, patch, generation}
	if !k.Valid() {
		return S5CIKey{}, "S5_BINDING"
	}
	return k, ""
}
