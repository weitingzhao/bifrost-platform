package agentd

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Deleting attempt branches (TD-383; the Owner's 「分支 B 用 agentd」): the
// platform says which (store/prune.go has the rule), agentd deletes them with
// its own Gitea identity, the one it pushes them with, and says what happened.
// A branch is deleted only while it still names the commit the platform
// gave: git's lease makes the push refuse it otherwise. Until the Owner lifts
// Gitea's protection of agent/**, Gitea refuses every deletion: the first
// refusal ends the round and is reported, and the platform pauses the
// handing out; it is never an error that stops agentd.

// PruneEvery is how often agentd asks.
const PruneEvery = 10 * time.Minute

// Prune is a branch the platform lets this host delete.
type Prune struct {
	Repo   string `json:"repo"`
	Ref    string `json:"ref"`
	Commit string `json:"commit"`
	Task   string `json:"task_id"`
	Why    string `json:"why"`
}

// PruneResult is what became of one.
type PruneResult struct {
	Repo    string `json:"repo"`
	Ref     string `json:"ref"`
	Commit  string `json:"commit"`
	Outcome string `json:"outcome"` // deleted, gone, refused, failed
	Detail  string `json:"detail,omitempty"`
}

// Prunes asks which branches this host may delete now.
func (c *Client) Prunes(ctx context.Context) ([]Prune, error) {
	var out struct {
		Prunes []Prune `json:"prunes"`
	}
	_, err := c.call(ctx, http.MethodGet, "/prunes?host_id="+url.QueryEscape(c.Host), nil, nil, &out)
	return out.Prunes, err
}

// ReportPrunes says what became of them.
func (c *Client) ReportPrunes(ctx context.Context, results []PruneResult) error {
	_, err := c.post(ctx, "/prunes", map[string]any{"results": results}, nil)
	return err
}

var (
	attemptBranch = regexp.MustCompile(`^refs/heads/agent/t_[0-9A-HJKMNP-TV-Z]{26}/a[1-9][0-9]{0,5}$`)
	objectID      = regexp.MustCompile(`^[0-9a-f]{40}$`)
	// unreached is git's way of saying it did not get an answer from Gitea.
	unreached = regexp.MustCompile(`(?i)could not resolve host|failed to connect|unable to access|connection (refused|reset|timed out)|operation timed out|could not read from remote`)
)

// Prune asks the platform which branches to delete, deletes each that still
// names its commit (one that is gone is reported gone; one that names another
// commit is left and not reported: the platform does not give it again), and
// reports what happened. It returns how many it deleted.
func (d *Daemon) Prune(ctx context.Context) (int, error) {
	ps, err := d.Client.Prunes(ctx)
	if err != nil || len(ps) == 0 {
		return 0, err
	}
	// git push needs a repository to run in; an empty bare one of agentd's
	// own, which no executor uses.
	scratch := filepath.Join(d.Root, "prune.git")
	env := isolatedGit(d.GitEnv)
	if _, err := os.Stat(filepath.Join(scratch, "HEAD")); err != nil {
		if _, err = gitRaw(ctx, d.Root, env, "init", "--quiet", "--bare", scratch); err != nil {
			return 0, err
		}
	}
	deleted := 0
	var results []PruneResult
	for _, p := range ps {
		if !attemptBranch.MatchString(p.Ref) || !objectID.MatchString(p.Commit) || !taskName.MatchString(p.Repo) {
			continue // not something agentd deletes
		}
		remote := d.pushRemote(p.Repo)
		r := PruneResult{Repo: p.Repo, Ref: p.Ref, Commit: p.Commit}
		head, ok, detail := lsRemote(ctx, scratch, env, remote, p.Ref)
		switch {
		case !ok:
			r.Outcome, r.Detail = "failed", detail
		case head == "":
			r.Outcome = "gone"
		case head != p.Commit:
			continue // moved since: not this attempt's result any more
		default:
			r.Outcome, r.Detail = deleteBranch(ctx, scratch, env, remote, p.Ref, p.Commit)
		}
		results = append(results, r)
		if r.Outcome == "deleted" {
			deleted++
		}
		if r.Outcome == "refused" || r.Outcome == "failed" {
			break // Gitea's protection, or Gitea out of reach, holds for the rest alike
		}
	}
	if len(results) == 0 {
		return 0, nil
	}
	return deleted, d.Client.ReportPrunes(ctx, results)
}

// lsRemote is the commit ref names on remote, "" when there is no such ref;
// ok false when Gitea could not be asked.
func lsRemote(ctx context.Context, dir string, env []string, remote, ref string) (string, bool, string) {
	var out, errb bytes.Buffer
	cmd := gitCmd(ctx, nil, dir, env, "ls-remote", remote, ref)
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", false, gitWords(errb.String(), err)
	}
	head, _, _ := strings.Cut(strings.TrimSpace(out.String()), "\t")
	return head, true, ""
}

// deleteBranch deletes ref on remote if it still names commit: deleted, or
// refused or failed with git's words.
func deleteBranch(ctx context.Context, dir string, env []string, remote, ref, commit string) (string, string) {
	var out, errb bytes.Buffer
	cmd := gitCmd(ctx, nil, dir, env, "push", "--porcelain", "--force-with-lease="+ref+":"+commit, remote, ":"+ref)
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		words := gitWords(out.String()+"\n"+errb.String(), err)
		if ctx.Err() != nil || unreached.MatchString(words) {
			return "failed", words
		}
		return "refused", words
	}
	return "deleted", ""
}

// gitWords is what of git's output says why, on one line: the lines from
// the remote and the rejections, not the remote's address, scrubbed as an
// action's words are (an address given with a credential in it).
func gitWords(text string, err error) string {
	var keep []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		low := strings.ToLower(line)
		if line == "" || strings.HasPrefix(low, "to ") || line == "Done" {
			continue
		}
		if strings.HasPrefix(low, "remote:") || strings.Contains(low, "rejected") || strings.HasPrefix(low, "error:") ||
			strings.HasPrefix(low, "fatal:") || strings.HasPrefix(line, "!") {
			keep = append(keep, line)
		}
	}
	if len(keep) == 0 {
		return fmt.Sprintf("git: %v", err)
	}
	words := scrub(strings.Join(keep, " | "))
	if len(words) > 400 {
		words = strings.ToValidUTF8(words[:400], "") + "…"
	}
	return words
}
