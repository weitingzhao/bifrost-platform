package server

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// The check answers what the reconciliation of a red full run would name,
// without a red run. Origin: 2026-11-14. The reconciliation only runs when a
// daily full run is red, and a deliberately broken commit cannot be put on
// main to see it work; this reads the same things on request, for real
// landings. The landings here are the kinds seen that day: one whose own run
// left a module out, one that ran everything, one with no run of its own
// that says, one whose run could not be read.
func TestUnitFullCICheck(t *testing.T) {
	t.Parallel()
	sha := func(c string) string { return strings.Repeat(c, 40) }
	history := []historyCommit{
		{sha: sha("5"), parent: sha("4"), subject: "five"},
		{sha: sha("4"), parent: sha("3"), subject: "four"},
		{sha: sha("3"), parent: sha("2"), subject: "three"},
		{sha: sha("2"), parent: sha("1"), subject: "two"},
		{sha: sha("1"), parent: sha("0"), subject: "one"},
		// A commit merged in beside the first-parent line is not a landing.
		{sha: sha("9"), parent: sha("1"), subject: "side"},
	}
	src := unitCheckSources{
		firstParents: func(_ context.Context, _, green, red string, max int) ([][2]string, error) {
			return firstParentsOf(history, green, red, max)
		},
		runModules: func(_ context.Context, repo, revision string) (string, string, error) {
			switch revision {
			case sha("5"):
				return "ci-5", " svc repo ", nil
			case sha("4"):
				return "ci-4", "*", nil
			case sha("3"):
				return "", "", nil
			default:
				return "", "", errors.New("cluster away")
			}
		},
		failedTasks: func(_ context.Context, run string) ([]string, error) {
			if run == "ci-full-x" {
				return []string{"test-web"}, nil
			}
			return nil, nil
		},
	}
	got, err := unitFullCICheck(context.Background(), src, "r", sha("1"), sha("5"), "ci-full-x")
	if err != nil || len(got.Landings) != 4 || got.Suspects != 1 || got.Unread != 2 || got.Unreconciled != "" {
		t.Fatalf("check: %+v %v", got, err)
	}
	want := []unitCheckLanding{
		{Commit: sha("5"), Subject: "five", Run: "ci-5", Modules: " svc repo ", Read: "ok", Suspect: true},
		{Commit: sha("4"), Subject: "four", Run: "ci-4", Modules: "*", Read: "ok"},
		{Commit: sha("3"), Subject: "three", Read: "no_run"},
		{Commit: sha("2"), Subject: "two", Read: "error"},
	}
	for i, l := range got.Landings {
		if l != want[i] {
			t.Errorf("landing %d: %+v, want %+v", i, l, want[i])
		}
	}
	if len(got.FailedTasks) != 1 || got.FailedTasks[0] != "test-web" {
		t.Errorf("failed tasks: %v", got.FailedTasks)
	}
	unitsContract(t, "full-ci-check.json", got)

	// Without a run nothing is said about failed tasks; a run with none says an empty list, not nothing.
	if got, _ = unitFullCICheck(context.Background(), src, "r", sha("4"), sha("5"), ""); got.FailedTasks != nil || len(got.Landings) != 1 {
		t.Errorf("without a run: %+v", got)
	}
	if got, _ = unitFullCICheck(context.Background(), src, "r", sha("4"), sha("5"), "ci-ok"); got.FailedTasks == nil || len(got.FailedTasks) != 0 {
		t.Errorf("a run with no failed task: %+v", got.FailedTasks)
	}
	// green that is not behind red on the first-parent line is an error, not an empty list.
	if _, err = unitFullCICheck(context.Background(), src, "r", sha("9"), sha("5"), ""); err == nil {
		t.Error("a commit beside the line was taken as the last green")
	}
	// More landings than are looked at: the newest ones, and it says so.
	long := []historyCommit{}
	for i := 0; i < 40; i++ {
		long = append(long, historyCommit{sha: strings.Repeat("a", 38) + string(rune('A'+i/26)) + string(rune('a'+i%26)), parent: strings.Repeat("a", 38) + string(rune('A'+(i+1)/26)) + string(rune('a'+(i+1)%26))})
	}
	src.firstParents = func(_ context.Context, _, green, red string, max int) ([][2]string, error) {
		return firstParentsOf(long, green, red, max)
	}
	got, err = unitFullCICheck(context.Background(), src, "r", long[39].sha, long[0].sha, "")
	if err != nil || len(got.Landings) != unitFullCICheckMax || !strings.Contains(got.Unreconciled, "only the newest 30") {
		t.Errorf("a long stretch: %d landings, %q, %v", len(got.Landings), got.Unreconciled, err)
	}
}
