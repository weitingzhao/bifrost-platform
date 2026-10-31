package s5check

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

const contractDisplay = "console/src/components/StatusBadge.tsx"

// contractRepos builds bare stores whose M0 holds every enrolled S5 file and
// one ordinary display file, and whose R edits exactly the given paths.
func contractRepos(t *testing.T, edits map[string]string) (platformDir, uiDir string, ids map[string]string) {
	t.Helper()
	dir := t.TempDir()
	work := filepath.Join(dir, "work")
	tempGit(t, dir, "init", "--template=", "--quiet", work)
	files := append(rules.S5Paths(), contractDisplay)
	for i, p := range files {
		if err := os.MkdirAll(filepath.Join(work, filepath.Dir(p)), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(work, p), "export const v"+string(rune('a'+i))+" = 1\n")
		tempGit(t, work, "add", p)
	}
	tempGit(t, work, "commit", "--quiet", "-m", "M0")
	ids = map[string]string{"m0": tempGit(t, work, "rev-parse", "HEAD")}
	for p, text := range edits {
		writeFile(t, filepath.Join(work, p), text)
	}
	tempGit(t, work, "commit", "--quiet", "-am", "R")
	ids["r"] = tempGit(t, work, "rev-parse", "HEAD")
	ids["baselineTree"] = tempGit(t, work, "rev-parse", ids["m0"]+"^{tree}")
	ids["resultTree"] = tempGit(t, work, "rev-parse", ids["r"]+"^{tree}")
	platformDir = filepath.Join(dir, "platform.git")
	tempGit(t, dir, "clone", "--bare", "--quiet", work, platformDir)
	// The candidate is replayed unchanged: base=M0 and A=R (as in ciRepos).
	for ref, sha := range map[string]string{"base": ids["m0"], "a": ids["r"], "m0": ids["m0"], "revision": ids["r"]} {
		tempGit(t, platformDir, "update-ref", "refs/s5/"+ref, sha)
	}
	ui := filepath.Join(dir, "ui")
	tempGit(t, dir, "init", "--template=", "--quiet", ui)
	writeFile(t, filepath.Join(ui, "index.ts"), "export const ui = 1\n")
	tempGit(t, ui, "add", "index.ts")
	tempGit(t, ui, "commit", "--quiet", "-m", "ui")
	ids["ui"] = tempGit(t, ui, "rev-parse", "HEAD")
	uiDir = filepath.Join(dir, "ui.git")
	tempGit(t, dir, "clone", "--bare", "--quiet", ui, uiDir)
	tempGit(t, uiDir, "update-ref", "refs/s5/ui", ids["ui"])
	return platformDir, uiDir, ids
}

