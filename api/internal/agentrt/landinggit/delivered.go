package landinggit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
)

// ChangeIdentity is what one accepted result changes, for the delivered-by
// backfill (DESIGN-step-landed-superseded-2026-11-01 §6 item 2): a result A
// is one commit on its task's base (hand-ins are squashed), so A^..A is the
// task's change. PatchID is `git patch-id --verbatim` of that diff: --stable
// strips whitespace, so it would equate a YAML indentation change with none.
// DiffSHA256 is the sha256 of the diff's own bytes (--binary, so full blob
// ids and binary content are in it).
type ChangeIdentity struct {
	PatchID, DiffSHA256 string
}

// SameChange: a and b carry the same change only when both the verbatim
// patch-id and the raw diff's digest agree (§6 item 2). A result that
// changes nothing has no patch-id and is never the same as anything.
func SameChange(a, b ChangeIdentity) bool {
	return a.PatchID != "" && a.PatchID == b.PatchID && a.DiffSHA256 == b.DiffSHA256
}

// ErrNotComparable: the two results cannot be compared, and reading them
// again gives the same answer: a SHA that is not one, a commit that is not
// one commit on one parent, a diff over the output bound. ErrNotServed is
// one too, but only inferred. Anything else (the network, a timeout) is a
// plain error worth trying again.
var ErrNotComparable = errors.New("the two results cannot be compared")

// ErrNotServed: the fetch of the two commits failed while the remote still
// answered. Most likely the remote no longer serves one of them (gc took an
// unreferenced commit), but a full temporary directory, an upload-pack
// error under load or a transfer cut short look the same from here, so it
// is worth reading again after a long wait, not never.
var ErrNotServed = fmt.Errorf("%w: the fetch failed while the remote answered", ErrNotComparable)

func notComparable(why string) error { return fmt.Errorf("%w: %s", ErrNotComparable, why) }

// Changes reads the change of each of two accepted results a and b. It
// fetches only those two commits, each with its parent (depth 2), into a
// temporary repository that is removed after, never main (Open fetches all
// of main). It pushes and writes nothing; every failure is an error, one
// that will not pass by itself is ErrNotComparable.
func (g *Git) Changes(ctx context.Context, a, b string) (ChangeIdentity, ChangeIdentity, error) {
	if !fullSHA.MatchString(a) || !fullSHA.MatchString(b) {
		return ChangeIdentity{}, ChangeIdentity{}, notComparable("a result is not a full SHA")
	}
	dir, err := os.MkdirTemp("", "agentrt-landing-change-")
	if err != nil {
		return ChangeIdentity{}, ChangeIdentity{}, err
	}
	defer os.RemoveAll(dir)
	if _, err = g.run(ctx, dir, false, nil, nil, "init", "--template=", "."); err != nil {
		return ChangeIdentity{}, ChangeIdentity{}, err
	}
	if _, err = g.run(ctx, dir, false, nil, nil, "remote", "add", "origin", g.remote); err != nil {
		return ChangeIdentity{}, ChangeIdentity{}, err
	}
	if _, err = g.run(ctx, dir, true, nil, nil, "fetch", "--depth=2", "--no-tags", "--no-recurse-submodules", "origin", a, b); err != nil {
		// Git's own words are not kept (run), so ask the remote something
		// it always answers: when it does, the fetch failed on the commits
		// or on this side, most likely the former (ErrNotServed).
		if _, probe := g.run(ctx, dir, true, nil, nil, "ls-remote", "--refs", "origin", "refs/heads/main"); probe == nil {
			return ChangeIdentity{}, ChangeIdentity{}, ErrNotServed
		}
		return ChangeIdentity{}, ChangeIdentity{}, err
	}
	x, err := g.change(ctx, dir, a)
	if err != nil {
		return ChangeIdentity{}, ChangeIdentity{}, err
	}
	y, err := g.change(ctx, dir, b)
	return x, y, err
}

// change is the identity of commit a's own change, a^..a.
func (g *Git) change(ctx context.Context, dir, a string) (ChangeIdentity, error) {
	parents, err := g.run(ctx, dir, false, nil, nil, "rev-list", "--parents", "-n", "1", a)
	if err != nil {
		return ChangeIdentity{}, err
	}
	if p := strings.Fields(parents); len(p) != 2 || p[0] != a {
		return ChangeIdentity{}, notComparable("a result is not one commit on one parent")
	}
	diff, err := g.run(ctx, dir, false, nil, nil, "diff", "--no-ext-diff", "--no-textconv", "--binary", a+"^", a, "--")
	if errors.Is(err, ErrOutputLimit) {
		return ChangeIdentity{}, notComparable("a result's diff is over the output bound")
	}
	if err != nil {
		return ChangeIdentity{}, err
	}
	id, err := g.run(ctx, dir, false, []byte(diff), nil, "patch-id", "--verbatim")
	if err != nil {
		return ChangeIdentity{}, err
	}
	sum := sha256.Sum256([]byte(diff))
	out := ChangeIdentity{DiffSHA256: hex.EncodeToString(sum[:])}
	if f := strings.Fields(id); len(f) > 0 {
		out.PatchID = f[0]
	}
	return out, nil
}
