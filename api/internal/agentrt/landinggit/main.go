package landinggit

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// MainEvidence reads main for the main reconciler (S3 must 3, CUT §10.6 C-R1,
// review 658 B1): main's head after a complete fetch, and whether r is in
// the history reachable from that head now. hasR comes from walking that
// history in full (rev-list) and finding r in it or not; it is false only
// when the walk completed without r. Every failure (the fetch, a shallow
// clone, a malformed head, a walk that did not finish) is an error, never a
// false: an unreadable main is not evidence that a push did not land. A
// commit main once had and lost (a rewrite) reads as absent: absent from the
// history main has now.
//
// With the root's task and accepted commit A, it also says whether R is in
// effect now (S3 B-02): the main history of A's result evaluated at the same
// head, Effective or Reverted (with the reversal in effect) when that names
// R, and Unknown otherwise or when it does not read. Unknown is never taken
// as in effect. A is fetched apart from main and only when R is in it: A no
// longer fetchable (its agent branch pruned) leaves head and hasR standing
// with validity unknown, never an error (review 662 R-03).
func (g *Git) MainEvidence(ctx context.Context, r, task, a string) (head string, hasR bool, hist landing.MainHistory, err error) {
	return g.readEvidence(ctx, "", r, task, a)
}

// ReleaseEvidence reads the complete history at a release's immutable P.
// It never substitutes today's main head for that historical snapshot.
func (g *Git) ReleaseEvidence(ctx context.Context, p, r, task, a string) (string, bool, landing.MainHistory, error) {
	if !fullSHA.MatchString(p) {
		return "", false, landing.MainHistory{}, errors.New("invalid release P")
	}
	return g.readEvidence(ctx, p, r, task, a)
}

func (g *Git) readEvidence(ctx context.Context, snapshot, r, task, a string) (head string, hasR bool, hist landing.MainHistory, err error) {
	hist = landing.MainHistory{State: landing.Unknown}
	if !fullSHA.MatchString(r) || (a != "" && !fullSHA.MatchString(a)) {
		return "", false, hist, errors.New("invalid R or A")
	}
	dir, err := os.MkdirTemp("", "agentrt-landing-main-")
	if err != nil {
		return "", false, hist, err
	}
	defer os.RemoveAll(dir)
	if _, err = g.run(ctx, dir, false, nil, nil, "init", "--template=", "."); err != nil {
		return "", false, hist, err
	}
	if _, err = g.run(ctx, dir, false, nil, nil, "remote", "add", "origin", g.remote); err != nil {
		return "", false, hist, err
	}
	if _, err = g.run(ctx, dir, true, nil, nil, "fetch", "--no-tags", "--no-recurse-submodules", "origin", "+refs/heads/main:refs/remotes/origin/main"); err != nil {
		return "", false, hist, err
	}
	shallow, err := g.run(ctx, dir, false, nil, nil, "rev-parse", "--is-shallow-repository")
	if err != nil || strings.TrimSpace(shallow) != "false" {
		return "", false, hist, errors.New("main's history is not complete")
	}
	out, err := g.run(ctx, dir, false, nil, nil, "rev-parse", "refs/remotes/origin/main")
	head = strings.TrimSpace(out)
	if err != nil || !fullSHA.MatchString(head) {
		return "", false, hist, errors.New("main's head is not readable")
	}
	if snapshot != "" {
		if _, err := g.run(ctx, dir, true, nil, nil, "fetch", "--no-tags", "--no-recurse-submodules", "origin", snapshot); err != nil {
			return "", false, hist, err
		}
		head = snapshot
	}
	// The whole walk or an error: rev-list fails on a missing or corrupt
	// object in the history it walks, so a finished walk is all of it.
	all, err := g.run(ctx, dir, false, nil, nil, "rev-list", head)
	if err != nil {
		return "", false, hist, err
	}
	for _, line := range strings.Split(all, "\n") {
		if line == r {
			hasR = true
			continue
		}
		if line != "" && !fullSHA.MatchString(line) {
			return "", false, hist, errors.New("main's history did not read")
		}
	}
	if hasR && task != "" && a != "" {
		// A's message names its lineage (Change-Id, task). Fetched by id,
		// it moves no ref: the head read above stands.
		if _, e := g.run(ctx, dir, true, nil, nil, "fetch", "--no-tags", "--no-recurse-submodules", "origin", a); e != nil {
			return head, hasR, hist, nil
		}
		w := &Workspace{git: g, dir: dir}
		if h, e := w.History(ctx, task, a, head); e == nil && h.Commit == r && (h.State == landing.Effective || h.State == landing.Reverted) {
			hist = h
		}
	}
	return head, hasR, hist, nil
}
