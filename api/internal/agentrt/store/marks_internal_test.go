package store

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The reasons a withdrawn block and a round past the withdrawals are told by
// (TD-395) are written in one place each: withdrawnBlockMark by BlockWithdrawn,
// liftMark by decideBlocked for a withdrawn block. Any other code that wrote
// them, or their text, would make a block a withdrawn one or lift the
// withdrawals (review td395-1 C2). Each is read where this lists it.
func TestMarksHaveTheirWriters(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []struct {
		name, text string
		in         []string // the files that may name it
	}{
		{"withdrawnBlockMark", withdrawnBlockMark, []string{"withdrawn.go", "blockedrecommend.go"}},
		{"liftMark", liftMark, []string{"retry.go", "blockedcard.go"}},
	} {
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			named := strings.Count(string(src), m.name)
			if named > 0 && !slices.Contains(m.in, f) {
				t.Errorf("%s names %s; only %v may", f, m.name, m.in)
			}
			if texts := strings.Count(string(src), strings.TrimSpace(strings.TrimSuffix(m.text, ": "))); texts > 0 && f != m.in[0] {
				t.Errorf("%s writes the text of %s %d times; only %s may", f, m.name, texts, m.in[0])
			}
		}
	}
	// Who writes them: one block, one move back to ready.
	for f, want := range map[string]string{"withdrawn.go": `moveTask(ctx, tx, "controller", t, "blocked", why)`,
		"blockedcard.go": "lift = liftMark"} {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(src), want) != 1 {
			t.Errorf("%s does not write its mark once (%q)", f, want)
		}
	}
}
