package s5check

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// CheckS5Candidate reads all four immutable commits, checks patch correspondence
// and the complete M0/R trees, then invokes only protected checker/runner code.
// Passed is false until the independently supplied behavior runner succeeds.
// PipelineRun, authenticated review and final landing authorization remain the
// responsibility of the caller; this type deliberately is not VerifiedS5.
func CheckS5Candidate(ctx context.Context, reader TrustedTreeReader, policy TrustedPolicy, key CandidateKey) ContentEvidence {
	out := ContentEvidence{Schema: Schema, CandidateKey: key, Files: []FileEvidence{}, Findings: []Finding{}}
	add := func(code, p, detail string) {
		out.Findings = append(out.Findings, Finding{Code: code, Path: p, Line: 0, Column: 0, Detail: detail})
	}
	defer func() { sortFindings(out.Findings) }()
	// This check precedes profile loading, even for a fabricated Go profile.
	for _, p := range policy.CandidatePaths {
		if path.Ext(p) == ".go" {
			add("S5_LANGUAGE_DISABLED", p, "Go candidate profiles are disabled")
			return out
		}
	}
	if key.R == "" {
		add("S5_EVIDENCE_MISSING", "", "R evidence is not available during preparation")
		return out
	}
	if err := validateKey(key); err != nil {
		add("S5_BINDING", "", err.Error())
		return out
	}
	profiles, err := policyFiles(policy.Manifest)
	if err != nil {
		add("S5_BINDING", "", err.Error())
		return out
	}
	manifestDigest, err := Digest(policy.Manifest)
	if err != nil || manifestDigest != unprefix(key.PolicyDigest) {
		add("S5_BINDING", "", "trusted policy digest does not match key")
		return out
	}
	// toolchainDigest is the image tool lock digest (see ToolchainDigest); the
	// lock must also pin the TypeScript release the profile was enrolled with.
	toolchain, err := toolchainBinding(policy.ToolsLock, policy.Manifest)
	if err != nil || toolchain != unprefix(key.ToolchainDigest) {
		add("S5_BINDING", "", "trusted tool lock does not match the key's toolchain digest")
		return out
	}
	if reader == nil || policy.Checker == nil {
		add("S5_EVIDENCE_MISSING", "", "trusted tree reader and static checker are required")
		return out
	}
	trees := map[string]TreeSnapshot{}
	for _, revision := range []string{key.Base, key.A, key.M0, key.R} {
		if _, exists := trees[revision]; exists {
			continue
		}
		tree, err := reader.ReadTree(ctx, revision)
		if err != nil {
			add("S5_BINDING", "", "cannot read complete immutable tree: "+err.Error())
			return out
		}
		if tree.Commit != revision || !fullSHA.MatchString(tree.Tree) {
			add("S5_BINDING", "", "tree reader returned a different identity")
			return out
		}
		trees[revision] = tree
	}
	baseline, result := trees[key.M0], trees[key.R]
	if baseline.Tree != key.BaselineTree || result.Tree != key.ResultTree {
		add("S5_BINDING", "", "actual Git tree differs from candidate key")
		return out
	}
	if !oneParent(trees[key.A], key.Base) || !oneParent(result, key.M0) {
		add("S5_PATCH_MISMATCH", "", "A must be one commit on base and R one commit on M0")
		return out
	}
	original, err := reader.ReadPatch(ctx, key.Base, key.A)
	if err != nil {
		add("S5_PATCH_MISMATCH", "", "cannot read original patch: "+err.Error())
		return out
	}
	replay, err := reader.ReadPatch(ctx, key.M0, key.R)
	if err != nil {
		add("S5_PATCH_MISMATCH", "", "cannot read replay patch: "+err.Error())
		return out
	}
	if original.StableID == "" || original.StableID != replay.StableID || !sameChanges(trees[key.Base], trees[key.A], baseline, result) {
		add("S5_PATCH_MISMATCH", "", "stable patch-id or path/mode changes differ")
		return out
	}
	changed := changedPaths(baseline, result)
	candidates := EvidencePaths(key.Repo, changed)
	for _, p := range candidates {
		// A candidate added, deleted or turned into another kind has no
		// before/after blob pair; it is an S5_CHANGE_KIND finding below and
		// such evidence never passes.
		before, bok := baseline.Files[p]
		after, aok := result.Files[p]
		if bok && aok {
			out.Files = append(out.Files, FileEvidence{Path: p, BeforeBlob: before.Blob, AfterBlob: after.Blob})
		}
	}
	requested := append([]string{}, policy.CandidatePaths...)
	if len(requested) == 0 {
		// The CI request names no paths: check exactly the evidence set. A
		// registered candidate without a trusted profile is refused below.
		requested = candidates
	}
	for _, p := range requested {
		if !validPath(p) {
			add("S5_PATH_NOT_ENROLLED", p, "invalid candidate path")
			continue
		}
		if _, exists := profiles[p]; !exists {
			add("S5_PATH_NOT_ENROLLED", p, "candidate has no exact trusted profile")
			continue
		}
		before, bok := baseline.Files[p]
		after, aok := result.Files[p]
		if !bok || !aok || before.Mode != "100644" || after.Mode != "100644" || before.Binary || after.Binary {
			add("S5_CHANGE_KIND", p, "only existing regular, nonbinary 100644 files may be modified")
		}
	}
	// A removed enrolled file cannot disappear from the scan even in caller-only
	// mode or when an explicit path list attempts to hide that endpoint.
	for _, p := range changed {
		if _, exists := profiles[p]; !exists {
			continue
		}
		before, bok := baseline.Files[p]
		after, aok := result.Files[p]
		if !bok || !aok || before.Mode != "100644" || after.Mode != "100644" || before.Binary || after.Binary {
			add("S5_CHANGE_KIND", p, "enrolled endpoint changed kind, mode or binary status")
		}
	}
	static, err := policy.Checker.Check(ctx, StaticRequest{Schema: Schema, Baseline: baseline, Result: result, Policies: policy.Manifest, Key: key})
	if err != nil {
		add(stageCode(err), "", "protected static checker failed: "+err.Error())
		sortFindings(out.Findings)
		return out
	}
	out.Findings = append(out.Findings, static.Findings...)
	for _, pair := range [][3]string{{"policy", static.PolicyDigest, key.PolicyDigest}, {"callers", static.CallersDigest, key.CallersDigest}, {"types", static.TypesDigest, key.TypesDigest}, {"tests", static.TestsDigest, key.TestsDigest}} {
		if !digestSHA.MatchString(pair[1]) || unprefix(pair[1]) != unprefix(pair[2]) {
			add("S5_BINDING", "", pair[0]+" digest differs from fixed candidate key")
		}
	}
	if len(out.Findings) == 0 {
		if policy.Behavior == nil {
			add("S5_BEHAVIOR", "", "trusted behavior suite was not executed")
		} else {
			behavior, err := policy.Behavior.Run(ctx, result, key)
			if err != nil {
				add("S5_BEHAVIOR", "", "trusted behavior suite failed: "+err.Error())
			} else if !behavior.Passed || behavior.ResultTree != result.Tree || unprefix(behavior.TestsDigest) != unprefix(key.TestsDigest) {
				add("S5_BEHAVIOR", "", "behavior result missing, failed, or bound to different tree/suite: "+behavior.Detail)
			}
		}
	}
	sortFindings(out.Findings)
	out.Passed = len(out.Findings) == 0
	return out
}

