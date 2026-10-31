package s5check

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// NodeChecker refers exclusively to deployment-owned files. It does not load a
// candidate package.json, tsconfig hook, script or dependency directory.
type NodeChecker struct {
	NodeBinary   string
	Script       string
	ScriptDigest string
	ModulePath   string
	Timeout      time.Duration
}

// StageError carries only a refusal class emitted by a protected checker
// bootstrap. Candidate diagnostics arrive as structured stdout findings; their
// text is never inspected to choose an error class.
type StageError struct {
	Code string
	Err  error
}

func (e *StageError) Error() string { return e.Code + ": " + e.Err.Error() }
func (e *StageError) Unwrap() error { return e.Err }

func stageCode(err error) string {
	var stage *StageError
	if errors.As(err, &stage) && (stage.Code == "S5_TYPE" || stage.Code == "S5_PARSE") {
		return stage.Code
	}
	return "S5_EVIDENCE_MISSING"
}

func checkerProcessError(err error, stderr string) error {
	detail := fmt.Errorf("protected checker: %w: %s", err, strings.TrimSpace(stderr))
	first := strings.SplitN(strings.TrimSpace(stderr), "\n", 2)[0]
	for _, code := range []string{"S5_TYPE", "S5_PARSE"} {
		if strings.HasPrefix(first, code+":") {
			return &StageError{Code: code, Err: detail}
		}
	}
	return detail
}

func (n NodeChecker) Check(ctx context.Context, request StaticRequest) (StaticResult, error) {
	var result StaticResult
	if !filepath.IsAbs(n.Script) || !digestSHA.MatchString(n.ScriptDigest) {
		return result, errors.New("checker requires an absolute protected script and its SHA-256")
	}
	actual, err := os.ReadFile(n.Script)
	if err != nil {
		return result, err
	}
	if SHA256(actual) != unprefix(n.ScriptDigest) {
		return result, errors.New("protected checker script digest changed")
	}
	for _, p := range filepath.SplitList(n.ModulePath) {
		if !filepath.IsAbs(p) {
			return result, errors.New("compiler module paths must be absolute")
		}
	}
	node := n.NodeBinary
	if node == "" {
		node = "node"
	}
	timeout := n.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	raw, err := CanonicalJSON(request)
	if err != nil {
		return result, err
	}
	cmd := exec.CommandContext(ctx, node, n.Script)
	cmd.Dir = filepath.Dir(n.Script)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=/dev/null", "LC_ALL=C", "TZ=UTC", "NODE_PATH=" + n.ModulePath}
	cmd.Stdin = bytes.NewReader(raw)
	out := &boundedBuffer{limit: 16 << 20}
	stderr := &boundedBuffer{limit: 8192}
	cmd.Stdout = out
	cmd.Stderr = stderr
	if err = cmd.Run(); err != nil {
		return result, checkerProcessError(err, stderr.String())
	}
	dec := json.NewDecoder(bytes.NewReader(out.Bytes()))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&result); err != nil {
		return result, fmt.Errorf("invalid checker result: %w", err)
	}
	var extra any
	if err = dec.Decode(&extra); err != io.EOF {
		return result, errors.New("checker emitted multiple JSON results")
	}
	if result.Findings == nil {
		return result, errors.New("checker omitted findings")
	}
	for _, digest := range []string{result.PolicyDigest, result.CallersDigest, result.TypesDigest, result.TestsDigest} {
		if !digestSHA.MatchString(digest) {
			return result, errors.New("checker omitted a valid protected material digest")
		}
	}
	for _, f := range result.Findings {
		if !strings.HasPrefix(f.Code, "S5_") || f.Line < 0 || f.Column < 0 || f.Detail == "" {
			return result, errors.New("checker returned malformed finding")
		}
	}
	sortFindings(result.Findings)
	return result, nil
}

// StaticEvidence intentionally cannot be mistaken for ContentEvidence success.
// A zero exit status from the static command proves only this stage completed.
type StaticEvidence struct {
	Schema       int            `json:"schema"`
	StaticOnly   bool           `json:"staticOnly"`
	BaselineTree string         `json:"baselineTree"`
	ResultTree   string         `json:"resultTree"`
	Files        []FileEvidence `json:"files"`
	StaticResult
	Passed bool `json:"passed"`
}

func CheckStaticTrees(ctx context.Context, reader TrustedTreeReader, policy TrustedPolicy, baselineRevision, resultRevision string) (StaticEvidence, error) {
	out := StaticEvidence{Schema: Schema, StaticOnly: true, Files: []FileEvidence{}, StaticResult: StaticResult{Findings: []Finding{}}}
	for _, p := range policy.CandidatePaths {
		if filepath.Ext(p) == ".go" {
			out.Findings = append(out.Findings, Finding{Code: "S5_LANGUAGE_DISABLED", Path: p, Detail: "Go candidate profiles are disabled"})
			return out, nil
		}
	}
	profiles, err := policyFiles(policy.Manifest)
	if err != nil {
		return out, err
	}
	if reader == nil || policy.Checker == nil {
		return out, errors.New("trusted reader and checker are required")
	}
	baseline, err := reader.ReadTree(ctx, baselineRevision)
	if err != nil {
		return out, err
	}
	result, err := reader.ReadTree(ctx, resultRevision)
	if err != nil {
		return out, err
	}
	out.BaselineTree, out.ResultTree = baseline.Tree, result.Tree
	for _, p := range changedPaths(baseline, result) {
		before, bok := baseline.Files[p]
		after, aok := result.Files[p]
		out.Files = append(out.Files, FileEvidence{Path: p, BeforeBlob: before.Blob, AfterBlob: after.Blob})
		if _, enrolled := profiles[p]; enrolled && (!bok || !aok || before.Mode != "100644" || after.Mode != "100644" || before.Binary || after.Binary) {
			out.Findings = append(out.Findings, Finding{Code: "S5_CHANGE_KIND", Path: p, Detail: "only existing regular, nonbinary 100644 files may be modified"})
		}
	}
	for _, p := range policy.CandidatePaths {
		if _, enrolled := profiles[p]; !enrolled {
			out.Findings = append(out.Findings, Finding{Code: "S5_PATH_NOT_ENROLLED", Path: p, Detail: "candidate has no exact trusted profile"})
		}
	}
	static, err := policy.Checker.Check(ctx, StaticRequest{Schema: Schema, Baseline: baseline, Result: result, Policies: policy.Manifest, Key: CandidateKey{M0: baselineRevision, R: resultRevision, BaselineTree: baseline.Tree, ResultTree: result.Tree}})
	if err != nil {
		out.Findings = append(out.Findings, Finding{Code: stageCode(err), Detail: err.Error()})
		sortFindings(out.Findings)
		return out, nil
	}
	static.Findings = append(out.Findings, static.Findings...)
	out.StaticResult = static
	sortFindings(out.Findings)
	return out, nil
}
