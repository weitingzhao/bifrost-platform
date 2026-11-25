package planstore_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Origin: 2026-11-25, ISSUE-17: the code keeps complaints and laws apart.
// The register of laws holds laws only; the Owner's words (owner_said) and the
// issues drawn from them (owner_issue) are their own records. An issue names
// the laws that answer it; no law code names an issue or reads what he said.
// This test keeps the law files and the complaint files from reaching into
// each other; the positive control below shows it would catch a crossing.
func TestLawsAndComplaintsStayApart(t *testing.T) {
	lawFiles, _ := filepath.Glob("law*.go")
	complaintFiles := []string{"ownersaid.go", "ownerissue.go"}
	if len(lawFiles) == 0 {
		t.Fatal("no law files found: the test reads nothing")
	}
	complaintWords := []string{"owner_said", "owner_issue", "OwnerSaid", "OwnerIssue", "owner-said:", "owner-issue:"}
	lawWords := []string{"law_recorded", "law_amend", "lawRow", "LawCard", "law_purged", "purgeLaw"}
	check := func(file string, words []string) []string {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var hit []string
		for _, w := range words {
			if strings.Contains(string(raw), w) {
				hit = append(hit, w)
			}
		}
		return hit
	}
	for _, f := range lawFiles {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		if hit := check(f, complaintWords); len(hit) > 0 {
			t.Errorf("%s (law code) names complaint records %v: the register of laws holds laws only", f, hit)
		}
	}
	for _, f := range complaintFiles {
		if hit := check(f, lawWords); len(hit) > 0 {
			t.Errorf("%s (complaint code) reaches into the law register %v: an issue names laws by code only", f, hit)
		}
	}
	// Positive control: the check finds a crossing where there is one.
	tmp := filepath.Join(t.TempDir(), "lawcrossing.go")
	if err := os.WriteFile(tmp, []byte("package x // reads owner_issue"), 0o600); err != nil {
		t.Fatal(err)
	}
	if len(check(tmp, complaintWords)) == 0 {
		t.Fatal("positive control: a law file naming owner_issue was not caught")
	}
}
