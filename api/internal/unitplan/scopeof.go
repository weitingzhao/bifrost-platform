package unitplan

import (
	"context"
	"strings"
)

// GET /api/v1/units/scope?repo=…&commit=…&base=… is which modules of a commit
// CI has to verify again against a base commit (declaration standard §2): a
// module whose tasks all keep the keys they had at the base does not run.
// The answer is computed from the two commits' declarations and trees; the
// platform holds no list of any project's modules. A commit with no
// declaration, a declaration that does not read, or a base that cannot be
// compared with answers "everything runs" and says why. So does a base the
// evidence ledger has no passing full CI for: a module that does not run is
// vouched for by the base's run (base_run), and a base that was red or never
// verified vouches for nothing. Read-only. format=text answers one line a
// module, "run <module>" or "same <module>", for a shell caller.
//
// No token, as /evidence/ci beside it: the in-cluster task that decides what
// a CI run runs holds no platform credential, and the answer names only what
// the repository's own declaration names.

// BaseCIFunc is the ledger's word on a commit's full CI: Succeeded, Failed or
// "", and the run that says so.
type BaseCIFunc func(ctx context.Context, repo, commit string) (status, run string, err error)

func ScopeOf(ctx context.Context, trees func(ctx context.Context, repo, commit string) (*Tree, error), baseCI BaseCIFunc, repo, commit, base string) (Scope, error) {
	pinned := Source{Pinned: func(p, at string) (*Tree, error) { return trees(ctx, p, at) }}
	t, err := trees(ctx, repo, commit)
	if err != nil {
		return Scope{}, err
	}
	if _, err = t.ReadFile(ManifestPath); err != nil {
		return WholeProject(repo, commit, base, "the commit has no "+ManifestPath+": the project is one block", false, nil), nil
	}
	head, err := Build(repo, commit, t, pinned)
	if err != nil {
		return WholeProject(repo, commit, base, "the declaration does not read: "+err.Error(), true, nil), nil
	}
	if base == "" {
		return WholeProject(repo, commit, base, "no base to compare with", true, head), nil
	}
	if base == commit {
		return WholeProject(repo, commit, base, "the base is the commit itself", true, head), nil
	}
	bt, err := trees(ctx, repo, base)
	if err != nil {
		return Scope{}, err
	}
	was, err := Build(repo, base, bt, pinned)
	if err != nil {
		return WholeProject(repo, commit, base, "the base has no declaration that reads: "+err.Error(), true, head), nil
	}
	status, run, err := baseCI(ctx, repo, base)
	if err != nil {
		return Scope{}, err
	}
	if status != "Succeeded" {
		if status == "" {
			status = "none"
		}
		return WholeProject(repo, commit, base, "the evidence ledger has no passing full CI of the base (it says "+status+"): nothing vouches for a module that would not run", true, head), nil
	}
	out := Compare(was, head)
	out.BaseRun = run
	return out, nil
}

// ScopeText is the answer for a shell: "declared yes|no", then one line a
// module, "run <module>" or "same <module>", and a last line "end" so that a
// cut-off answer is not taken for a whole one. A commit that runs whole and
// names no module (no declaration) has no module lines.
func ScopeText(s Scope) string {
	var b strings.Builder
	if s.Declared {
		b.WriteString("declared yes\n")
	} else {
		b.WriteString("declared no\n")
	}
	for _, m := range s.Modules {
		if m.Run {
			b.WriteString("run " + m.Module + "\n")
		} else {
			b.WriteString("same " + m.Module + "\n")
		}
	}
	b.WriteString("end\n")
	return b.String()
}
