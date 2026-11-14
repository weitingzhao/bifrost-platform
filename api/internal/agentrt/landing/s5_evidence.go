package landing

import (
	"time"
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
