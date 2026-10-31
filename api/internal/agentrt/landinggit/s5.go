package landinggit

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// S5Patch reads immutable Git objects. Before replay only A's evidence is
// returned; after replay both parents, complete patches and endpoint sets
// are checked. It executes no code from either tree.
func (w *Workspace) S5Patch(ctx context.Context, task, base, a, m0, r string) (landing.PatchEvidence, error) {
	p := landing.PatchEvidence{A: a, Base: base, M0: m0, R: r, DiffAlgorithmVersion: "git-stable-v1"}
	single, changes, e := w.Result(ctx, task, base, a)
	if e != nil {
		return p, e
	}
	if !single {
		return p, fmt.Errorf("S5_PATCH_MISMATCH: A parent")
	}
	p.ParentA = base
	for _, c := range changes {
		p.OldNewPathsAndModes = append(p.OldNewPathsAndModes, landing.S5PathMode{OldPath: c.OldPath, NewPath: c.NewPath, OldMode: c.OldMode, NewMode: c.NewMode, Kind: c.Kind, Binary: c.Binary})
	}
	patch, e := w.local(ctx, "diff", "--no-ext-diff", "--no-textconv", "--binary", base, a, "--")
	if e != nil {
		return p, e
	}
	p.SourcePatchSHA256 = landing.S5Hash([]byte(patch))
	id, e := w.git.run(ctx, w.dir, false, []byte(patch), nil, "patch-id", "--stable")
	if e != nil {
		return p, e
	}
	ids := strings.Fields(id)
	if len(ids) == 0 {
		return p, fmt.Errorf("S5_PATCH_MISMATCH: empty patch")
	}
	p.SourceStablePatchID = ids[0]
	if r == "" && m0 == "" {
		return p, nil
	}
	if !fullSHA.MatchString(m0) || !fullSHA.MatchString(r) {
		return p, fmt.Errorf("S5_PATCH_MISMATCH: R/M0")
	}
	single, replayed, e := w.Result(ctx, task, m0, r)
	if e != nil {
		return p, e
	}
	if !single {
		return p, fmt.Errorf("S5_PATCH_MISMATCH: R parent")
	}
	p.ParentR = m0
	modes := []landing.S5PathMode{}
	for _, c := range replayed {
		modes = append(modes, landing.S5PathMode{OldPath: c.OldPath, NewPath: c.NewPath, OldMode: c.OldMode, NewMode: c.NewMode, Kind: c.Kind, Binary: c.Binary})
	}
	if !reflect.DeepEqual(modes, p.OldNewPathsAndModes) {
		return p, fmt.Errorf("S5_PATCH_MISMATCH: paths/modes")
	}
	drift, e := w.local(ctx, "diff", "--name-only", "-z", "--no-renames", base, m0, "--")
	if e != nil {
		return p, e
	}
	for _, d := range strings.Split(drift, "\x00") {
		for _, c := range changes {
			for _, path := range c.Paths() {
				if d == path {
					return p, fmt.Errorf("S5_PATCH_MISMATCH: overlap")
				}
			}
		}
	}
	replay, e := w.local(ctx, "diff", "--no-ext-diff", "--no-textconv", "--binary", m0, r, "--")
	if e != nil {
		return p, e
	}
	p.ReplayPatchSHA256 = landing.S5Hash([]byte(replay))
	id, e = w.git.run(ctx, w.dir, false, []byte(replay), nil, "patch-id", "--stable")
	if e != nil {
		return p, e
	}
	ids = strings.Fields(id)
	if len(ids) == 0 {
		return p, fmt.Errorf("S5_PATCH_MISMATCH: empty replay")
	}
	p.ReplayStablePatchID = ids[0]
	p.ReplayMatch = p.SourceStablePatchID == p.ReplayStablePatchID
	if !p.ReplayMatch {
		return p, fmt.Errorf("S5_PATCH_MISMATCH: patch-id")
	}
	tree, e := w.local(ctx, "rev-parse", m0+"^{tree}")
	if e != nil {
		return p, e
	}
	p.BaselineTree = strings.TrimSpace(tree)
	tree, e = w.local(ctx, "rev-parse", r+"^{tree}")
	if e != nil {
		return p, e
	}
	p.ResultTree = strings.TrimSpace(tree)
	return p, nil
}

// FetchS5Patch is the remote reader after the candidate branch is pushed.
// The workspace was opened at A; a cherry-picked R does not contain A in its
// ancestry, so fetching just R would not supply the reviewed source object.
func (w *Workspace) FetchS5Patch(ctx context.Context, task, base, a, m0, r string) (landing.PatchEvidence, error) {
	if r != "" {
		if !fullSHA.MatchString(r) {
			return landing.PatchEvidence{}, fmt.Errorf("S5_BINDING: result SHA")
		}
		if _, err := w.git.run(ctx, w.dir, true, nil, nil, "fetch", "--no-tags", "--no-recurse-submodules", "origin", r); err != nil {
			return landing.PatchEvidence{}, err
		}
	}
	return w.S5Patch(ctx, task, base, a, m0, r)
}
