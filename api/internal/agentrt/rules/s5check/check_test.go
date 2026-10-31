package s5check

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

type memoryReader struct {
	trees   map[string]TreeSnapshot
	patches map[string]Patch
	reads   int
}

func (r *memoryReader) ReadTree(_ context.Context, sha string) (TreeSnapshot, error) {
	r.reads++
	tree, ok := r.trees[sha]
	if !ok {
		return tree, errors.New("missing commit")
	}
	return tree, nil
}
func (r *memoryReader) ReadPatch(_ context.Context, a, b string) (Patch, error) {
	p, ok := r.patches[a+":"+b]
	if !ok {
		return p, errors.New("missing patch")
	}
	return p, nil
}

type checkerFunc func(context.Context, StaticRequest) (StaticResult, error)

func (f checkerFunc) Check(ctx context.Context, r StaticRequest) (StaticResult, error) {
	return f(ctx, r)
}

type behaviorFunc func(context.Context, TreeSnapshot, CandidateKey) (BehaviorResult, error)

func (f behaviorFunc) Run(ctx context.Context, t TreeSnapshot, k CandidateKey) (BehaviorResult, error) {
	return f(ctx, t, k)
}

const fixturePath = "console/src/lib/patrol/cronSchedule.ts"

func checkerFixture(t *testing.T) (*memoryReader, TrustedPolicy, CandidateKey) {
	t.Helper()
	sha := func(n int) string { return fmt.Sprintf("%040x", n) }
	digest := strings.Repeat("a", 64)
	manifest := json.RawMessage(`{"typescript":"5.9.3","files":[{"path":"` + fixturePath + `","language":"ts","profileVersion":1}]}`)
	pd, err := Digest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	lock := json.RawMessage(`{"typescript":{"version":"5.9.3"}}`)
	toolchain, err := ToolchainDigest(lock)
	if err != nil {
		t.Fatal(err)
	}
	k := CandidateKey{Repo: "bifrost-platform", Base: sha(1), A: sha(2), M0: sha(3), R: sha(4), BaselineTree: sha(13), ResultTree: sha(14), RuleVersion: 3, RuleDigest: digest, CheckerImageDigest: digest, PolicyDigest: pd, ToolchainDigest: toolchain, CallersDigest: digest, TypesDigest: digest, TestsDigest: digest, UISHA: sha(5), CIUID: "trusted-run-uid", KeyDigest: digest, Generation: 1, ReviewBindingDigest: digest, PatchEvidenceDigest: digest}
	reader := &memoryReader{trees: map[string]TreeSnapshot{}, patches: map[string]Patch{}}
	for i := 1; i <= 4; i++ {
		blob := sha(21)
		content := "export function f(){return 1}"
		if i%2 == 0 {
			blob = sha(22)
			content = "export function f(){return 2}"
		}
		parents := []string{}
		if i%2 == 0 {
			parents = []string{sha(i - 1)}
		}
		reader.trees[sha(i)] = TreeSnapshot{Commit: sha(i), Tree: sha(i + 10), Parents: parents, Files: map[string]TreeEntry{fixturePath: {Content: content, Blob: blob, Mode: "100644"}}}
	}
	reader.patches[k.Base+":"+k.A] = Patch{Bytes: []byte("original patch"), StableID: sha(99)}
	reader.patches[k.M0+":"+k.R] = Patch{Bytes: []byte("replayed patch with different context"), StableID: sha(99)}
	p := TrustedPolicy{Manifest: manifest, ToolsLock: lock, Checker: checkerFunc(func(_ context.Context, r StaticRequest) (StaticResult, error) {
		if r.Schema != 2 || r.Baseline.Commit != k.M0 || r.Result.Commit != k.R {
			t.Fatal("checker received unbound input")
		}
		return StaticResult{Findings: []Finding{}, PolicyDigest: pd, CallersDigest: digest, TypesDigest: digest, TestsDigest: digest}, nil
	}), Behavior: behaviorFunc(func(_ context.Context, tree TreeSnapshot, key CandidateKey) (BehaviorResult, error) {
		return BehaviorResult{Passed: true, ResultTree: tree.Tree, TestsDigest: key.TestsDigest}, nil
	})}
	return reader, p, k
}

func hasCode(e ContentEvidence, code string) bool {
	for _, f := range e.Findings {
		if f.Code == code {
			return true
		}
	}
	return false
}

