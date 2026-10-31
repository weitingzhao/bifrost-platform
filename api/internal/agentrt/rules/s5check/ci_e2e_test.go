package s5check

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCIRequestEndToEndOnRealTree runs the production --ci-request-stdin path
// (protected static checker + separate behavior process) on the real platform
// tree: M0 is this checkout's HEAD, R one real P1 edit, and a guard deletion is
// the negative. It needs the trusted declaration installation the manifest's
// toolchain was enrolled against (TypeScript 5.9.3, Console declarations and
// @bifrost/ui dist), supplied as S5_TRUSTED_NODE_PATH; without it the test is
// skipped and says so — it is never reported as a pass.
func TestCIRequestEndToEndOnRealTree(t *testing.T) {
	modules := os.Getenv("S5_TRUSTED_NODE_PATH")
	if modules == "" || !filepath.IsAbs(modules) {
		t.Skip("S5_TRUSTED_NODE_PATH (absolute trusted node_modules) not set; end-to-end CI check not run")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node is not installed")
	}
	root, policyRoot, _ := installation(t)
	if err := os.Symlink(modules, filepath.Join(root, "node_modules")); err != nil {
		t.Fatal(err)
	}
	here, _ := filepath.Abs(".")
	head := tempGit(t, here, "rev-parse", "HEAD")
	objects := tempGit(t, here, "rev-parse", "--path-format=absolute", "--git-common-dir") + "/objects"
	tmp := t.TempDir()
	platformDir := filepath.Join(tmp, "platform.git")
	tempGit(t, tmp, "init", "--bare", "--quiet", "--template=", platformDir)
	writeFile(t, filepath.Join(platformDir, "objects/info/alternates"), objects+"\n")
	const c1 = "console/src/lib/patrol/cronSchedule.ts"
	raw, err := exec.Command("git", "--git-dir="+platformDir, "cat-file", "blob", head+":"+c1).Output()
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	commit := func(from, to string) (string, string) {
		edited := strings.Replace(source, from, to, 1)
		if edited == source {
			t.Fatalf("edit %q did not apply", from)
		}
		blobFile := filepath.Join(tmp, "blob")
		writeFile(t, blobFile, edited)
		blob := tempGit(t, platformDir, "hash-object", "-w", blobFile)
		index := filepath.Join(tmp, "index-"+blob)
		git := func(args ...string) string {
			cmd := exec.Command("git", args...)
			cmd.Dir = platformDir
			cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=/dev/null", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_INDEX_FILE=" + index,
				"GIT_AUTHOR_NAME=S5 test", "GIT_AUTHOR_EMAIL=s5@example.invalid", "GIT_COMMITTER_NAME=S5 test", "GIT_COMMITTER_EMAIL=s5@example.invalid"}
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("git %v: %v", args, err)
			}
			return strings.TrimSpace(string(out))
		}
		git("read-tree", head)
		git("update-index", "--cacheinfo", "100644,"+blob+","+c1)
		tree := git("write-tree")
		return git("commit-tree", tree, "-p", head, "-m", "R"), tree
	}
	ui := filepath.Join(tmp, "ui")
	if err := os.MkdirAll(ui, 0o755); err != nil {
		t.Fatal(err)
	}
	tempGit(t, ui, "init", "--template=", "--quiet", ".")
	writeFile(t, filepath.Join(ui, "index.ts"), "export const ui = 1\n")
	tempGit(t, ui, "add", "index.ts")
	tempGit(t, ui, "commit", "--quiet", "-m", "ui")
	uiSHA := tempGit(t, ui, "rev-parse", "HEAD")
	uiDir := filepath.Join(tmp, "ui.git")
	tempGit(t, tmp, "clone", "--bare", "--quiet", ui, uiDir)
	tempGit(t, uiDir, "update-ref", "refs/s5/ui", uiSHA)
	writeFile(t, filepath.Join(policyRoot, UIRevisionFile), `{"uiSHA":"`+uiSHA+`"}`)
	lock, err := os.ReadFile(filepath.Join(root, "TOOLS.lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	pinned, err := ComputeMaterials(policyRoot, lock)
	if err != nil {
		t.Fatal(err)
	}
	baselineTree := tempGit(t, platformDir, "rev-parse", head+"^{tree}")
	for _, tc := range []struct {
		name, from, to, code string
		pass                 bool
	}{
		{"P1_real_duration_rewrite", "  const secs = totalSec % 60", "  const remainder = totalSec % 60\n  const secs = remainder", "", true},
		{"required_date_guard_deleted", "  if (!Number.isFinite(t)) return '—'\n", "", "S5_REQUIRED_GUARD", false},
		// Reviews A-1'/A-1'': containers are written only through the allocating binding.
		{"N1_conditional_alias_push_join", "  const totalSec =", "  const arr: (string | Date)[] = ['a']\n  const alias = ms === 1 ? arr : arr\n  alias.push(new Date(0))\n  if (ms === 1) return arr.join('')\n  const totalSec =", "S5_MUTATION_ESCAPE", false},
		{"N2_if_merge_splits_alias_push_join", "  const totalSec =", "  const arr: (string | Date)[] = ['a']\n  const alias = arr\n  if (ms === 2) arr.push('b')\n  alias.push(new Date(0))\n  if (ms === 1) return arr.join('')\n  const totalSec =", "S5_MUTATION_ESCAPE", false},
		{"R1_merged_unknown_element_push", "  const totalSec =", "  const arr: (string | Date)[] = ['a']\n  const dates: (string | Date)[] = [new Date(0)]\n  const alias = ms === 1 ? arr : dates\n  alias.push(new Date(0))\n  if (ms === 1) return arr.join('')\n  const totalSec =", "S5_MUTATION_ESCAPE", false},
		{"R2_let_reassigned_alias_push", "  const totalSec =", "  const arr: (string | Date)[] = ['a']\n  const dates: (string | Date)[] = [new Date(0)]\n  let alias = dates\n  if (ms === 1) alias = arr\n  alias.push(new Date(0))\n  if (ms === 1) return arr.join('')\n  const totalSec =", "S5_MUTATION_ESCAPE", false},
		{"R3_nested_merged_element_push", "  const totalSec =", "  const arr: (string | Date)[] = ['a']\n  const outer: (string | Date)[][] = [arr, [new Date(0)]]\n  outer[0].push(new Date(0))\n  if (ms === 1) return arr.join('')\n  const totalSec =", "S5_MUTATION_ESCAPE", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, resultTree := commit(tc.from, tc.to)
			for ref, sha := range map[string]string{"base": head, "a": r, "m0": head, "revision": r} {
				tempGit(t, platformDir, "update-ref", "refs/s5/"+ref, sha)
			}
			k := ciTestKey()
			k.Base, k.A, k.M0, k.R, k.BaselineTree, k.ResultTree, k.UISHA = head, r, head, r, baselineTree, resultTree, uiSHA
			k.ProfileDigest, k.ToolchainDigest, k.CallersDigest, k.TypesDigest, k.TestsDigest = pinned.ProfileDigest, pinned.ToolchainDigest, pinned.CallersDigest, pinned.TypesDigest, pinned.TestsDigest
			req, err := DecodeCIRequest(ciRequestBytes(t, CIRequest{Schema: 2, Key: k, CIUID: testCIUID, PlatformGitDir: platformDir, UIGitDir: uiDir, PolicyRoot: policyRoot}))
			if err != nil {
				t.Fatal(err)
			}
			e, err := RunCIRequest(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			if e.Passed != tc.pass {
				t.Fatalf("passed=%v, findings %+v", e.Passed, e.Findings)
			}
			if !tc.pass && !hasCode(e, tc.code) {
				t.Fatalf("refusal without %s: %+v", tc.code, e.Findings)
			}
			raw, _ := CanonicalJSON(e)
			key, _ := k.Canonical()
			t.Logf("evidence %s", raw)
			t.Logf("key %s", key)
		})
	}
}
