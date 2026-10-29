package landinggit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strings"
)

// CandidatePatch is the Git half of a landing grant's material (coordinator
// v3 §5.6 item 3): the full patch M0..R and its sha256, the patch shown to
// the Owner (cut at ShownLimit bytes), and whether R's patch is A's (stable
// patch-id of M0..R against A's own parent..A). It is read with the
// environment's Git identity in a temporary clone that is removed after.
type CandidatePatch struct {
	SHA256    string
	Shown     string
	Truncated bool
	Size      int
	Match     bool
}

// ShownLimit is the most of a patch the material page shows (§5.6 item 3).
const ShownLimit = 512 << 10

// CandidatePatch reads main, A and R from the remote and diffs them. It
// pushes and writes nothing; every failure is an error, never an empty patch.
func (g *Git) CandidatePatch(ctx context.Context, a, m0, r string) (CandidatePatch, error) {
	var out CandidatePatch
	if !fullSHA.MatchString(a) || !fullSHA.MatchString(m0) || !fullSHA.MatchString(r) {
		return out, errors.New("invalid A, M0 or R")
	}
	dir, err := os.MkdirTemp("", "agentrt-landing-patch-")
	if err != nil {
		return out, err
	}
	defer os.RemoveAll(dir)
	if _, err = g.run(ctx, dir, false, nil, nil, "init", "--template=", "."); err != nil {
		return out, err
	}
	if _, err = g.run(ctx, dir, false, nil, nil, "remote", "add", "origin", g.remote); err != nil {
		return out, err
	}
	if _, err = g.run(ctx, dir, true, nil, nil, "fetch", "--no-tags", "--no-recurse-submodules", "origin",
		"+refs/heads/main:refs/remotes/origin/main", a, r); err != nil {
		return out, err
	}
	// R's parent must be M0: the patch shown is exactly what main receives.
	parents, err := g.run(ctx, dir, false, nil, nil, "rev-list", "--parents", "-n", "1", r)
	if err != nil {
		return out, err
	}
	if p := strings.Fields(parents); len(p) != 2 || p[1] != m0 {
		return out, errors.New("R is not one commit on M0")
	}
	patch, err := g.run(ctx, dir, false, nil, nil, "diff", "--no-ext-diff", "--no-textconv", "--binary", m0, r, "--")
	if err != nil {
		return out, err
	}
	original, err := g.run(ctx, dir, false, nil, nil, "diff", "--no-ext-diff", "--no-textconv", "--binary", a+"^", a, "--")
	if err != nil {
		return out, err
	}
	out = patchEvidence(patch)
	x, err := g.run(ctx, dir, false, []byte(original), nil, "patch-id", "--stable")
	if err != nil {
		return out, err
	}
	y, err := g.run(ctx, dir, false, []byte(patch), nil, "patch-id", "--stable")
	if err != nil {
		return out, err
	}
	out.Match = len(strings.Fields(x)) > 0 && len(strings.Fields(y)) > 0 && strings.Fields(x)[0] == strings.Fields(y)[0]
	return out, nil
}

// patchEvidence is the digest of the whole patch and what of it is shown:
// all of it up to ShownLimit bytes, none past it (the digest still covers
// every byte; the Owner then reads the file table and Gitea's compare).
func patchEvidence(patch string) CandidatePatch {
	sum := sha256.Sum256([]byte(patch))
	out := CandidatePatch{SHA256: hex.EncodeToString(sum[:]), Size: len(patch), Shown: patch, Truncated: len(patch) > ShownLimit}
	if out.Truncated {
		out.Shown = ""
	}
	return out
}