func TestCandidateRequiresAllTrustedStages(t *testing.T) {
	r, p, k := checkerFixture(t)
	e := CheckS5Candidate(context.Background(), r, p, k)
	if !e.Passed || len(e.Findings) > 0 || len(e.Files) != 1 || r.reads != 4 {
		t.Fatalf("valid fixture: %+v, reads=%d", e, r.reads)
	}
	p.Behavior = nil
	e = CheckS5Candidate(context.Background(), r, p, k)
	if e.Passed || !hasCode(e, "S5_BEHAVIOR") {
		t.Fatalf("static-only became permission: %+v", e)
	}
}

func TestCandidateBindingRefusals(t *testing.T) {
	cases := []struct {
		name, code string
		mutate     func(*memoryReader, *TrustedPolicy, *CandidateKey)
	}{
		{"preparation", "S5_EVIDENCE_MISSING", func(_ *memoryReader, _ *TrustedPolicy, k *CandidateKey) { k.R = "" }},
		{"tree", "S5_BINDING", func(_ *memoryReader, _ *TrustedPolicy, k *CandidateKey) { k.ResultTree = strings.Repeat("f", 40) }},
		{"policy", "S5_BINDING", func(_ *memoryReader, _ *TrustedPolicy, k *CandidateKey) { k.PolicyDigest = strings.Repeat("f", 64) }},
		{"generation", "S5_BINDING", func(_ *memoryReader, _ *TrustedPolicy, k *CandidateKey) { k.Generation = 0 }},
		{"toolchain-key", "S5_BINDING", func(_ *memoryReader, _ *TrustedPolicy, k *CandidateKey) { k.ToolchainDigest = strings.Repeat("f", 64) }},
		{"tool-lock-missing", "S5_BINDING", func(_ *memoryReader, p *TrustedPolicy, _ *CandidateKey) { p.ToolsLock = nil }},
		{"tool-lock-other-typescript", "S5_BINDING", func(_ *memoryReader, p *TrustedPolicy, k *CandidateKey) {
			// Even a key built for this lock is refused: the lock must pin the
			// TypeScript release the profile was enrolled with.
			p.ToolsLock = json.RawMessage(`{"typescript":{"version":"5.9.2"}}`)
			d, _ := Digest(p.ToolsLock)
			k.ToolchainDigest = d
		}},
		{"parent", "S5_PATCH_MISMATCH", func(r *memoryReader, _ *TrustedPolicy, k *CandidateKey) {
			v := r.trees[k.R]
			v.Parents = []string{k.Base}
			r.trees[k.R] = v
		}},
		{"patch-id", "S5_PATCH_MISMATCH", func(r *memoryReader, _ *TrustedPolicy, k *CandidateKey) {
			v := r.patches[k.M0+":"+k.R]
			v.StableID = strings.Repeat("b", 40)
			r.patches[k.M0+":"+k.R] = v
		}},
		{"hidden-path", "S5_PATCH_MISMATCH", func(r *memoryReader, _ *TrustedPolicy, k *CandidateKey) {
			v := r.trees[k.R]
			v.Files["console/src/new.ts"] = TreeEntry{Blob: strings.Repeat("c", 40), Mode: "100644"}
			r.trees[k.R] = v
		}},
		{"not-enrolled", "S5_PATH_NOT_ENROLLED", func(_ *memoryReader, p *TrustedPolicy, _ *CandidateKey) {
			p.CandidatePaths = []string{"console/src/neighbor.ts"}
		}},
		{"behavior-tree", "S5_BEHAVIOR", func(_ *memoryReader, p *TrustedPolicy, _ *CandidateKey) {
			p.Behavior = behaviorFunc(func(_ context.Context, _ TreeSnapshot, k CandidateKey) (BehaviorResult, error) {
				return BehaviorResult{Passed: true, ResultTree: k.BaselineTree, TestsDigest: k.TestsDigest}, nil
			})
		}},
		{"behavior-error", "S5_BEHAVIOR", func(_ *memoryReader, p *TrustedPolicy, _ *CandidateKey) {
			p.Behavior = behaviorFunc(func(context.Context, TreeSnapshot, CandidateKey) (BehaviorResult, error) {
				return BehaviorResult{}, errors.New("timeout")
			})
		}},
		{"checker-error", "S5_EVIDENCE_MISSING", func(_ *memoryReader, p *TrustedPolicy, _ *CandidateKey) {
			p.Checker = checkerFunc(func(context.Context, StaticRequest) (StaticResult, error) {
				return StaticResult{}, errors.New("missing compiler")
			})
		}},
		{"checker-digest", "S5_BINDING", func(_ *memoryReader, p *TrustedPolicy, _ *CandidateKey) {
			p.Checker = checkerFunc(func(context.Context, StaticRequest) (StaticResult, error) {
				return StaticResult{Findings: []Finding{}}, nil
			})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, p, k := checkerFixture(t)
			tc.mutate(r, &p, &k)
			e := CheckS5Candidate(context.Background(), r, p, k)
			if e.Passed || !hasCode(e, tc.code) {
				t.Fatalf("wanted %s: %+v", tc.code, e)
			}
		})
	}
}

