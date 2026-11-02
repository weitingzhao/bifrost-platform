package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/debtimport"
)

type historyFixture struct {
	t   *testing.T
	dir string
}

func newHistoryFixture(t *testing.T, root, name string) historyFixture {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Join(dir, "agent-config"), 0700); err != nil {
		t.Fatal(err)
	}
	f := historyFixture{t, dir}
	f.git("init", "-b", "main")
	return f
}
func (f historyFixture) git(args ...string) string {
	f.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", f.dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=Importer Test", "GIT_AUTHOR_EMAIL=importer@example.test", "GIT_COMMITTER_NAME=Importer Test", "GIT_COMMITTER_EMAIL=importer@example.test")
	out, err := cmd.CombinedOutput()
	if err != nil {
		f.t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
func (f historyFixture) commit(body, message string) string {
	f.t.Helper()
	if err := os.WriteFile(filepath.Join(f.dir, "agent-config", "TECH_DEBT.md"), []byte(body), 0600); err != nil {
		f.t.Fatal(err)
	}
	f.git("add", "agent-config/TECH_DEBT.md")
	f.git("commit", "-m", message)
	return f.git("rev-parse", "HEAD")
}
func TestFloorUsesRemovedBranchHeadingsMainTrailersAndReservations(t *testing.T) {
	root := t.TempDir()
	infra := newHistoryFixture(t, root, "infra")
	infra.commit("### TD-9\nBody mentions TD-99999, not a heading.\n", "Seed")
	infra.git("checkout", "-b", "retired")
	introduced := infra.commit("### TD-9\n### TD-510\n", "Add branch-only heading")
	removed := infra.commit("### TD-9\n", "Remove heading")
	infra.git("checkout", "main")
	other := newHistoryFixture(t, root, "other")
	trailerCommit := other.commit("not a ledger\n", "Fix TD-99998 in subject\n\nNot a trailer: Work: TD-99997\n\nWork: W-31, TD-520")
	other.git("checkout", "-b", "unmerged")
	other.commit("changed\n", "Unmerged work\n\nWork: TD-99000")
	other.git("checkout", "main")
	h, err := newGitHistory(infra.dir)
	if err != nil {
		t.Fatal(err)
	}
	sources, err := collectFloors(h, root, "TD-530,TD-529")
	if err != nil {
		t.Fatal(err)
	}
	maxID := 0
	for _, s := range sources {
		maxID = max(maxID, tdNumber(s.DebtID))
	}
	if maxID != 530 {
		t.Fatalf("floor %d: %+v", maxID, sources)
	}
	if !slices.ContainsFunc(sources, func(s debtimport.FloorSource) bool {
		return s.Kind == "infra_history_heading" && s.DebtID == "TD-510" && (s.Commit == introduced || s.Commit == removed)
	}) {
		t.Fatal("removed unmerged heading lost")
	}
	if !slices.ContainsFunc(sources, func(s debtimport.FloorSource) bool {
		return s.Kind == "main_work_trailer" && s.Repository == "other" && s.DebtID == "TD-520" && s.Commit == trailerCommit
	}) {
		t.Fatal("main trailer evidence lost")
	}
	if _, err = collectFloors(h, root, "TD-4oops"); err == nil {
		t.Fatal("invalid reservation accepted")
	}
}
func TestFloorMainMustBePresentAndReservedMustBeExplicit(t *testing.T) {
	root := t.TempDir()
	infra := newHistoryFixture(t, root, "infra")
	infra.commit("### TD-489\n", "Seed")
	h, err := newGitHistory(infra.dir)
	if err != nil {
		t.Fatal(err)
	}
	sources, err := collectFloors(h, root, "")
	if err != nil {
		t.Fatal(err)
	}
	if sources[len(sources)-1].Kind != "reserved_unspecified" {
		t.Fatal("missing reservations claimed complete")
	}
	sources, err = collectFloors(h, root, "none")
	if err != nil {
		t.Fatal(err)
	}
	if sources[len(sources)-1].Kind != "reserved" {
		t.Fatal("explicit no reservations lost")
	}
	infra.git("branch", "-m", "other")
	if _, err = h.mainFloors(); err == nil {
		t.Fatal("missing main silently accepted")
	}
}

func TestHistoricalHeadingsIncludeMergeOnlyAndEarlierLocations(t *testing.T) {
	root := t.TempDir()
	infra := newHistoryFixture(t, root, "infra")
	infra.commit("### TD-9\n", "Seed")
	infra.git("checkout", "-b", "side")
	infra.commit("### TD-9\nside\n", "Side change")
	infra.git("checkout", "main")
	infra.git("merge", "--no-ff", "--no-commit", "side")
	merge := infra.commit("### TD-9\n### TD-540\n", "Merge adds unique heading")
	infra.git("mv", "agent-config/TECH_DEBT.md", "TECH_DEBT.md")
	infra.git("commit", "-m", "Move ledger")
	if err := os.WriteFile(filepath.Join(infra.dir, "TECH_DEBT.md"), []byte("### TD-550\n"), 0600); err != nil {
		t.Fatal(err)
	}
	infra.git("add", "TECH_DEBT.md")
	infra.git("commit", "-m", "Old location heading")
	h, err := newGitHistory(infra.dir)
	if err != nil {
		t.Fatal(err)
	}
	source, err := h.headingFloor()
	if err != nil {
		t.Fatal(err)
	}
	if source.DebtID != "TD-550" {
		t.Fatalf("earlier location omitted: %+v", source)
	}
	at, err := h.FirstSeen("TD-540")
	if err != nil || at.IsZero() {
		t.Fatalf("merge-only heading lost (%s): %v", merge, err)
	}
	if _, err = h.FirstSeen("TD-550"); err == nil {
		t.Fatal("FirstSeen unexpectedly searched outside documented path")
	}
}
