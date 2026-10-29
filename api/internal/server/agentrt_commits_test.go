package server

import (
	"slices"
	"testing"
)

// The paths and lines of a commit's diff, as the pinned result check reads
// them (agentrt_commits.go): both sides of a rename, a new and a deleted
// file, a binary one; a path it cannot read for sure is an error.
func TestDiffFacts(t *testing.T) {
	t.Parallel()
	diff := "diff --git a/api/x.go b/api/x.go\nindex 1..2 100644\n--- a/api/x.go\n+++ b/api/x.go\n@@ -1,2 +1,2 @@\n a\n-b\n+c\n" +
		"diff --git a/new.txt b/new.txt\nnew file mode 100644\n--- /dev/null\n+++ b/new.txt\n@@ -0,0 +1 @@\n+++plus\n" +
		"diff --git a/gone.txt b/gone.txt\ndeleted file mode 100644\n--- a/gone.txt\n+++ /dev/null\n@@ -1 +0,0 @@\n---minus\n" +
		"diff --git a/api/internal/agentrt/old.go b/docs/new.go\nsimilarity index 90%\nrename from api/internal/agentrt/old.go\nrename to docs/new.go\n" +
		"diff --git a/bin.png b/bin.png\nBinary files a/bin.png and b/bin.png differ\n"
	paths, lines, err := diffFacts([]byte(diff))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"api/x.go", "new.txt", "gone.txt", "api/internal/agentrt/old.go", "docs/new.go", "bin.png"}
	if !slices.Equal(paths, want) || lines != 1<<30 {
		t.Fatalf("paths %v lines %d", paths, lines)
	}
	for name, d := range map[string]string{
		"a quoted path":        "diff --git \"a/x y\" \"b/x y\"\n",
		"two names, no rename": "diff --git a/x.go b/y.go\n--- a/x.go\n+++ b/y.go\n",
		"a quoted rename":      "diff --git a/x b/y\nrename from \"x\"\nrename to y\n",
	} {
		if _, _, err := diffFacts([]byte(d)); err == nil {
			t.Errorf("%s: read", name)
		}
	}
}
