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
func s5Digest(s string) bool {
	_, e := hex.DecodeString(s)
	return len(s) == 64 && strings.ToLower(s) == s && e == nil
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
	// NoReview: the attempt was accepted by a check that is no review (on
	// submission, the landing CI its check, or a deterministic pass; R5:5.7-9),
	// so the review fields are empty and no review is bound.
	NoReview bool `json:"noReview,omitempty"`
	// Internal reader's refusal survives empty/invalid rows; never candidate input.
	Problem string `json:"-"`
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
