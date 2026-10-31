package landing

import (
	"slices"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// S5CIResult is returned only by the deployment's strict CI reader. The
// adapter validates actual pipeline/task/image contents, latest run and all
// mandatory results before setting IdentityVerified. No public API decodes it.
type S5CIResult struct {
	Key                                 S5CIKey
	Run, UID, KeyDigest, Status, Reason string
	CreatedAt, CompletedAt              time.Time
	IdentityVerified                    bool
	Evidence                            *S5ContentEvidence
	// Retryable: the run failed before the mandatory tasks for an
	// infrastructure reason (release freeze). Still not evidence; the
	// operation may open a new generation for it within S5InfraRetries.
	Retryable bool
}
type S5FileEvidence struct {
	Path       string `json:"path"`
	BeforeBlob string `json:"beforeBlob"`
	AfterBlob  string `json:"afterBlob"`
}
type S5Finding struct {
	Code   string   `json:"code"`
	Path   string   `json:"path"`
	Line   int      `json:"line"`
	Column int      `json:"column"`
	Detail string   `json:"detail"`
	Symbol string   `json:"symbol,omitempty"`
	Chain  []string `json:"chain,omitempty"`
}

// S5ContentBinding is the flat schema-2 checker result from design §3.1.
// CI additionally binds the complete S5CIKey (including task/op/environment).
type S5ContentBinding struct {
	Schema              int    `json:"schema"`
	Repo                string `json:"repo"`
	Base                string `json:"base"`
	A                   string `json:"A"`
	M0                  string `json:"M0"`
	R                   string `json:"R"`
	BaselineTree        string `json:"baselineTree"`
	ResultTree          string `json:"resultTree"`
	RuleVersion         int    `json:"ruleVersion"`
	RuleDigest          string `json:"ruleDigest"`
	CheckerImageDigest  string `json:"checkerImageDigest"`
	PolicyDigest        string `json:"policyDigest"`
	ToolchainDigest     string `json:"toolchainDigest"`
	CallersDigest       string `json:"callersDigest"`
	TypesDigest         string `json:"typesDigest"`
	TestsDigest         string `json:"testsDigest"`
	UISHA               string `json:"uiSHA"`
	Generation          uint64 `json:"generation"`
	ReviewBindingDigest string `json:"reviewBindingDigest"`
	PatchEvidenceDigest string `json:"patchEvidenceDigest"`
}

// This copies public binding data; it does not assert success or create a
// VerifiedS5. The trusted reader must decode and compare the actual result.
func S5BindingForKey(k S5CIKey) S5ContentBinding {
	return S5ContentBinding{k.Schema, k.Repo, k.Base, k.A, k.M0, k.R, k.BaselineTree, k.ResultTree, k.RuleVersion, k.RuleDigest, k.CheckerImageDigest, k.ProfileDigest, k.ToolchainDigest, k.CallersDigest, k.TypesDigest, k.TestsDigest, k.UISHA, k.Generation, k.ReviewBindingDigest, k.PatchEvidenceDigest}
}

type S5ContentEvidence struct {
	S5ContentBinding
	CIUID     string           `json:"ciUID"`
	KeyDigest string           `json:"keyDigest"`
	Files     []S5FileEvidence `json:"files"`
	Findings  []S5Finding      `json:"findings"`
	Passed    bool             `json:"passed"`
}

// VerifiedS5 has no exported fields or decoder. Its zero value authorizes nothing.
type VerifiedS5 struct {
	key         S5CIKey
	digest, uid string
}

func VerifyS5Evidence(ci S5CIResult, f Facts, p S5Policy, b ReviewBinding) (VerifiedS5, string) {
	f.S5Policy = p
	f.S5Review = b
	q := ClassifyS5PathsAndRelations(f, p)
	if len(q.Reasons) > 0 {
		return VerifiedS5{}, q.Reasons[0]
	}
	k, why := NewS5CIKey(f, f.Operation.S5.Key.Op, f.Operation.S5.Key.Generation)
	if why != "" {
		return VerifiedS5{}, why
	}
	if ci.Key.Generation != k.Generation {
		return VerifiedS5{}, "S5_GENERATION_STALE"
	}
	if k != f.Operation.S5.Key || ci.Key != k {
		return VerifiedS5{}, "S5_BINDING"
	}
	kd, _ := k.Digest()
	name, _ := k.RunName()
	if ci.Status == "Missing" || ci.Status == "Running" || ci.UID == "" || f.Operation.S5.UID == "" {
		return VerifiedS5{}, "S5_EVIDENCE_MISSING"
	}
	if ci.UID != f.Operation.S5.UID || ci.Run != name || ci.KeyDigest != kd {
		return VerifiedS5{}, "S5_CI_IDENTITY"
	}
	// A failure the trusted reader attributed keeps its own code (§5):
	// behavior, missing evidence, a checker finding or an identity problem.
	// The reader computes it; it is never taken from the run's own output
	// unless the run's identity held, and no failure authorizes anything.
	// IdentityVerified gates success only.
	if ci.Reason != "" {
		if !strings.HasPrefix(ci.Reason, "S5_") {
			return VerifiedS5{}, "S5_CI_IDENTITY"
		}
		return VerifiedS5{}, ci.Reason
	}
	if ci.Status == "Failed" {
		return VerifiedS5{}, "S5_BEHAVIOR"
	}
	if !ci.IdentityVerified {
		return VerifiedS5{}, "S5_CI_IDENTITY"
	}
	e := ci.Evidence
	if ci.Status != "Succeeded" || e == nil {
		return VerifiedS5{}, "S5_EVIDENCE_MISSING"
	}
	if e.Generation != k.Generation {
		return VerifiedS5{}, "S5_GENERATION_STALE"
	}
	if e.S5ContentBinding != S5BindingForKey(k) || e.CIUID != ci.UID || e.KeyDigest != kd {
		return VerifiedS5{}, "S5_BINDING"
	}
	if !ci.CreatedAt.After(f.Operation.PreparedAt) || ci.CompletedAt.Before(ci.CreatedAt) {
		return VerifiedS5{}, "S5_CI_IDENTITY"
	}
	if len(e.Findings) > 0 {
		if e.Findings[0].Code == "" {
			return VerifiedS5{}, "S5_BINDING"
		}
		return VerifiedS5{}, e.Findings[0].Code
	}
	if !e.Passed {
		return VerifiedS5{}, "S5_BEHAVIOR"
	}
	// Evidence file contract (shared with s5check.EvidencePaths): Files is
	// exactly this change's S5 candidate paths, q.CandidatePaths, which
	// rules.S5Candidate selects from every endpoint of f.Changes. Not every
	// enrolled file (an unchanged one proves nothing) and not the whole diff
	// (a display file in the same change keeps its own rule through
	// rules.S5Refusals). A file outside the set, a duplicate or a bad blob is
	// S5_BINDING; a candidate without evidence is S5_EVIDENCE_MISSING.
	want := map[string]bool{}
	for _, path := range q.CandidatePaths {
		want[path] = true
	}
	seen := map[string]bool{}
	for _, file := range e.Files {
		if seen[file.Path] || !want[file.Path] || !ValidSHA(file.BeforeBlob) || !ValidSHA(file.AfterBlob) {
			return VerifiedS5{}, "S5_BINDING"
		}
		seen[file.Path] = true
	}
	for path := range want {
		if !seen[path] {
			return VerifiedS5{}, "S5_EVIDENCE_MISSING"
		}
	}
	return VerifiedS5{key: k, digest: kd, uid: ci.UID}, ""
}

// AuthorizeLanding recomputes the current key and review before removing a
// single refusal. Preparation is deliberately a different type.
func AuthorizeLanding(f Facts, v VerifiedS5, now time.Time) (string, []rules.LandingRefusal) {
	why, soft := PolicyChecks(f, now)
	if why != "" {
		return why, soft
	}
	q := ClassifyS5PathsAndRelations(f, f.S5Policy)
	if !q.NeedsS5Evidence {
		return "", soft
	}
	if len(q.Reasons) > 0 {
		return q.Reasons[0], soft
	}
	if why := ValidateS5Review(f, f.S5Review); why != "" {
		return why, soft
	}
	if why := ValidateS5Patch(f, f.S5Patch, true); why != "" {
		return why, soft
	}
	if v.digest == "" {
		return "S5_EVIDENCE_MISSING", s5FailureSoft(soft)
	}
	key, why := NewS5CIKey(f, v.key.Op, v.key.Generation)
	if why != "" {
		return why, soft
	}
	if key != v.key || key != f.Operation.S5.Key || v.uid != f.Operation.S5.UID {
		return "S5_BINDING", s5FailureSoft(soft)
	}
	if d := MayPrepareS5(f, q, now); !d.Allowed {
		return d.Reason, soft
	}
	return "", rules.S5Refusals(f.Rule.Landing, f.Repo, f.Changes)
}
func s5FailureSoft(soft []rules.LandingRefusal) []rules.LandingRefusal {
	out := slices.Clone(soft)
	if !slices.Contains(out, rules.LandingFloorSoft) {
		out = append(out, rules.LandingFloorSoft)
	}
	return out
}