func validateKey(key CandidateKey) error {
	if key.Repo == "" || key.RuleVersion == 0 || key.Generation == 0 || key.CIUID == "" {
		return fmt.Errorf("missing repo, rule version, generation or CI UID")
	}
	for _, v := range []string{key.Base, key.A, key.M0, key.R, key.BaselineTree, key.ResultTree, key.UISHA} {
		if !fullSHA.MatchString(v) {
			return fmt.Errorf("all commit and tree identities must be full Git SHAs")
		}
	}
	for _, v := range []string{key.RuleDigest, key.CheckerImageDigest, key.PolicyDigest, key.ToolchainDigest, key.CallersDigest, key.TypesDigest, key.TestsDigest, key.KeyDigest, key.ReviewBindingDigest, key.PatchEvidenceDigest} {
		if !digestSHA.MatchString(v) {
			return fmt.Errorf("all bound digests must be SHA-256")
		}
	}
	return nil
}

func policyFiles(raw json.RawMessage) (map[string]FilePolicy, error) {
	var manifest struct {
		Files []FilePolicy `json:"files"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, fmt.Errorf("invalid protected policy: %w", err)
	}
	if len(manifest.Files) == 0 {
		return nil, fmt.Errorf("protected policy has no profiles")
	}
	out := map[string]FilePolicy{}
	for _, p := range manifest.Files {
		if !validPath(p.Path) || p.Language != "ts" || path.Ext(p.Path) != ".ts" || p.ProfileVersion < 1 {
			return nil, fmt.Errorf("invalid TS-only file policy")
		}
		if _, ok := out[p.Path]; ok {
			return nil, fmt.Errorf("duplicate policy path")
		}
		out[p.Path] = p
	}
	return out, nil
}

func unprefix(s string) string { return strings.TrimPrefix(s, "sha256:") }
func oneParent(tree TreeSnapshot, parent string) bool {
	return len(tree.Parents) == 1 && tree.Parents[0] == parent
}

// EvidencePaths is the evidence file contract shared with
// landing.VerifyS5Evidence: ContentEvidence.Files names exactly the changed
// paths the platform routes through S5, decided by the one registry
// predicate (rules.S5Candidate) that landing.ClassifyS5PathsAndRelations
// applies to every endpoint of the change. It is neither the whole diff (an
// ordinary display or caller file in the same change keeps its own landing
// rule, design §5 "展示文件仍按原展示判定 ... 逐 change 分类") nor every
// enrolled file (an unchanged enrolled file has nothing to prove). The
// checker and the trusted replay bind M0→R to base→A path for path
// (sameChanges), so the two sides compute the same set.
func EvidencePaths(repo string, changed []string) []string {
	out := []string{}
	for _, p := range changed {
		if rules.S5Candidate(repo, p) {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

func changedPaths(before, after TreeSnapshot) []string {
	set := map[string]bool{}
	for p, b := range before.Files {
		a, ok := after.Files[p]
		if !ok || b.Blob != a.Blob || b.Mode != a.Mode {
			set[p] = true
		}
	}
	for p, a := range after.Files {
		b, ok := before.Files[p]
		if !ok || b.Blob != a.Blob || b.Mode != a.Mode {
			set[p] = true
		}
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func sameChanges(a, b, c, d TreeSnapshot) bool {
	x, y := changedPaths(a, b), changedPaths(c, d)
	if len(x) != len(y) {
		return false
	}
	for i, p := range x {
		if p != y[i] || a.Files[p].Mode != c.Files[p].Mode || b.Files[p].Mode != d.Files[p].Mode {
			return false
		}
	}
	return true
}

func sortFindings(findings []Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Column != b.Column {
			return a.Column < b.Column
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.Detail < b.Detail
	})
}
