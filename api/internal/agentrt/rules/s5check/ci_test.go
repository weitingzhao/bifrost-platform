package s5check

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

func golden(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	raw, err := os.ReadFile("testdata/ci-materials-golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var g map[string]json.RawMessage
	if err = json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	return g
}

func goldenString(t *testing.T, g map[string]json.RawMessage, name string) string {
	t.Helper()
	var s string
	if err := json.Unmarshal(g[name], &s); err != nil || s == "" {
		t.Fatalf("golden %s: %v", name, err)
	}
	return s
}

// The same lock bytes give the same toolchainDigest in Go, in the TS checker's
// canonical encoder (ts/check.test.cjs) and in infra verify_materials.py
// (docker/s5-checker/test_verify_materials.py pins the same hex values).
func TestToolchainDigestCrossLanguageGolden(t *testing.T) {
	g := golden(t)
	for _, pair := range [][2]string{{"toolsLock", "toolchainDigest"}, {"edgeToolsLock", "edgeToolchainDigest"}} {
		got, err := ToolchainDigest(g[pair[0]])
		if err != nil || got != goldenString(t, g, pair[1]) {
			t.Fatalf("%s: %s %v", pair[0], got, err)
		}
	}
	for _, bad := range []string{``, `[]`, `{}`, `{"typescript":{}}`, `{"typescript":{"version":""}}`, `not json`} {
		if _, err := ToolchainDigest([]byte(bad)); err == nil {
			t.Fatalf("lock %q accepted", bad)
		}
	}
}

const testUISHA = "7777777777777777777777777777777777777777"

// installation copies the protected policy root (manifest, checker scripts,
// trusted suite) as the image lays it out: <root>/s5check, <root>/TOOLS.lock.json.
func installation(t *testing.T) (root, policyRoot string, lock []byte) {
	t.Helper()
	root = t.TempDir()
	policyRoot = filepath.Join(root, "s5check")
	for _, dir := range []string{"policies", "ts", "testdata"} {
		err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() && info.Name() == "node_modules" {
				return filepath.SkipDir
			}
			target := filepath.Join(policyRoot, p)
			if info.IsDir() {
				return os.MkdirAll(target, 0o755)
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			return os.WriteFile(target, data, 0o644)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(policyRoot, UIRevisionFile), `{"uiSHA":"`+testUISHA+`"}`)
	lock = golden(t)["toolsLock"]
	writeFile(t, filepath.Join(root, "TOOLS.lock.json"), string(lock))
	return root, policyRoot, lock
}

func writeFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCIMaterialsMatchCheckerGolden(t *testing.T) {
	_, policyRoot, lock := installation(t)
	got, err := ComputeMaterials(policyRoot, lock)
	if err != nil {
		t.Fatal(err)
	}
	g := golden(t)
	want := Materials{ProfileDigest: goldenString(t, g, "profileDigest"), ToolchainDigest: goldenString(t, g, "toolchainDigest"),
		CallersDigest: goldenString(t, g, "callersDigest"), TypesDigest: goldenString(t, g, "typesDigest"), TestsDigest: goldenString(t, g, "testsDigest"), UISHA: testUISHA}
	if got != want {
		t.Fatalf("materials differ from the checker's own digests:\n got %+v\nwant %+v", got, want)
	}
	if reviewed, _ := EmbeddedPolicyDigest(); got.ProfileDigest != reviewed {
		t.Fatal("profile digest is not the compiled-in manifest digest")
	}
}

// The shipped pin is the bifrost-ui revision whose built declarations the
// manifest's toolchain hashes were enrolled from (71 @bifrost/ui entries).
// Changing either side without the other must be a deliberate core change.
func TestShippedUIPinIsEnrolledRevision(t *testing.T) {
	root, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	got, err := pinnedUI(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != "c83f40b40e65e52f8592ec6b73c5f6987c116e93" {
		t.Fatalf("shipped UI pin %s is not the enrolled bifrost-ui revision", got)
	}
}

func TestCIMaterialsFailClosed(t *testing.T) {
	cases := map[string]func(t *testing.T, policyRoot string, lock *[]byte){
		"zero UI pin": func(t *testing.T, policyRoot string, _ *[]byte) {
			writeFile(t, filepath.Join(policyRoot, UIRevisionFile), `{"uiSHA":"`+strings.Repeat("0", 40)+`"}`)
		},
		"missing UI pin": func(_ *testing.T, policyRoot string, _ *[]byte) {
			os.Remove(filepath.Join(policyRoot, UIRevisionFile))
		},
		"UI pin extra field": func(t *testing.T, policyRoot string, _ *[]byte) {
			writeFile(t, filepath.Join(policyRoot, UIRevisionFile), `{"uiSHA":"`+testUISHA+`","x":1}`)
		},
		"changed behavior runner": func(t *testing.T, policyRoot string, _ *[]byte) {
			p := filepath.Join(policyRoot, behaviorScript)
			data, _ := os.ReadFile(p)
			writeFile(t, p, string(data)+"\n// candidate edit\n")
		},
		"changed trusted test copy": func(t *testing.T, policyRoot string, _ *[]byte) {
			p := filepath.Join(policyRoot, "testdata/baseline/console/src/lib/patrol/__tests__/cronSchedule.test.ts")
			data, _ := os.ReadFile(p)
			writeFile(t, p, strings.Replace(string(data), "expect", "void", 1))
		},
		"changed checker script": func(t *testing.T, policyRoot string, _ *[]byte) {
			p := filepath.Join(policyRoot, "ts/contracts.cjs")
			data, _ := os.ReadFile(p)
			writeFile(t, p, string(data)+"\n")
		},
		"symlinked checker": func(t *testing.T, policyRoot string, _ *[]byte) {
			p := filepath.Join(policyRoot, "ts/analyze.cjs")
			moved := p + ".real"
			if err := os.Rename(p, moved); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(moved, p); err != nil {
				t.Fatal(err)
			}
		},
		"other manifest": func(t *testing.T, policyRoot string, _ *[]byte) {
			p := filepath.Join(policyRoot, "policies/manifest.json")
			data, _ := os.ReadFile(p)
			writeFile(t, p, string(data)+" ")
		},
		"lock pins other TypeScript": func(_ *testing.T, _ string, lock *[]byte) {
			*lock = []byte(strings.Replace(string(*lock), `"5.9.3"`, `"5.9.2"`, 1))
		},
		"lock without TypeScript": func(_ *testing.T, _ string, lock *[]byte) {
			*lock = []byte(`{"node":{"version":"22.14.0"}}`)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			_, policyRoot, lock := installation(t)
			mutate(t, policyRoot, &lock)
			if m, err := ComputeMaterials(policyRoot, lock); err == nil {
				t.Fatalf("materials emitted: %+v", m)
			}
		})
	}
}

func ciTestKey() landing.S5CIKey {
	h := strings.Repeat("a", 64)
	return landing.S5CIKey{Schema: 2, Env: "stg", Tenant: "dev", Repo: "bifrost-platform", Task: "t_01ARZ3NDEKTSV4RRFFQ69G5FAV", Attempt: "a_01ARZ3NDEKTSV4RRFFQ69G5FAV", Op: "land:t_01ARZ3NDEKTSV4RRFFQ69G5FAV:1",
		Base: strings.Repeat("1", 40), A: strings.Repeat("2", 40), M0: strings.Repeat("3", 40), R: strings.Repeat("4", 40),
		BaselineTree: strings.Repeat("5", 40), ResultTree: strings.Repeat("6", 40), UISHA: testUISHA, RuleVersion: 3,
		RuleDigest: h, ProfileDigest: h, CheckerImageDigest: "sha256:" + strings.Repeat("1", 64), ToolchainDigest: h, CallersDigest: h, TypesDigest: h, TestsDigest: h,
		ReviewBindingDigest: h, PatchEvidenceDigest: h, Generation: 1}
}

const testCIUID = "0b5c7a1e-3f4d-4c2b-9a8e-1d2c3b4a5f60"

func ciRequestBytes(t *testing.T, req CIRequest) []byte {
	t.Helper()
	raw, err := CanonicalJSON(req)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestDecodeCIRequestIsStrict(t *testing.T) {
	valid := CIRequest{Schema: 2, Key: ciTestKey(), CIUID: testCIUID, PlatformGitDir: "/workspace/trees/platform.git", UIGitDir: "/workspace/trees/ui.git", PolicyRoot: "/opt/s5/s5check"}
	raw := ciRequestBytes(t, valid)
	if got, err := DecodeCIRequest(append(append([]byte{}, raw...), '\n')); err != nil || got != valid {
		t.Fatalf("canonical request refused: %v", err)
	}
	mutate := func(f func(*CIRequest)) []byte { r := valid; f(&r); return ciRequestBytes(t, r) }
	bad := map[string][]byte{
		"schema":         mutate(func(r *CIRequest) { r.Schema = 1 }),
		"invalid key":    mutate(func(r *CIRequest) { r.Key.Env = "dev" }),
		"unbuilt image":  mutate(func(r *CIRequest) { r.Key.CheckerImageDigest = "sha256:" + strings.Repeat("0", 64) }),
		"uid":            mutate(func(r *CIRequest) { r.CIUID = "uid" }),
		"relative path":  mutate(func(r *CIRequest) { r.PolicyRoot = "opt/s5/s5check" }),
		"unclean path":   mutate(func(r *CIRequest) { r.PlatformGitDir = "/workspace/trees/../platform.git" }),
		"pretty":         []byte(strings.Replace(string(raw), `{"ciUID"`, `{ "ciUID"`, 1)),
		"unknown field":  []byte(strings.Replace(string(raw), `{`, `{"passed":true,`, 1)),
		"duplicate":      []byte(strings.Replace(string(raw), `"ciUID":`, `"ciUID":"x","ciUID":`, 1)),
		"trailing":       append(append([]byte{}, raw...), []byte(`{}`)...),
		"key unknown":    []byte(strings.Replace(string(raw), `"key":{`, `"key":{"candidate":1,`, 1)),
		"oversize":       []byte(strings.Repeat(" ", maxCIRequestBytes+1)),
		"not json":       []byte(`schema=2`),
		"empty":          nil,
		"generation str": []byte(strings.Replace(string(raw), `"generation":1`, `"generation":"1"`, 1)),
		// Case variants of a known member (review B-2): canonical key order puts
		// "ciUID" before "ciuid" and "Generation" before "generation".
		"case variant":   []byte(strings.Replace(string(raw), `"ciUID":"`+testCIUID+`"`, `"ciUID":"`+testCIUID+`","ciuid":"11111111-2222-4333-8444-555555555555"`, 1)),
		"nested case":    []byte(strings.Replace(string(raw), `"generation":1`, `"Generation":9,"generation":1`, 1)),
		"case only":      []byte(strings.Replace(string(raw), `"ciUID":`, `"ciuid":`, 1)),
		"missing member": []byte(strings.Replace(string(raw), `,"uiGitDir":"/workspace/trees/ui.git"`, ``, 1)),
	}
	for name, input := range bad {
		if bytes.Equal(input, raw) {
			t.Fatalf("%s: mutation did not apply", name)
		}
		if _, err := DecodeCIRequest(input); err == nil {
			t.Errorf("%s: request accepted", name)
		}
	}
}

// ciRepos builds read-only-style bare object stores from the protected
// fixture snapshot: base/M0 hold the reviewed C1 source, A/R one real edit.
func ciRepos(t *testing.T) (platformDir, uiDir string, ids map[string]string) {
	t.Helper()
	dir := t.TempDir()
	work := filepath.Join(dir, "work")
	if err := os.MkdirAll(filepath.Join(work, filepath.Dir(fixturePath)), 0o755); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(filepath.Join("testdata/baseline", fixturePath))
	if err != nil {
		t.Fatal(err)
	}
	tempGit(t, work, "init", "--template=", ".")
	writeFile(t, filepath.Join(work, fixturePath), string(source))
	tempGit(t, work, "add", fixturePath)
	tempGit(t, work, "commit", "-m", "M0")
	ids = map[string]string{"base": tempGit(t, work, "rev-parse", "HEAD")}
	edited := strings.Replace(string(source), "  const secs = totalSec % 60", "  const remainder = totalSec % 60\n  const secs = remainder", 1)
	if edited == string(source) {
		t.Fatal("fixture edit did not apply")
	}
	writeFile(t, filepath.Join(work, fixturePath), edited)
	tempGit(t, work, "commit", "-am", "R")
	ids["a"] = tempGit(t, work, "rev-parse", "HEAD")
	ids["baselineTree"] = tempGit(t, work, "rev-parse", ids["base"]+"^{tree}")
	ids["resultTree"] = tempGit(t, work, "rev-parse", ids["a"]+"^{tree}")
	platformDir = filepath.Join(dir, "platform.git")
	tempGit(t, dir, "clone", "--bare", "--quiet", work, platformDir)
	for ref, sha := range map[string]string{"base": ids["base"], "a": ids["a"], "m0": ids["base"], "revision": ids["a"]} {
		tempGit(t, platformDir, "update-ref", "refs/s5/"+ref, sha)
	}
	ui := filepath.Join(dir, "ui")
	if err := os.MkdirAll(ui, 0o755); err != nil {
		t.Fatal(err)
	}
	tempGit(t, ui, "init", "--template=", ".")
	writeFile(t, filepath.Join(ui, "index.ts"), "export const ui = 1\n")
	tempGit(t, ui, "add", "index.ts")
	tempGit(t, ui, "commit", "-m", "ui")
	ids["ui"] = tempGit(t, ui, "rev-parse", "HEAD")
	uiDir = filepath.Join(dir, "ui.git")
	tempGit(t, dir, "clone", "--bare", "--quiet", ui, uiDir)
	tempGit(t, uiDir, "update-ref", "refs/s5/ui", ids["ui"])
	return platformDir, uiDir, ids
}

type ciFixture struct {
	req    CIRequest
	key    CandidateKey
	pinned Materials
	policy TrustedPolicy
}

func newCIFixture(t *testing.T) ciFixture {
	t.Helper()
	platformDir, uiDir, ids := ciRepos(t)
	manifest := json.RawMessage(`{"typescript":"5.9.3","files":[{"path":"` + fixturePath + `","language":"ts","profileVersion":1}]}`)
	lock := golden(t)["toolsLock"]
	profile, _ := Digest(manifest)
	toolchain, err := ToolchainDigest(lock)
	if err != nil {
		t.Fatal(err)
	}
	pinned := Materials{ProfileDigest: profile, ToolchainDigest: toolchain, CallersDigest: strings.Repeat("c", 64), TypesDigest: strings.Repeat("d", 64), TestsDigest: strings.Repeat("e", 64), UISHA: ids["ui"]}
	k := ciTestKey()
	k.Base, k.A, k.M0, k.R = ids["base"], ids["a"], ids["base"], ids["a"]
	k.BaselineTree, k.ResultTree, k.UISHA = ids["baselineTree"], ids["resultTree"], ids["ui"]
	k.ProfileDigest, k.ToolchainDigest, k.CallersDigest, k.TypesDigest, k.TestsDigest = pinned.ProfileDigest, pinned.ToolchainDigest, pinned.CallersDigest, pinned.TypesDigest, pinned.TestsDigest
	req := CIRequest{Schema: 2, Key: k, CIUID: testCIUID, PlatformGitDir: platformDir, UIGitDir: uiDir, PolicyRoot: "/opt/s5/s5check"}
	if _, err := DecodeCIRequest(ciRequestBytes(t, req)); err != nil {
		t.Fatal(err)
	}
	ck, err := candidateKey(req)
	if err != nil {
		t.Fatal(err)
	}
	policy := TrustedPolicy{Manifest: manifest, ToolsLock: lock,
		Checker: checkerFunc(func(_ context.Context, r StaticRequest) (StaticResult, error) {
			if r.Baseline.Commit != k.M0 || r.Result.Commit != k.R || r.Result.Files[fixturePath].Content == "" {
				t.Fatal("checker did not receive the fetched immutable trees")
			}
			return StaticResult{Findings: []Finding{}, PolicyDigest: pinned.ProfileDigest, CallersDigest: pinned.CallersDigest, TypesDigest: pinned.TypesDigest, TestsDigest: pinned.TestsDigest}, nil
		}),
		Behavior: behaviorFunc(func(_ context.Context, tree TreeSnapshot, key CandidateKey) (BehaviorResult, error) {
			return BehaviorResult{Passed: true, ResultTree: tree.Tree, TestsDigest: key.TestsDigest}, nil
		})}
	return ciFixture{req: req, key: ck, pinned: pinned, policy: policy}
}

// The evidence member set is exactly what infra runner.py validate_evidence
// requires: the key bindings with policyDigest=profileDigest, ciUID and the
// landing keyDigest, plus files/findings/passed.
func TestRunCIEmitsRunnerEvidenceShape(t *testing.T) {
	f := newCIFixture(t)
	e := runCI(context.Background(), f.req, f.key, f.pinned, f.policy)
	if !e.Passed || len(e.Findings) != 0 || len(e.Files) != 1 || e.Files[0].Path != fixturePath {
		t.Fatalf("control: %+v", e)
	}
	raw, err := CanonicalJSON(e)
	if err != nil {
		t.Fatal(err)
	}
	var members map[string]any
	if err = json.Unmarshal(raw, &members); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(members))
	for n := range members {
		names = append(names, n)
	}
	sort.Strings(names)
	want := strings.Fields("A M0 R base baselineTree callersDigest checkerImageDigest ciUID files findings generation keyDigest passed patchEvidenceDigest policyDigest repo resultTree reviewBindingDigest ruleDigest ruleVersion schema testsDigest toolchainDigest typesDigest uiSHA")
	if strings.Join(names, " ") != strings.Join(want, " ") {
		t.Fatalf("evidence members %v", names)
	}
	keyDigest, _ := f.req.Key.Digest()
	if e.KeyDigest != keyDigest || e.CIUID != testCIUID || e.PolicyDigest != f.req.Key.ProfileDigest || e.Schema != 2 || e.Generation != f.req.Key.Generation {
		t.Fatalf("evidence bindings: %+v", e.CandidateKey)
	}
}

func TestRunCIRefusesUnboundInputs(t *testing.T) {
	cases := map[string]func(t *testing.T, f *ciFixture){
		"material differs from pin": func(_ *testing.T, f *ciFixture) { f.pinned.TestsDigest = strings.Repeat("f", 64) },
		"UI differs from pin":       func(_ *testing.T, f *ciFixture) { f.pinned.UISHA = strings.Repeat("8", 40) },
		"unpinned material":         func(_ *testing.T, f *ciFixture) { f.pinned = Materials{} },
		"moved revision ref": func(t *testing.T, f *ciFixture) {
			tempGit(t, f.req.PlatformGitDir, "update-ref", "refs/s5/revision", f.req.Key.M0)
		},
		"moved source ref": func(t *testing.T, f *ciFixture) {
			tempGit(t, f.req.PlatformGitDir, "update-ref", "refs/s5/a", f.req.Key.Base)
		},
		"missing m0 ref": func(t *testing.T, f *ciFixture) {
			tempGit(t, f.req.PlatformGitDir, "update-ref", "-d", "refs/s5/m0")
		},
		"other UI ref": func(t *testing.T, f *ciFixture) {
			tempGit(t, f.req.UIGitDir, "update-ref", "-d", "refs/s5/ui")
		},
		"wrong tool lock": func(t *testing.T, f *ciFixture) {
			f.policy.ToolsLock = golden(t)["edgeToolsLock"]
		},
		"behavior not run": func(_ *testing.T, f *ciFixture) { f.policy.Behavior = nil },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := newCIFixture(t)
			mutate(t, &f)
			e := runCI(context.Background(), f.req, f.key, f.pinned, f.policy)
			if e.Passed || len(e.Findings) == 0 {
				t.Fatalf("unbound input passed: %+v", e)
			}
		})
	}
}

// Without a complete protected installation RunCIRequest refuses before any
// tree is read; it never falls back to defaults.
func TestRunCIRequestRefusesIncompleteInstallation(t *testing.T) {
	f := newCIFixture(t)
	_, policyRoot, _ := installation(t)
	writeFile(t, filepath.Join(policyRoot, UIRevisionFile), `{"uiSHA":"0000000000000000000000000000000000000000"}`)
	f.req.PolicyRoot = policyRoot
	e, err := RunCIRequest(context.Background(), f.req)
	if err != nil || e.Passed || len(e.Findings) != 1 || e.Findings[0].Code != "S5_EVIDENCE_MISSING" {
		t.Fatalf("UNBUILT installation: %+v %v", e, err)
	}
	f.req.PolicyRoot = filepath.Join(t.TempDir(), "s5check")
	if e, err = RunCIRequest(context.Background(), f.req); err != nil || e.Passed {
		t.Fatalf("missing installation: %+v %v", e, err)
	}
}
