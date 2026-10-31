package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules/s5check"
)

const policySource = "../../internal/agentrt/rules/s5check"

// imageRoot lays out the protected files as the image does:
// <root>/s5check/{policies,ts,testdata} and <root>/TOOLS.lock.json.
func imageRoot(t *testing.T, uiSHA string) (policyRoot, lockPath string, golden map[string]json.RawMessage) {
	t.Helper()
	root := t.TempDir()
	policyRoot = filepath.Join(root, "s5check")
	for _, dir := range []string{"policies", "ts", "testdata"} {
		src := filepath.Join(policySource, dir)
		err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() && info.Name() == "node_modules" {
				return filepath.SkipDir
			}
			rel, _ := filepath.Rel(policySource, p)
			target := filepath.Join(policyRoot, rel)
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
	if err := os.WriteFile(filepath.Join(policyRoot, s5check.UIRevisionFile), []byte(`{"uiSHA":"`+uiSHA+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(policySource, "testdata/ci-materials-golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	lockPath = filepath.Join(root, "TOOLS.lock.json")
	if err = os.WriteFile(lockPath, golden["toolsLock"], 0o644); err != nil {
		t.Fatal(err)
	}
	return policyRoot, lockPath, golden
}

func TestCIMaterialsModeEmitsCanonicalPinnedMaterial(t *testing.T) {
	ui := strings.Repeat("7", 40)
	policyRoot, lockPath, golden := imageRoot(t, ui)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--ci-materials", policyRoot, lockPath}, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	str := func(name string) string { var s string; _ = json.Unmarshal(golden[name], &s); return s }
	want := `{"callersDigest":"` + str("callersDigest") + `","profileDigest":"` + str("profileDigest") + `","testsDigest":"` + str("testsDigest") +
		`","toolchainDigest":"` + str("toolchainDigest") + `","typesDigest":"` + str("typesDigest") + `","uiSHA":"` + ui + `"}` + "\n"
	if stdout.String() != want {
		t.Fatalf("materials:\n got %s\nwant %s", stdout.String(), want)
	}
}

func TestCIMaterialsModeRefusesUnpinnedOrMalformedInput(t *testing.T) {
	policyRoot, lockPath, _ := imageRoot(t, strings.Repeat("0", 40))
	for name, args := range map[string][]string{
		"zero UI pin":   {"--ci-materials", policyRoot, lockPath},
		"relative root": {"--ci-materials", "s5check", lockPath},
		"missing lock":  {"--ci-materials", policyRoot},
		"extra arg":     {"--ci-materials", policyRoot, lockPath, "x"},
		"absent lock":   {"--ci-materials", policyRoot, filepath.Join(t.TempDir(), "TOOLS.lock.json")},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, nil, &stdout, &stderr); code == 0 || stdout.Len() != 0 {
			t.Errorf("%s: exit %d, stdout %q", name, code, stdout.String())
		}
	}
}

func TestCIRequestModeRefusesMalformedRequests(t *testing.T) {
	for name, input := range map[string]string{
		"empty":       "",
		"not json":    "schema=2",
		"wrong shape": `{"schema":2}`,
		"unknown":     `{"passed":true,"schema":2}`,
	} {
		var stdout, stderr bytes.Buffer
		if code := run([]string{"--ci-request-stdin"}, strings.NewReader(input), &stdout, &stderr); code == 0 || stdout.Len() != 0 {
			t.Errorf("%s: exit %d, stdout %q", name, code, stdout.String())
		}
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--ci-request-stdin", "extra"}, strings.NewReader(""), &stdout, &stderr); code != 2 {
		t.Fatalf("extra argument: exit %d", code)
	}
}

// Flag mode is static inspection only: the removed full mode (which could never
// pass without a behavior runner) is not reachable by any flag combination.
func TestInspectIsStaticOnly(t *testing.T) {
	base := []string{"--repo", t.TempDir(), "--policies", "manifest.json", "--checker", "check.cjs", "--checker-sha256", strings.Repeat("a", 64)}
	for name, args := range map[string][]string{
		"no trees":         base,
		"no result":        append(append([]string{}, base...), "--baseline", strings.Repeat("1", 40)),
		"removed key flag": append(append([]string{}, base...), "--key", "key.json"),
		"removed static":   append(append([]string{}, base...), "--static-only"),
		"removed lock":     append(append([]string{}, base...), "--tools-lock", "TOOLS.lock.json"),
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, nil, &stdout, &stderr); code != 2 || stdout.Len() != 0 {
			t.Errorf("%s: exit %d, stdout %q", name, code, stdout.String())
		}
	}
}
