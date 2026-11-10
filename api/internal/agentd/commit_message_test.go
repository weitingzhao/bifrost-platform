package agentd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommitHandInTitle(t *testing.T) {
	env := gitEnv(t.TempDir())
	bare, base := remote(t, env)
	d := fakeDaemon(t, newFake())
	e, a := entryFor(t, d, "a1", "exited")
	e.Title, e.AttemptNo = "Explain the hand-in commit", 2
	e.Effect, e.WorkID = "branch", "W-31"
	ws, err := Prepare(bg, e.Workdir, bare, base, env)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.Workdir, "change.txt"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.Out, "report.md"), []byte("Added meaningful commit messages.\n\nChecks passed."), 0600); err != nil {
		t.Fatal(err)
	}
	if err := d.prepare(bg, a, ws, &e); err != nil {
		t.Fatal(err)
	}
	msg := run(t, e.Workdir, env, "git", "log", "-1", "--format=%B")
	if !strings.HasPrefix(msg, e.Title+"\n\nAttempt 2\n\nAdded meaningful commit messages.") {
		t.Fatalf("message = %q", msg)
	}
	for _, trailer := range []string{"Agentrt-Task: " + e.TaskID, "Change-Id: " + e.ChangeID, "Work: W-31"} {
		if !strings.Contains(msg, trailer) {
			t.Fatalf("missing %q: %s", trailer, msg)
		}
	}
}

func TestCommitTitleLimit(t *testing.T) {
	title := strings.Repeat("界", 73)
	got := handInMessage(&Entry{Title: title, AttemptNo: 1}, nil)
	if subject, _, _ := strings.Cut(got, "\n"); subject != strings.Repeat("界", 72) {
		t.Fatalf("subject = %q", subject)
	}
}
