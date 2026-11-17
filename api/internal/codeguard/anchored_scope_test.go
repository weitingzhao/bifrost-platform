package codeguard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
)

// onceAnchored is every file of this repository that the release policy's
// no_trust_anchor_change rule named one by one on 2026-11-17 (main da847b47),
// and where that file is today. A file that moves gets its new path written
// in `now`; the entry is never deleted while the file exists. The test below
// holds each `now` path to the path table in bifrost-trade-infra: a file that
// was protected stays protected wherever it goes.
//
// Origin: 2026-11-17. The table named these files by path, so moving one into
// a sub-package would silently take it out of protection (duty/detours.md,
// "名单按文件路径点名 → 搬文件即掉出保护"). The fix is to name directories; this
// list is the proof that the protected set only grows.
var onceAnchored = []struct{ was, now string }{
	{"api/internal/server/actions_wire.go", "api/internal/server/actions_wire.go"},
	{"api/internal/server/release_policy_sources.go", "api/internal/server/release_policy_sources.go"},
	{"api/internal/server/release_policy_wire.go", "api/internal/server/release_policy_wire.go"},
	{"api/internal/server/release_policy_pins_test.go", "api/internal/server/release_policy_pins_test.go"},
	{"api/internal/server/release_policy_records_test.go", "api/internal/server/release_policy_records_test.go"},
	{"api/internal/server/release_policy_sources_test.go", "api/internal/server/release_policy_sources_test.go"},
	{"api/internal/server/release_policy_test.go", "api/internal/server/release_policy_test.go"},
	{"api/internal/delivery/release_policy_diff.go", "api/internal/delivery/release_policy_diff.go"},
	{"api/internal/delivery/release_policy_facts.go", "api/internal/delivery/release_policy_facts.go"},
	{"api/internal/delivery/release_policy_tree.go", "api/internal/delivery/release_policy_tree.go"},
	{"api/internal/delivery/release_policy_diff_test.go", "api/internal/delivery/release_policy_diff_test.go"},
	{"api/internal/delivery/release_policy_facts_test.go", "api/internal/delivery/release_policy_facts_test.go"},
	{"api/internal/delivery/release_policy_tree_test.go", "api/internal/delivery/release_policy_tree_test.go"},
	{"api/internal/delivery/release_window.go", "api/internal/delivery/release_window.go"},
	{"api/internal/delivery/window_store.go", "api/internal/delivery/window_store.go"},
	{"api/internal/delivery/window_handler.go", "api/internal/delivery/window_handler.go"},
	{"api/internal/delivery/run_window.go", "api/internal/delivery/run_window.go"},
}

// anchorPatterns reads the no_trust_anchor_change patterns that apply to this
// repository from the path table of the bifrost-trade-infra checkout next to
// it (the layout of the workspace and of the CI run).
func anchorPatterns(t *testing.T, root string) []string {
	t.Helper()
	table := filepath.Join(filepath.Dir(root), "bifrost-trade-infra", "agent-config", "release-policy", "paths.json")
	raw, err := os.ReadFile(table)
	if err != nil {
		if os.Getenv("AGENTRT_TEST_REQUIRED") == "1" {
			t.Fatalf("the path table is not readable and this run requires it: %v", err)
		}
		t.Skipf("no bifrost-trade-infra checkout beside this one (%v); the CI run has it", err)
	}
	var doc struct {
		Paths map[string][]struct {
			Repo  string   `json:"repo"`
			Paths []string `json:"paths"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("%s: %v", table, err)
	}
	var out []string
	for _, entry := range doc.Paths["no_trust_anchor_change"] {
		if entry.Repo == "*" || entry.Repo == "bifrost-platform" {
			out = append(out, entry.Paths...)
		}
	}
	if len(out) == 0 {
		t.Fatalf("%s names no no_trust_anchor_change path for bifrost-platform", table)
	}
	return out
}

// uncovered lists the paths no pattern matches, judged by the matcher the
// platform itself judges a release with.
func uncovered(paths, patterns []string) []string {
	var out []string
	for _, p := range paths {
		hit := false
		for _, pattern := range patterns {
			if releasepolicy.PathMatches(p, pattern) {
				hit = true
				break
			}
		}
		if !hit {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// TestOnceAnchoredStaysAnchored: every file the path table once named is
// still in the tree at the path this list gives and still matched by the
// table.
//
// Origin: 2026-11-17, see onceAnchored.
func TestOnceAnchoredStaysAnchored(t *testing.T) {
	root := repoRootForAnchors(t)
	patterns := anchorPatterns(t, root)
	var now []string
	for _, f := range onceAnchored {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(f.now))); err != nil {
			t.Errorf("%s (was %s) is not in the tree: if it moved, write its new path here; if it was deleted, say so in the commit and remove the entry with the Owner's signature on that release", f.now, f.was)
			continue
		}
		now = append(now, f.now)
	}
	if left := uncovered(now, patterns); len(left) > 0 {
		t.Errorf("these files were protected by the release policy's path table and no pattern of it matches them now:\n  %s\nadd their directory to bifrost-trade-infra/agent-config/release-policy/paths.json and have the policy signed before the move lands",
			strings.Join(left, "\n  "))
	}
}

// TestUncoveredFindsAMovedFile is the positive control of
// TestOnceAnchoredStaysAnchored: a protected file moved to a directory the
// table does not name is reported, and the same file at its old path is not.
//
// Origin: 2026-11-17, the control of the check above.
func TestUncoveredFindsAMovedFile(t *testing.T) {
	patterns := []string{"api/internal/delivery/release_window.go", "api/internal/delivery/releasewindow/**"}
	got := uncovered([]string{
		"api/internal/delivery/release_window.go",
		"api/internal/delivery/releasewindow/window.go",
		"api/internal/delivery/elsewhere/release_window.go",
	}, patterns)
	if len(got) != 1 || got[0] != "api/internal/delivery/elsewhere/release_window.go" {
		t.Fatalf("uncovered = %v, want only the file under elsewhere/", got)
	}
}

// repoRootForAnchors is the bifrost-platform checkout this test runs in.
func repoRootForAnchors(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "config", "domain-ownership.json")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no bifrost-platform root above the test's directory")
		}
		dir = parent
	}
}
