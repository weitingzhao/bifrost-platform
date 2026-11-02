package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestFirstSeenIncludesOtherBranchesAndExactHeading(t *testing.T) {
	dir := t.TempDir()
	git := func(date string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=Importer Test", "GIT_AUTHOR_EMAIL=importer@example.test", "GIT_COMMITTER_NAME=Importer Test", "GIT_COMMITTER_EMAIL=importer@example.test", "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return string(out)
	}
	git("2026-01-01T00:00:00Z", "init", "-b", "main")
	if err := os.Mkdir(filepath.Join(dir, "agent-config"), 0700); err != nil {
		t.Fatal(err)
	}
	ledger := filepath.Join(dir, "agent-config", "TECH_DEBT.md")
	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(ledger, []byte(s), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("## 条目\n### TD-91\n")
	git("2026-01-01T00:00:00Z", "add", "agent-config/TECH_DEBT.md")
	git("2026-01-01T00:00:00Z", "commit", "-m", "Seed fixture")
	git("2026-01-02T00:00:00Z", "checkout", "-b", "other")
	write("## 条目\n### TD-91\n### TD-9\n")
	git("2026-01-02T00:00:00Z", "add", "agent-config/TECH_DEBT.md")
	git("2026-01-02T00:00:00Z", "commit", "-m", "Add branch-only fixture")
	git("2026-01-03T00:00:00Z", "checkout", "main")
	h, err := newGitHistory(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ id, want string }{{"TD-91", "2026-01-01T00:00:00Z"}, {"TD-9", "2026-01-02T00:00:00Z"}} {
		got, err := h.FirstSeen(tc.id)
		if err != nil {
			t.Fatal(err)
		}
		if got.Format(time.RFC3339) != tc.want {
			t.Fatalf("%s: %s", tc.id, got)
		}
	}
	if _, err := h.FirstSeen("TD-999"); err == nil {
		t.Fatal("invented date")
	}
	if _, err := h.FirstSeen("--all"); err == nil {
		t.Fatal("accepted option as id")
	}
}
