package landinggit

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// EvidenceReader is one workspace with main fetched, for reading many pairs
// in one pass (R5:5.7-5): STG coverage reads every archived evidence for a
// newly landed member, and opening a workspace for each pair fetched main's
// whole history each time. Every read still fetches its own P and A, exactly
// as readEvidence does; only the workspace and main's fetch are shared.
type EvidenceReader struct {
	g    *Git
	dir  string
	head string // main's head as fetched when the reader opened
}

// OpenEvidence makes the workspace, fetches main and checks that its
// history is complete. On any failure the workspace is removed; on success
// the caller owns it and must Close it.
func (g *Git) OpenEvidence(ctx context.Context) (er *EvidenceReader, err error) {
	dir, err := os.MkdirTemp("", "agentrt-landing-main-")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(dir)
		}
	}()
	if _, err = g.run(ctx, dir, false, nil, nil, "init", "--template=", "."); err != nil {
		return nil, err
	}
	if _, err = g.run(ctx, dir, false, nil, nil, "remote", "add", "origin", g.remote); err != nil {
		return nil, err
	}
	if _, err = g.run(ctx, dir, true, nil, nil, "fetch", "--no-tags", "--no-recurse-submodules", "origin", "+refs/heads/main:refs/remotes/origin/main"); err != nil {
		return nil, err
	}
	shallow, err := g.run(ctx, dir, false, nil, nil, "rev-parse", "--is-shallow-repository")
	if err != nil || strings.TrimSpace(shallow) != "false" {
		return nil, errors.New("main's history is not complete")
	}
	out, err := g.run(ctx, dir, false, nil, nil, "rev-parse", "refs/remotes/origin/main")
	head := strings.TrimSpace(out)
	if err != nil || !fullSHA.MatchString(head) {
		return nil, errors.New("main's head is not readable")
	}
	return &EvidenceReader{g: g, dir: dir, head: head}, nil
}

// Close removes the workspace.
func (er *EvidenceReader) Close() error { return os.RemoveAll(er.dir) }

// ReleaseEvidence is (*Git).ReleaseEvidence read in this workspace.
func (er *EvidenceReader) ReleaseEvidence(ctx context.Context, p, r, task, a string) (string, bool, landing.MainHistory, error) {
	if !fullSHA.MatchString(p) {
		return "", false, landing.MainHistory{}, errors.New("invalid release P")
	}
	hist := landing.MainHistory{State: landing.Unknown}
	if !fullSHA.MatchString(r) || (a != "" && !fullSHA.MatchString(a)) {
		return "", false, hist, errors.New("invalid R or A")
	}
	return er.read(ctx, p, r, task, a)
}

// read is one pair. An empty snapshot reads main's head as fetched when the
// reader opened (MainEvidence); otherwise snapshot is fetched first, on
// every read, and read in place of main.
// ancestor says whether commit is r or below it in r's history.
func (er *EvidenceReader) ancestor(ctx context.Context, commit, r string) bool {
	if !fullSHA.MatchString(commit) || !fullSHA.MatchString(r) {
		return false
	}
	_, err := er.g.run(ctx, er.dir, false, nil, nil, "merge-base", "--is-ancestor", commit, r)
	return err == nil
}

func (er *EvidenceReader) read(ctx context.Context, snapshot, r, task, a string) (head string, hasR bool, hist landing.MainHistory, err error) {
	hist = landing.MainHistory{State: landing.Unknown}
	g, dir := er.g, er.dir
	head = er.head
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
		// The result's commit is R, or, for a member of a landing batch
		// other than its last, its own replayed commit on the stack below
		// R: the batch records every member's R as the stack's top (2026-11-10,
		// M-374 w2 and M-377 v1 read unknown, landed-unverified cards, and
		// B5 waited on them). The provenance History requires is unchanged.
		if h, e := w.History(ctx, task, a, head); e == nil && (h.State == landing.Effective || h.State == landing.Reverted) &&
			(h.Commit == r || er.ancestor(ctx, h.Commit, r)) {
			hist = h
		}
	}
	return head, hasR, hist, nil
}
