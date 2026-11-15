package delivery

import (
	"context"
	"reflect"
	"testing"
)

// Owner 2026-11-14: approval cards for changes that have a way back; design
// ci-speed/owner-off-the-loop-rules.md §3 class 3. The release policy names
// who wrote a trust-anchor change from each commit's message and file list:
// the walk gives both, commit by commit, and fails where the file union fails.
func TestCommitsBetweenCarriesEachCommitsMessageAndFiles(t *testing.T) {
	s, _ := diffService(t, commitRanges{
		factSHA1 + "|" + factSHA2: {
			{sha: shaN(2), files: []string{"a.go", "b.go"}, message: "feat: two\n\nClaude-Session: local_x\n"},
			{sha: shaN(1), files: []string{"a.go"}}, // a mirror that sends no message: read as none
		},
		factSHA2 + "|" + factSHA1: nil,
	}, nil)
	got, err := s.commitsBetween(context.Background(), "repo-app", factSHA2, factSHA1, "", "")
	want := []RangeCommit{
		{SHA: shaN(2), Message: "feat: two\n\nClaude-Session: local_x\n", Files: []string{"a.go", "b.go"}},
		{SHA: shaN(1), Files: []string{"a.go"}},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("commits = %+v err %v", got, err)
	}
	// Positive control of the refusal: the old side not an ancestor is an
	// error here as it is for the file union.
	s, _ = diffService(t, commitRanges{
		factSHA1 + "|" + factSHA2: {{sha: shaN(2), files: []string{"a.go"}}},
		factSHA2 + "|" + factSHA1: {{sha: shaN(9), files: []string{"z.go"}}},
	}, nil)
	if got, err := s.commitsBetween(context.Background(), "repo-app", factSHA2, factSHA1, "", ""); err == nil {
		t.Fatalf("a diverged history gave commits: %+v", got)
	}
}
