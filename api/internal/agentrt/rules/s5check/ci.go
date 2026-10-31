package s5check

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// CIRequest is the image runner's stdin request (infra docker/s5-checker
// runner.py check): the full server-issued S5CIKey, the PipelineRun UID and
// read-only bare object stores fetched by the separate fetch task.
type CIRequest struct {
	Schema         int             `json:"schema"`
	Key            landing.S5CIKey `json:"key"`
	CIUID          string          `json:"ciUID"`
	PlatformGitDir string          `json:"platformGitDir"`
	UIGitDir       string          `json:"uiGitDir"`
	PolicyRoot     string          `json:"policyRoot"`
}

const maxCIRequestBytes = 64 << 10

// A Kubernetes object UID; the trusted reader compares it with the persisted run.
var kubeUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// DecodeCIRequest accepts exactly one canonical JSON request with no unknown,
// duplicate or case-variant members, a landing-valid key and absolute clean paths.
func DecodeCIRequest(raw []byte) (CIRequest, error) {
	var req CIRequest
	if len(raw) > maxCIRequestBytes {
		return req, errors.New("S5_BINDING: CI request exceeds limit")
	}
	raw = bytes.TrimSuffix(raw, []byte("\n"))
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return req, fmt.Errorf("S5_BINDING: invalid CI request: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return req, errors.New("S5_BINDING: trailing CI request data")
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return req, err
	}
	if canonical, err := CanonicalJSON(generic); err != nil || !bytes.Equal(canonical, raw) {
		return req, errors.New("S5_BINDING: noncanonical or duplicate CI request")
	}
	// encoding/json matches member names case-insensitively, so a second member
	// differing only by case ("ciuid" next to "ciUID") would silently win. The
	// decoded struct must re-encode to exactly the input: every member name then
	// equals its json tag, at every nesting level, and every member is present.
	if exact, err := CanonicalJSON(req); err != nil || !bytes.Equal(exact, raw) {
		return req, errors.New("S5_BINDING: CI request member names must equal the schema exactly")
	}
	if req.Schema != Schema || !req.Key.Valid() {
		return req, errors.New("S5_BINDING: invalid CI request schema or key")
	}
	if !kubeUID.MatchString(req.CIUID) {
		return req, errors.New("S5_CI_IDENTITY: invalid PipelineRun UID")
	}
	for _, p := range []string{req.PlatformGitDir, req.UIGitDir, req.PolicyRoot} {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p {
			return req, errors.New("S5_BINDING: CI paths must be absolute and clean")
		}
	}
	return req, nil
}

// candidateKey maps the landing key to the content-evidence identity. The
// evidence repeats the profile digest as policyDigest and adds the CI UID and
// the landing keyDigest (S5 §3.1).
func candidateKey(req CIRequest) (CandidateKey, error) {
	k := req.Key
	digest, err := k.Digest()
	if err != nil {
		return CandidateKey{}, err
	}
	return CandidateKey{Repo: k.Repo, Base: k.Base, A: k.A, M0: k.M0, R: k.R, BaselineTree: k.BaselineTree, ResultTree: k.ResultTree,
		RuleVersion: uint64(k.RuleVersion), RuleDigest: k.RuleDigest, CheckerImageDigest: k.CheckerImageDigest, PolicyDigest: k.ProfileDigest,
		ToolchainDigest: k.ToolchainDigest, CallersDigest: k.CallersDigest, TypesDigest: k.TypesDigest, TestsDigest: k.TestsDigest, UISHA: k.UISHA,
		CIUID: req.CIUID, KeyDigest: digest, Generation: k.Generation, ReviewBindingDigest: k.ReviewBindingDigest, PatchEvidenceDigest: k.PatchEvidenceDigest}, nil
}

func refusal(key CandidateKey, code, detail string) ContentEvidence {
	return ContentEvidence{Schema: Schema, CandidateKey: key, Files: []FileEvidence{}, Findings: []Finding{{Code: code, Detail: detail}}}
}