// contractCase runs the real check.go (real Git reader, trusted-stage stubs
// as in newCIFixture) on the edit and returns landing's facts for the same
// change, the CI result carrying the checker's actual evidence, and now.
func contractCase(t *testing.T, edits map[string]string, displayText string) (landing.Facts, landing.S5CIResult, time.Time) {
	t.Helper()
	platformDir, uiDir, ids := contractRepos(t, edits)
	entries := []string{}
	for _, p := range rules.S5Paths() {
		entries = append(entries, `{"path":"`+p+`","language":"ts","profileVersion":1}`)
	}
	manifest := json.RawMessage(`{"typescript":"5.9.3","files":[` + strings.Join(entries, ",") + `]}`)
	lock := golden(t)["toolsLock"]
	profile, _ := Digest(manifest)
	toolchain, err := ToolchainDigest(lock)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 30, 13, 0, 0, 0, time.UTC)
	epoch := now.Add(-24 * time.Hour)
	changes := []rules.LandingChange{}
	paths := []string{}
	for p := range edits {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	modes := []landing.S5PathMode{}
	for _, p := range paths {
		text := "return value + 1"
		if p == contractDisplay {
			text = displayText
		}
		changes = append(changes, rules.LandingChange{Kind: "M", OldPath: p, NewPath: p, OldMode: "100644", NewMode: "100644", Added: 1, Deleted: 1, AddedText: text})
		modes = append(modes, landing.S5PathMode{OldPath: p, NewPath: p, OldMode: "100644", NewMode: "100644", Kind: "M"})
	}
	k0 := ciTestKey()
	f := landing.Facts{Environment: k0.Env, Mode: "dry_run", Tenant: k0.Tenant, Repo: k0.Repo, Task: k0.Task, Attempt: k0.Attempt, A: ids["r"], Base: ids["m0"],
		MissionState: "active", StepState: "active", TaskState: "accepted", SingleCommit: true, DependenciesEffective: true,
		AcceptedAt: epoch.Add(time.Minute), MissionCreated: epoch, EnabledAt: epoch, History: landing.MainHistory{State: landing.Never},
		Changes: changes, DeployChanges: changes,
		Rule: &rules.Rule{Header: rules.Header{ID: "landing:dev", Version: 3, Effective: epoch.Add(-12 * time.Hour), Expires: epoch.Add(30 * 24 * time.Hour)}, Kind: rules.KindLanding,
			Landing: &rules.Landing{Tenant: "dev", Repos: []rules.LandingRepo{{Repo: "bifrost-platform", Allow: append([]string{"console/src/components/**/*.tsx"}, rules.S5Paths()...), STGRelease: "bifrost-deliver-platform"}},
				Limits: rules.LandingLimits{MaxFiles: 10, MaxChangedLines: 300, MaxLandingsPerDay: 5, MaxInFlightPerRepo: 1}}}}
	d := strings.Repeat("c", 64)
	f.S5Policy = landing.S5Policy{ProfileDigest: profile, CheckerImageDigest: k0.CheckerImageDigest, ToolchainDigest: toolchain, CallersDigest: d, TypesDigest: d, TestsDigest: d, UISHA: ids["ui"]}
	stable := strings.Repeat("9", 40)
	f.S5Patch = landing.PatchEvidence{A: f.A, ParentA: f.Base, Base: f.Base, SourcePatchSHA256: d, SourceStablePatchID: stable, DiffAlgorithmVersion: "git-stable-v1",
		OldNewPathsAndModes: modes, M0: ids["m0"], R: ids["r"], ParentR: ids["m0"], ReplayMatch: true, ReplayPatchSHA256: strings.Repeat("b", 64), ReplayStablePatchID: stable,
		BaselineTree: ids["baselineTree"], ResultTree: ids["resultTree"]}
	f.S5Review = landing.ReviewBinding{Repo: f.Repo, Task: f.Task, AcceptedAttempt: f.Attempt, A: f.A, Base: f.Base, AcceptanceVerificationID: "v_accept", ImplementerAttempt: f.Attempt,
		ImplementerExecutor: "exec:codex", ImplementerProvider: "openai", ReviewVerificationID: "v_review", ReviewerTask: "t_review", ReviewerAttempt: "a_review",
		ReviewerExecutor: "exec:claude", ReviewerProvider: "anthropic", Verdict: "pass", ReviewArtifactID: "sha256:" + d, ReviewArtifactSHA256: d, ReviewedPatchSHA256: d, ReviewedStablePatchID: stable}
	f.Operation = landing.Operation{Task: f.Task, Attempt: f.Attempt, A: f.A, R: ids["r"], M0: ids["m0"], Phase: "branch_pushed", PreparedAt: now.Add(-2 * time.Hour)}
	k, why := landing.NewS5CIKey(f, k0.Op, 1)
	if why != "" {
		t.Fatal(why)
	}
	kd, _ := k.Digest()
	name, _ := k.RunName()
	f.Operation.S5 = landing.S5State{Required: true, Key: k, KeyDigest: kd, UID: testCIUID}

	req := CIRequest{Schema: 2, Key: k, CIUID: testCIUID, PlatformGitDir: platformDir, UIGitDir: uiDir, PolicyRoot: "/opt/s5/s5check"}
	if _, err := DecodeCIRequest(ciRequestBytes(t, req)); err != nil {
		t.Fatal(err)
	}
	ck, err := candidateKey(req)
	if err != nil {
		t.Fatal(err)
	}
	pinned := Materials{ProfileDigest: profile, ToolchainDigest: toolchain, CallersDigest: d, TypesDigest: d, TestsDigest: d, UISHA: ids["ui"]}
	policy := TrustedPolicy{Manifest: manifest, ToolsLock: lock,
		Checker: checkerFunc(func(_ context.Context, r StaticRequest) (StaticResult, error) {
			return StaticResult{Findings: []Finding{}, PolicyDigest: profile, CallersDigest: d, TypesDigest: d, TestsDigest: d}, nil
		}),
		Behavior: behaviorFunc(func(_ context.Context, tree TreeSnapshot, key CandidateKey) (BehaviorResult, error) {
			return BehaviorResult{Passed: true, ResultTree: tree.Tree, TestsDigest: key.TestsDigest}, nil
		})}
	e := runCI(context.Background(), req, ck, pinned, policy)
	if !e.Passed || len(e.Findings) != 0 {
		t.Fatalf("checker refused the fixture: %+v", e.Findings)
	}
	// The exact bytes the runner reports, decoded strictly into landing's type.
	raw, err := CanonicalJSON(e)
	if err != nil {
		t.Fatal(err)
	}
	var content landing.S5ContentEvidence
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&content); err != nil {
		t.Fatal(err)
	}
	ci := landing.S5CIResult{Key: k, Run: name, UID: testCIUID, KeyDigest: kd, Status: "Succeeded", CreatedAt: now.Add(-time.Hour), CompletedAt: now.Add(-50 * time.Minute),
		IdentityVerified: true, Evidence: &content}
	return f, ci, now
}

func evidencePaths(ci landing.S5CIResult) []string {
	out := []string{}
	for _, file := range ci.Evidence.Files {
		out = append(out, file.Path)
	}
	return out
}