func TestGoDisabledBeforeAnyProfileOrTreeRead(t *testing.T) {
	for _, source := range []string{"func f() int { return 1 }", "env.OpsToken()", "reader.Read()", "time.Unix(0,0).Year()", "strconv.Atoi(s)", "time.Parse(layout,s)"} {
		t.Run(source, func(t *testing.T) {
			// The candidate Go source is present in R; the route must refuse it
			// without reading any tree, whatever the source does.
			const goPath = "api/internal/example.go"
			r := &memoryReader{trees: map[string]TreeSnapshot{"R": {Commit: "R", Files: map[string]TreeEntry{goPath: {Content: "package example\n\nfunc f() { " + source + " }\n", Mode: "100644"}}}}}
			e := CheckS5Candidate(context.Background(), r, TrustedPolicy{CandidatePaths: []string{goPath}, Manifest: json.RawMessage(`not json`)}, CandidateKey{R: "R"})
			if e.Passed || len(e.Findings) != 1 || e.Findings[0].Code != "S5_LANGUAGE_DISABLED" || r.reads != 0 {
				t.Fatalf("Go route: %+v", e)
			}
		})
	}
}

func TestEnrolledEndpointKindsCannotDisappear(t *testing.T) {
	for _, kind := range []string{"delete", "add", "executable", "symlink", "binary"} {
		t.Run(kind, func(t *testing.T) {
			r, p, k := checkerFixture(t)
			for _, pair := range [][2]string{{k.Base, k.A}, {k.M0, k.R}} {
				before, after := r.trees[pair[0]], r.trees[pair[1]]
				entry := after.Files[fixturePath]
				switch kind {
				case "delete":
					delete(after.Files, fixturePath)
				case "add":
					delete(before.Files, fixturePath)
				case "executable":
					entry.Mode = "100755"
					after.Files[fixturePath] = entry
				case "symlink":
					entry.Mode = "120000"
					after.Files[fixturePath] = entry
				case "binary":
					entry.Binary = true
					after.Files[fixturePath] = entry
				}
				r.trees[pair[0]], r.trees[pair[1]] = before, after
			}
			e := CheckS5Candidate(context.Background(), r, p, k)
			if e.Passed || !hasCode(e, "S5_CHANGE_KIND") {
				t.Fatalf("%s escaped: %+v", kind, e)
			}
		})
	}
}

func TestStaticEvidenceCanNeverPassAuthorization(t *testing.T) {
	r, p, k := checkerFixture(t)
	e, err := CheckStaticTrees(context.Background(), r, p, k.M0, k.R)
	if err != nil || e.Passed || !e.StaticOnly || len(e.Findings) > 0 {
		t.Fatalf("static evidence: %+v %v", e, err)
	}
}

func TestCanonicalJSONAndDigest(t *testing.T) {
	v := map[string]any{"z": []any{2, 1}, "a": "<>&\u2028\\u2028", "n": uint64(18446744073709551615)}
	raw, err := CanonicalJSON(v)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\"a\":\"<>&\u2028\\\\u2028\",\"n\":18446744073709551615,\"z\":[2,1]}"
	if string(raw) != want {
		t.Fatalf("canonical\n got %s\nwant %s", raw, want)
	}
	a, _ := Digest(json.RawMessage(`{"b":2,"a":1}`))
	b, _ := Digest(json.RawMessage(`{ "a":1, "b":2 }`))
	if a != b || a != SHA256([]byte(`{"a":1,"b":2}`)) {
		t.Fatal("digest is not canonical SHA-256")
	}
	if _, err = CanonicalJSON(json.RawMessage(`{invalid}`)); err == nil {
		t.Fatal("malformed JSON accepted")
	}
}