// ciInstallation is the protected material next to policyRoot in the image:
// <root>/TOOLS.lock.json and <root>/node_modules, where policyRoot=<root>/s5check.
type ciInstallation struct {
	manifest   []byte
	lock       []byte
	materials  Materials
	modulePath string
}

func loadCIInstallation(policyRoot string) (ciInstallation, error) {
	var in ciInstallation
	root := filepath.Dir(policyRoot)
	lock, err := regularFile(root, "TOOLS.lock.json")
	if err != nil {
		return in, err
	}
	if in.materials, in.manifest, err = computeMaterials(policyRoot, lock); err != nil {
		return in, err
	}
	in.lock, in.modulePath = lock, filepath.Join(root, "node_modules")
	return in, nil
}

// RunCIRequest is `s5check --ci-request-stdin`: it binds the request to the
// image's pinned material and the fetched refs, then runs CheckS5Candidate with
// the protected static checker and the separate behavior process.
func RunCIRequest(ctx context.Context, req CIRequest) (ContentEvidence, error) {
	key, err := candidateKey(req)
	if err != nil {
		return ContentEvidence{}, err
	}
	in, err := loadCIInstallation(req.PolicyRoot)
	if err != nil {
		return refusal(key, "S5_EVIDENCE_MISSING", "protected checker installation: "+err.Error()), nil
	}
	node, err := trustedNode()
	if err != nil {
		return refusal(key, "S5_EVIDENCE_MISSING", err.Error()), nil
	}
	policy := TrustedPolicy{Manifest: in.manifest, ToolsLock: in.lock,
		Checker:  NodeChecker{NodeBinary: node, Script: filepath.Join(req.PolicyRoot, "ts", "check.cjs"), ScriptDigest: SHA256(embeddedCheckJS), ModulePath: in.modulePath, Timeout: 2 * time.Minute},
		Behavior: NodeBehaviorRunner{NodeBinary: node, PolicyRoot: req.PolicyRoot, ModulePath: in.modulePath, Manifest: in.manifest, Timeout: 90 * time.Second}}
	return runCI(ctx, req, key, in.materials, policy), nil
}

func trustedNode() (string, error) {
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if !filepath.IsAbs(dir) {
			continue
		}
		candidate := filepath.Join(dir, "node")
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", errors.New("trusted Node runtime is not installed")
}

// runCI holds the request bindings. The checker and behavior runner are the
// protected ones built by RunCIRequest; this seam is unexported.
func runCI(ctx context.Context, req CIRequest, key CandidateKey, pinned Materials, policy TrustedPolicy) ContentEvidence {
	k := req.Key
	for _, pair := range [][3]string{{"profile", pinned.ProfileDigest, k.ProfileDigest}, {"toolchain", pinned.ToolchainDigest, k.ToolchainDigest},
		{"callers", pinned.CallersDigest, k.CallersDigest}, {"types", pinned.TypesDigest, k.TypesDigest}, {"tests", pinned.TestsDigest, k.TestsDigest}, {"UI", pinned.UISHA, k.UISHA}} {
		if pair[1] == "" || pair[1] != pair[2] {
			return refusal(key, "S5_BINDING", pair[0]+" material differs from the image's pinned material")
		}
	}
	platform := GitTreeReader{Directory: req.PlatformGitDir, Bare: true}
	for ref, want := range map[string]string{"base": k.Base, "a": k.A, "m0": k.M0, "revision": k.R} {
		if got, err := platform.ResolveCommitRef(ctx, ref); err != nil || got != want {
			return refusal(key, "S5_BINDING", "fetched refs/s5/"+ref+" is not the key's commit")
		}
	}
	ui := GitTreeReader{Directory: req.UIGitDir, Bare: true}
	if got, err := ui.ResolveCommitRef(ctx, "ui"); err != nil || got != k.UISHA {
		return refusal(key, "S5_BINDING", "fetched refs/s5/ui is not the pinned UI revision")
	}
	return CheckS5Candidate(ctx, platform, policy, key)
}