// Review P2 (2026-10-30): the checker's actual output must satisfy landing's
// verifier. Evidence names exactly the change's S5 files; missing, extra or
// mismatched files are still refused.
func TestCheckerOutputSatisfiesLandingVerifier(t *testing.T) {
	s5 := rules.S5Paths()
	one := map[string]string{s5[0]: "export const va = 2\n"}
	two := map[string]string{s5[0]: "export const va = 2\n", s5[2]: "export const vc = 2\n"}
	mixed := map[string]string{s5[1]: "export const vb = 2\n", contractDisplay: "export const ve = 2\n"}

	for name, edits := range map[string]map[string]string{"one enrolled file": one, "two enrolled files": two} {
		f, ci, now := contractCase(t, edits, "")
		want := EvidencePaths("bifrost-platform", mapsKeys(edits))
		if got := evidencePaths(ci); !slices.Equal(got, want) || len(got) != len(edits) {
			t.Fatal(name, "checker evidence files", got, want)
		}
		v, why := landing.VerifyS5Evidence(ci, f, f.S5Policy, f.S5Review)
		if why != "" {
			t.Fatal(name, why)
		}
		if why, soft := landing.AuthorizeLanding(f, v, now); why != "" || len(soft) != 0 {
			t.Fatal(name, "authorization", why, soft)
		}
	}

	// Missing: one of the two changed files has no evidence.
	f, ci, _ := contractCase(t, two, "")
	ci.Evidence.Files = ci.Evidence.Files[:1]
	if _, why := landing.VerifyS5Evidence(ci, f, f.S5Policy, f.S5Review); why != "S5_EVIDENCE_MISSING" {
		t.Fatal("missing file", why)
	}
	// Extra: an enrolled file the change does not touch.
	f, ci, _ = contractCase(t, one, "")
	ci.Evidence.Files = append(ci.Evidence.Files, landing.S5FileEvidence{Path: s5[3], BeforeBlob: ci.Evidence.Files[0].BeforeBlob, AfterBlob: ci.Evidence.Files[0].BeforeBlob})
	if _, why := landing.VerifyS5Evidence(ci, f, f.S5Policy, f.S5Review); why != "S5_BINDING" {
		t.Fatal("extra file", why)
	}
	// Mismatch: real evidence of one change offered for a different change.
	f, ci, _ = contractCase(t, one, "")
	other := s5[2]
	f.Changes[0].OldPath, f.Changes[0].NewPath = other, other
	if _, why := landing.VerifyS5Evidence(ci, f, f.S5Policy, f.S5Review); why == "" {
		t.Fatal("evidence for another change accepted")
	}

	// Mixed with an ordinary display file (design §5: S5 proves only its
	// files; the display file keeps its own display rule via
	// rules.S5Refusals). The checker's evidence names the S5 file alone.
	f, ci, now := contractCase(t, mixed, `<span className="badge">ok</span>`)
	if got := evidencePaths(ci); !slices.Equal(got, []string{s5[1]}) {
		t.Fatal("mixed evidence files", got)
	}
	v, why := landing.VerifyS5Evidence(ci, f, f.S5Policy, f.S5Review)
	if why != "" {
		t.Fatal("mixed", why)
	}
	if why, soft := landing.AuthorizeLanding(f, v, now); why != "" || len(soft) != 0 {
		t.Fatal("mixed authorization", why, soft)
	}
	// The display file is still judged as display: an effect in it keeps its
	// presentation refusal although S5 evidence verified.
	f, ci, now = contractCase(t, mixed, `fetch("/api/x")`)
	v, why = landing.VerifyS5Evidence(ci, f, f.S5Policy, f.S5Review)
	if why != "" {
		t.Fatal("mixed effect", why)
	}
	if why, soft := landing.AuthorizeLanding(f, v, now); why == "" && !slices.Contains(soft, rules.LandingPresentation) {
		t.Fatal("display effect exempted by S5", why, soft)
	}
	// Evidence that also lists the display file (the old whole-diff output)
	// is refused: it is not this change's S5 set.
	f, ci, _ = contractCase(t, mixed, `<span className="badge">ok</span>`)
	ci.Evidence.Files = append(ci.Evidence.Files, landing.S5FileEvidence{Path: contractDisplay, BeforeBlob: ci.Evidence.Files[0].BeforeBlob, AfterBlob: ci.Evidence.Files[0].AfterBlob})
	if _, why := landing.VerifyS5Evidence(ci, f, f.S5Policy, f.S5Review); why != "S5_BINDING" {
		t.Fatal("display file in evidence", why)
	}
}

// The checker's enrolled profiles and the platform's routing registry are
// one set, so EvidencePaths (rules.S5Candidate) and the profiles agree.
func TestEmbeddedProfilesAreTheLandingRegistry(t *testing.T) {
	manifest, err := os.ReadFile("policies/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := policyFiles(manifest)
	if err != nil {
		t.Fatal(err)
	}
	got := mapsKeys(profiles)
	want := slices.Clone(rules.S5Paths())
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatal("profiles", got, "registry", want)
	}
}

func mapsKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
