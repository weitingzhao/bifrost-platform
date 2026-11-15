package evidence

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Owner 2026-11-14: approval cards for changes that have a way back; design
// ci-speed/owner-off-the-loop-rules.md §3 class 3. A review record is a row
// of the evidence ledger that no reading of a commit's CI can take for a
// test verdict: it is not the unit "repo", its key document has a version,
// and its scope is not full, so a passed review is never a green key.
func TestReviewRowIsNotATestVerdict(t *testing.T) {
	at := time.Date(2026, 11, 15, 6, 0, 0, 0, time.UTC)
	rv := Review{Repo: "bifrost-platform", Commit: strings.Repeat("a", 40), Reviewer: "local_r", Files: []string{"b.go", "a.go", "b.go"}, Passed: true, NotRead: " the tests "}
	row, err := ReviewRow(rv, at)
	if err != nil {
		t.Fatal(err)
	}
	if row.Unit != ReviewUnit || row.Unit == EvidenceUnitRepo || row.Task != ReviewTask || row.Result != EvidencePassed || row.Scope == EvidenceScopeFull || row.CommitSHA != rv.Commit {
		t.Fatalf("row = %+v", row)
	}
	if EvidenceVerdict([]EvidenceRow{row}) != EvidenceNone {
		t.Fatal("a passed review reads as a green key")
	}
	var inputs map[string]any
	var st struct {
		Reviewer string   `json:"reviewer"`
		Files    []string `json:"files"`
		Verdict  string   `json:"verdict"`
		NotRead  string   `json:"not_read"`
	}
	if err := json.Unmarshal(row.Inputs, &inputs); err != nil || inputs["v"] == nil || inputs["reviewer"] != "local_r" {
		t.Fatalf("inputs = %s err %v", row.Inputs, err)
	}
	if err := json.Unmarshal(row.Statement, &st); err != nil || st.Reviewer != "local_r" || strings.Join(st.Files, ",") != "a.go,b.go" || st.Verdict != EvidencePassed || st.NotRead != "the tests" {
		t.Fatalf("statement = %s err %v", row.Statement, err)
	}
	// The same review a moment later is another row; a failed one says failed.
	later, _ := ReviewRow(rv, at.Add(time.Second))
	rv.Passed = false
	failed, _ := ReviewRow(rv, at)
	if later.RunUID == row.RunUID || later.InputKey == row.InputKey || failed.Result != EvidenceFailed {
		t.Fatalf("later = %+v failed = %+v", later, failed)
	}
	// One thing wrong each: none of these is a record.
	for name, bad := range map[string]Review{
		"no reviewer":     {Repo: "r", Commit: rv.Commit, Files: []string{"a"}},
		"a short commit":  {Repo: "r", Commit: "abc", Reviewer: "x", Files: []string{"a"}},
		"no files":        {Repo: "r", Commit: rv.Commit, Reviewer: "x"},
		"an empty path":   {Repo: "r", Commit: rv.Commit, Reviewer: "x", Files: []string{" "}},
		"an absolute one": {Repo: "r", Commit: rv.Commit, Reviewer: "x", Files: []string{"/a"}},
		"a long note":     {Repo: "r", Commit: rv.Commit, Reviewer: "x", Files: []string{"a"}, NotRead: strings.Repeat("n", maxReviewNote+1)},
	} {
		if _, err := ReviewRow(bad, at); err == nil {
			t.Errorf("%s: recorded", name)
		}
	}
}
