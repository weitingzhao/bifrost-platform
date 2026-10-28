package landinggit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// MainEvidence reads main for the main reconciler (S3 must 3, CUT §10.6 C-R1):
// main's head after a complete fetch, and whether r is in its history. A
// commit main never had is absent from the fetched objects: that, or an
// ancestry check that answers no, is HasR false. Every other failure (the
// fetch, a malformed head, a command that did not run) is an error, never a
// false: an unreadable main is not evidence that a push did not land.
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
	out, err := g.run(ctx, dir, false, nil, nil, "rev-parse", "refs/remotes/origin/main")
	head = strings.TrimSpace(out)
	if err != nil || !fullSHA.MatchString(head) {
		return "", false, errors.New("main's head is not readable")
	}
	// The fetch is the whole of main's history: r not among its objects
	// means main never had it.
	code, err := g.exit(ctx, dir, "cat-file", "-e", r+"^{commit}")
	if err != nil {
		return "", false, err
	}
	if code != 0 {
		return head, false, nil
	}
	switch code, err = g.exit(ctx, dir, "merge-base", "--is-ancestor", r, head); {
	case err != nil:
		return "", false, err
	case code == 0:
		return head, true, nil
	case code == 1:
		return head, false, nil
	}
	return "", false, fmt.Errorf("landing git merge-base answered %d", code)
}

// exit runs a local Git command that answers with its exit status: the
// status, or an error when it did not run to an answer (a timeout, a signal,
// no git).
func (g *Git) exit(ctx context.Context, dir string, args ...string) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append(append([]string{}, isolation...), args...)...)
	cmd.Dir = dir
	cmd.Env = isolatedEnv()
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0, nil
	case errors.As(err, &exit) && ctx.Err() == nil && exit.ExitCode() > 0:
		return exit.ExitCode(), nil
	}
	return 0, fmt.Errorf("landing git %s failed", args[0])
}
