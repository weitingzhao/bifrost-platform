package landinggit

import (
	"context"
	"errors"
	"regexp"
	"strings"
)

// Wave refs are separate from landing refs: these operations never push main.
var waveRef = regexp.MustCompile(`^refs/heads/wave/[A-Za-z0-9][A-Za-z0-9_/-]*$`)

func (g *Git) waveAllowed(ref string) bool {
	return !g.reader && waveRef.MatchString(ref) && !strings.Contains(ref, "//") && !strings.HasSuffix(ref, "/")
}

// CutWave creates ref at base, requiring that the remote branch not exist.
func (g *Git) CutWave(ctx context.Context, ref, base string) error {
	if !g.waveAllowed(ref) || !fullSHA.MatchString(base) {
		return errors.New("invalid wave branch or base")
	}
	w, err := g.Open(ctx, base)
	if err != nil {
		return err
	}
	defer w.Close()
	_, err = g.run(ctx, w.dir, true, nil, nil, "push", "--porcelain", "--force-with-lease="+ref+":", "origin", base+":"+ref)
	return err
}

// AppendWave appends the accepted single commit a to head. The exact head
// lease fences concurrent writers, including when a needs replaying onto head.
// Replays retain the original author, message and trailers without adding
// landing provenance, since a wave append does not land the change.
func (g *Git) AppendWave(ctx context.Context, ref, head, a, base string) (string, bool, error) {
	if !g.waveAllowed(ref) || !fullSHA.MatchString(head) || !fullSHA.MatchString(a) || !fullSHA.MatchString(base) {
		return "", false, errors.New("invalid wave append")
	}
	// Git may report an already matching destination as up to date without
	// checking its lease. Check it here too, including that no-op case.
	remote, err := g.run(ctx, "", true, nil, nil, "ls-remote", "--refs", g.remote, ref)
	if err != nil {
		return "", false, err
	}
	fields := strings.Fields(remote)
	if len(fields) != 2 || fields[0] != head || fields[1] != ref {
		return "", false, errors.New("wave head lease lost")
	}
	w, err := g.Open(ctx, a)
	if err != nil {
		return "", false, err
	}
	defer w.Close()
	if err = w.Fetch(ctx, head, base); err != nil {
		return "", false, err
	}
	parents, err := w.local(ctx, "rev-list", "--parents", "-n", "1", a)
	if err != nil {
		return "", false, err
	}
	p := strings.Fields(parents)
	if len(p) != 2 || p[1] != base {
		return "", false, errors.New("result is not one commit on base")
	}
	commit, rebased := a, base != head
	if rebased {
		if _, err = w.local(ctx, "checkout", "--detach", head); err != nil {
			return "", false, err
		}
		if _, err = w.local(ctx, "cherry-pick", "--no-commit", a); err != nil {
			// Only an unmerged index is a real conflict. Other failures must not
			// send a task into conflict rework.
			paths, readErr := w.local(ctx, "diff", "--name-only", "--diff-filter=U")
			if readErr == nil && strings.TrimSpace(paths) != "" {
				return "", false, w.conflict(ctx, head)
			}
			return "", false, err
		}
		env := []string{"GIT_COMMITTER_NAME=Agent Runtime", "GIT_COMMITTER_EMAIL=agentrt@bifrost.local"}
		if _, err = g.run(ctx, w.dir, false, nil, env, "commit", "--no-verify", "--allow-empty", "--cleanup=verbatim", "-C", a); err != nil {
			return "", false, err
		}
		out, err := w.local(ctx, "rev-parse", "HEAD")
		if err != nil {
			return "", false, err
		}
		commit = strings.TrimSpace(out)
	}
	_, err = g.run(ctx, w.dir, true, nil, nil, "push", "--porcelain", "--force-with-lease="+ref+":"+head, "origin", commit+":"+ref)
	if err != nil {
		return "", false, err
	}
	return commit, rebased, nil
}
