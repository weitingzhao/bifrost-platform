package landinggit

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestWaveAppend(t *testing.T) {
	for _, rebase := range []bool{false, true} {
		name := "fast-forward"
		if rebase {
			name = "rebase"
		}
		t.Run(name, func(t *testing.T) {
			g, in, src := fixture(t)
			ctx := context.Background()
			head := in.Base
			if rebase {
				head = in.M0
			}
			ref := "refs/heads/wave/test"
			if err := g.CutWave(ctx, ref, head); err != nil {
				t.Fatal(err)
			}
			if err := g.CutWave(ctx, ref, in.A); err == nil {
				t.Fatal("overwrote existing branch")
			}
			commit, rebased, err := g.AppendWave(ctx, ref, head, in.A, in.Base)
			if err != nil {
				t.Fatal(err)
			}
			if rebased != rebase || (!rebase && commit != in.A) {
				t.Fatalf("commit=%s rebased=%v", commit, rebased)
			}
			gitTest(t, src, "fetch", "--no-tags", g.remote, ref)
			if tip := gitTest(t, src, "rev-parse", "FETCH_HEAD"); tip != commit {
				t.Fatalf("tip=%s want %s", tip, commit)
			}
			if parent := gitTest(t, src, "rev-parse", commit+"^"); parent != head {
				t.Fatalf("parent=%s", parent)
			}
			for _, format := range []string{"%B", "%an <%ae> %aI"} {
				original := gitTest(t, src, "show", "-s", "--format="+format, in.A)
				actual := gitTest(t, src, "show", "-s", "--format="+format, commit)
				if actual != original {
					t.Fatalf("metadata changed: %q != %q", actual, original)
				}
			}
			if _, _, err := g.AppendWave(ctx, ref, head, in.A, in.Base); err == nil {
				t.Fatal("lost lease succeeded")
			}
		})
	}
}

func TestWaveConflict(t *testing.T) {
	g, in, _ := sameFileFixture(t, "second", "second")
	ctx := context.Background()
	ref := "refs/heads/wave/conflict"
	if err := g.CutWave(ctx, ref, in.M0); err != nil {
		t.Fatal(err)
	}
	_, _, err := g.AppendWave(ctx, ref, in.M0, in.A, in.Base)
	var conflict *ConflictError
	if !errors.As(err, &conflict) || conflict.M0 != in.M0 || strings.Join(conflict.Paths, ",") != "code.go" {
		t.Fatalf("conflict: %#v, %v", conflict, err)
	}
}
