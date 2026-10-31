// Package s5check is the read-only adapter for the protected S5 TypeScript
// checker. Static success is preparation evidence, never landing authority.
package s5check

import (
	"context"
	"encoding/json"
)

const Schema = 2

type SymbolID struct {
	File          string `json:"file"`
	ExportOrDecl  string `json:"exportOrDecl"`
	SignatureHash string `json:"signatureHash"`
}

type TypeContract struct {
	Module    string `json:"module"`
	Name      string `json:"name"`
	ShapeHash string `json:"shapeHash"`
	SourceSHA string `json:"sourceSHA"`
}

// CallRule is one enrolled standard-library call: its resolved identity and
// form. The per-member argument contracts are the §3.4 rules coded in the
// checker (ts/analyze.cjs), not data a profile can widen.
type CallRule struct {
	Target SymbolID `json:"target"`
	Form   string   `json:"form"`
}

type SiteRule struct {
	File              string   `json:"file"`
	EnclosingSymbol   string   `json:"enclosingSymbol"`
	Position          string   `json:"position"`
	ParameterIndex    *int     `json:"parameterIndex,omitempty"`
	Target            SymbolID `json:"target"`
	NormalizedASTHash string   `json:"normalizedAstHash"`
	Kind              string   `json:"kind"`
	EnclosingASTHash  string   `json:"enclosingAstHash,omitempty"`
	DefaultUse        string   `json:"defaultUse,omitempty"`
}

type GuardRule struct {
	ID                 string `json:"id"`
	EnclosingSymbol    string `json:"enclosingSymbol"`
	PredicateASTHash   string `json:"predicateAstHash"`
	ConsequenceASTHash string `json:"consequenceAstHash"`
	Site               []int  `json:"site"`
	Kind               string `json:"kind"`
}

type CallerContract struct {
	File              string   `json:"file"`
	EnclosingSymbol   string   `json:"enclosingSymbol"`
	NormalizedASTHash string   `json:"normalizedAstHash"`
	Callee            SymbolID `json:"callee"`
	Use               string   `json:"use"`
	Inputs            []string `json:"inputs"`
	Sinks             []string `json:"sinks"`
	ArgumentASTHashes []string `json:"argumentAstHashes"`
	DefaultArguments  []string `json:"defaultArguments"`
}

type FilePolicy struct {
	Path             string           `json:"path"`
	Language         string           `json:"language"`
	ProfileVersion   int              `json:"profileVersion"`
	Exports          []SymbolID       `json:"exports"`
	TypeImports      []TypeContract   `json:"typeImports"`
	ValueImports     []SymbolID       `json:"valueImports"`
	AllowedCalls     []CallRule       `json:"allowedCalls"`
	EnvironmentSites []SiteRule       `json:"environmentSites"`
	RequiredGuards   []GuardRule      `json:"requiredGuards"`
	Callers          []CallerContract `json:"callers"`
	TrustedTests     []string         `json:"trustedTests"`
}

type Finding struct {
	Code   string   `json:"code"`
	Path   string   `json:"path"`
	Line   int      `json:"line"`
	Column int      `json:"column"`
	Symbol string   `json:"symbol,omitempty"`
	Chain  []string `json:"chain,omitempty"`
	Detail string   `json:"detail"`
}

// CandidateKey is supplied by the trusted coordinator. This package does not
// authenticate a PipelineRun or ReviewBinding; the final gate still must.
type CandidateKey struct {
	Repo                string `json:"repo"`
	Base                string `json:"base"`
	A                   string `json:"A"`
	M0                  string `json:"M0"`
	R                   string `json:"R"`
	BaselineTree        string `json:"baselineTree"`
	ResultTree          string `json:"resultTree"`
	RuleVersion         uint64 `json:"ruleVersion"`
	RuleDigest          string `json:"ruleDigest"`
	CheckerImageDigest  string `json:"checkerImageDigest"`
	PolicyDigest        string `json:"policyDigest"`
	ToolchainDigest     string `json:"toolchainDigest"`
	CallersDigest       string `json:"callersDigest"`
	TypesDigest         string `json:"typesDigest"`
	TestsDigest         string `json:"testsDigest"`
	UISHA               string `json:"uiSHA"`
	CIUID               string `json:"ciUID"`
	KeyDigest           string `json:"keyDigest"`
	Generation          uint64 `json:"generation"`
	ReviewBindingDigest string `json:"reviewBindingDigest"`
	PatchEvidenceDigest string `json:"patchEvidenceDigest"`
}

type FileEvidence struct {
	Path       string `json:"path"`
	BeforeBlob string `json:"beforeBlob"`
	AfterBlob  string `json:"afterBlob"`
}

type ContentEvidence struct {
	Schema int `json:"schema"`
	CandidateKey
	Files    []FileEvidence `json:"files"`
	Findings []Finding      `json:"findings"`
	Passed   bool           `json:"passed"`
}

// TreeEntry includes all Git entries, including unsupported modes and binary
// files. The checker cannot silently lose a deleted or unreadable input.
type TreeEntry struct {
	Content string `json:"content"`
	Blob    string `json:"blob"`
	Mode    string `json:"mode"`
	Binary  bool   `json:"binary,omitempty"`
}

type TreeSnapshot struct {
	Commit  string               `json:"commit"`
	Tree    string               `json:"tree"`
	Parents []string             `json:"parents"`
	Files   map[string]TreeEntry `json:"files"`
}

type Patch struct {
	Bytes    []byte
	StableID string
}

type TrustedTreeReader interface {
	ReadTree(context.Context, string) (TreeSnapshot, error)
	ReadPatch(context.Context, string, string) (Patch, error)
}

type StaticRequest struct {
	Schema   int             `json:"schema"`
	Baseline TreeSnapshot    `json:"baseline"`
	Result   TreeSnapshot    `json:"result"`
	Policies json.RawMessage `json:"policies"`
	Key      CandidateKey    `json:"key"`
}

// StaticResult carries the material digests the checker derives from the
// protected manifest. The toolchain digest is not among them: it is the image
// tool lock digest (ToolchainDigest), bound by the Go wrapper.
type StaticResult struct {
	Findings      []Finding `json:"findings"`
	CallersDigest string    `json:"callersDigest"`
	TypesDigest   string    `json:"typesDigest"`
	TestsDigest   string    `json:"testsDigest"`
	PolicyDigest  string    `json:"policyDigest"`
}

type StaticChecker interface {
	Check(context.Context, StaticRequest) (StaticResult, error)
}

// BehaviorResult must come from a protected runner outside the candidate's
// process/container. A result file in the candidate tree is never read.
type BehaviorResult struct {
	Passed      bool
	ResultTree  string
	TestsDigest string
	Detail      string
}

type TrustedBehaviorRunner interface {
	Run(context.Context, TreeSnapshot, CandidateKey) (BehaviorResult, error)
}

type TrustedPolicy struct {
	Manifest json.RawMessage
	Checker  StaticChecker
	Behavior TrustedBehaviorRunner
	// ToolsLock is the checker image's TOOLS.lock.json; its ToolchainDigest
	// must equal the key's toolchainDigest. Missing is a binding refusal.
	ToolsLock json.RawMessage
	// CandidatePaths explicitly requests exemptions. Empty selects enrolled
	// paths touched by the diff, while still running all consumer contracts.
	CandidatePaths []string
}
