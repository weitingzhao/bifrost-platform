package agentd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

// PruneEvery is how often agentd asks; PruneWork bounds a round's asking and
// git; PruneReport bounds sending what it did, on a clock of its own, so a
// round that ran out still reports (review 436 B1).
const (
	PruneEvery  = 10 * time.Minute
	PruneWork   = 2 * time.Minute
	PruneReport = 30 * time.Second
)

// prunePending is where a round's results wait until the platform has them:
// written before they are sent, removed once they are; the next round sends
// what is there first.
const prunePending = "prune-pending.json"

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

// errReportRefused is the platform refusing a report as it is: sending it
// again cannot help.
var errReportRefused = errors.New("the platform refused the report")

// ReportPrunes says what became of them.
func (c *Client) ReportPrunes(ctx context.Context, results []PruneResult) error {
	code, err := c.post(ctx, "/prunes", map[string]any{"results": results}, nil)
	if err != nil && (code == http.StatusBadRequest || code == http.StatusUnprocessableEntity) {
		return fmt.Errorf("%w: %v", errReportRefused, err)
	}
	return err
}

var (
	attemptBranch = regexp.MustCompile(`^refs/heads/agent/t_[0-9A-HJKMNP-TV-Z]{26}/a[1-9][0-9]{0,5}$`)
	objectID      = regexp.MustCompile(`^[0-9a-f]{40}$`)
	// unreached is git's way of saying it did not get an answer from Gitea.
	unreached = regexp.MustCompile(`(?i)could not resolve host|failed to connect|unable to access|connection (refused|reset|timed out)|operation timed out|could not read from remote`)
	// userinfo is whatever an address carries before its host: a token
	// given as the user name too (review 436 C2).
	userinfo = regexp.MustCompile(`://[^/\s@'"]+@`)
)

// Prune asks the platform which branches to delete, deletes each that still
// names its commit (one that is gone is reported gone; one that names another
// commit is left and not reported: the platform does not give it again), and
// reports what happened. Results not yet taken by the platform are kept on
// disk and sent first next time, before anything new is asked. It returns
// how many it deleted.
func (d *Daemon) Prune(ctx context.Context) (int, error) {
	pending := filepath.Join(d.Root, prunePending)
	if b, err := os.ReadFile(pending); err == nil {
		var left []PruneResult
		if json.Unmarshal(b, &left) != nil {
			_ = os.Remove(pending) // not ours to read: nothing more can be done with it
		} else if err = d.reportPrunes(ctx, pending, left); err != nil {
			return 0, fmt.Errorf("results of an earlier round not taken yet: %w", err)
		}
	}
	wctx, cancel := context.WithTimeout(ctx, PruneWork)
	defer cancel()
	ps, err := d.Client.Prunes(wctx)
	if err != nil || len(ps) == 0 {
		return 0, err
	}
	// git push needs a repository to run in; an empty bare one of agentd's
	// own, which no executor uses.
	scratch := filepath.Join(d.Root, "prune.git")
	env := isolatedGit(d.GitEnv)
	if _, err := os.Stat(filepath.Join(scratch, "HEAD")); err != nil {
		if _, err = gitRaw(wctx, d.Root, env, "init", "--quiet", "--bare", scratch); err != nil {
			return 0, err
		}
	}
	deleted := 0
	var results []PruneResult
	for _, p := range ps {
		if wctx.Err() != nil {
			break // out of time: what was done is reported
		}
		if !attemptBranch.MatchString(p.Ref) || !objectID.MatchString(p.Commit) || !taskName.MatchString(p.Repo) {
			continue // not something agentd deletes
		}
		remote := d.pushRemote(p.Repo)
		r := PruneResult{Repo: p.Repo, Ref: p.Ref, Commit: p.Commit}
		head, ok, detail := lsRemote(wctx, scratch, env, remote, p.Ref)
		switch {
		case !ok:
			r.Outcome, r.Detail = "failed", detail
		case head == "":
			r.Outcome = "gone"
		case head != p.Commit:
			continue // moved since: not this attempt's result any more
		default:
			r.Outcome, r.Detail = deleteBranch(wctx, scratch, env, remote, p.Ref, p.Commit)
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
	return deleted, d.reportPrunes(ctx, pending, results)
}

// reportPrunes keeps results at pending, sends them on a clock of their own,
// and removes pending once the platform has them, or has refused them as
// they are.
func (d *Daemon) reportPrunes(ctx context.Context, pending string, results []PruneResult) error {
	b, err := json.Marshal(results)
	if err != nil {
		return err
	}
	if err = writeDurably(pending, b); err != nil {
		return err
	}
	rctx, cancel := context.WithTimeout(ctx, PruneReport)
	defer cancel()
	if err = d.Client.ReportPrunes(rctx, results); err != nil {
		if errors.Is(err, errReportRefused) {
			_ = os.Remove(pending) // kept, it would stop every later round
		}
		return err
	}
	return os.Remove(pending)
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
	words := userinfo.ReplaceAllString(scrub(strings.Join(keep, " | ")), "://<secret>@")
	if len(words) > 400 {
		words = strings.ToValidUTF8(words[:400], "") + "…"
	}
	return words
}
