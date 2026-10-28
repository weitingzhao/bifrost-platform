package landinggit

import (
	"context"
	"errors"
	"os"
	"strings"
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
func (g *Git) MainEvidence(ctx context.Context, r string) (head string, hasR bool, err error) {
	if !fullSHA.MatchString(r) {
		return "", false, errors.New("invalid R")
	}
	dir, err := os.MkdirTemp("", "agentrt-landing-main-")
	if err != nil {
		return "", false, err
	}
	defer os.RemoveAll(dir)
	if _, err = g.run(ctx, dir, false, nil, nil, "init", "--template=", "."); err != nil {
		return "", false, err
	}
	if _, err = g.run(ctx, dir, false, nil, nil, "remote", "add", "origin", g.remote); err != nil {
		return "", false, err
	}
	if _, err = g.run(ctx, dir, true, nil, nil, "fetch", "--no-tags", "--no-recurse-submodules", "origin", "+refs/heads/main:refs/remotes/origin/main"); err != nil {
		return "", false, err
	}
	shallow, err := g.run(ctx, dir, false, nil, nil, "rev-parse", "--is-shallow-repository")
	if err != nil || strings.TrimSpace(shallow) != "false" {
		return "", false, errors.New("main's history is not complete")
	}
	out, err := g.run(ctx, dir, false, nil, nil, "rev-parse", "refs/remotes/origin/main")
	head = strings.TrimSpace(out)
	if err != nil || !fullSHA.MatchString(head) {
		return "", false, errors.New("main's head is not readable")
	}
	// The whole walk or an error: rev-list fails on a missing or corrupt
	// object in the history it walks, so a finished walk is all of it.
	all, err := g.run(ctx, dir, false, nil, nil, "rev-list", head)
	if err != nil {
		return "", false, err
	}
	for _, line := range strings.Split(all, "\n") {
		if line == r {
			return head, true, nil
		}
		if line != "" && !fullSHA.MatchString(line) {
			return "", false, errors.New("main's history did not read")
		}
	}
	return head, false, nil
}
