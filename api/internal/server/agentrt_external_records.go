package server

import (
	"bytes"
	"context"
	gitsha1 "crypto/sha1" // #nosec G505 -- git object ids are sha1
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// wireExternalRecords supplies bounded, read-only lists for durable backfill.
func (s *Server) wireExternalRecords(src *controller.Sources) {
	src.Questions = s.questionRuntime.agentRuntimeQuestionObservations()
	if s.approvals != nil {
		reads := newBoundedReads(2)
		src.Approvals = func(ctx context.Context) ([]store.ApprovalFact, error) {
			items, ok, err := within(ctx, reads, func() ([]approvals.Approval, bool, error) {
				items, err := s.approvals.Observations()
				return items, true, err
			})
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, fmt.Errorf("approval observations unavailable")
			}
			now := time.Now()
			var out []store.ApprovalFact
			for _, a := range items {
				if f, ok := approvalObservation(a, now); ok {
					out = append(out, f)
				}
			}
			return out, nil
		}
	}
	if s.delivery != nil {
		src.CommitTree = giteaCommitTree(s.delivery.GiteaAccess, &http.Client{Timeout: 10 * time.Second})
		src.PipelineRuns = func(ctx context.Context) ([]store.PipelineRun, error) {
			items, err := s.delivery.PipelineRunObservations(ctx)
			if err != nil {
				return nil, err
			}
			var out []store.PipelineRun
			for _, item := range items {
				if f, ok := pipelineRunFact(item); ok {
					out = append(out, f)
				}
			}
			return out, nil
		}
	}
}

// pipelineRecordsEveryEnv sets how often the workers list the retained
// pipeline runs for the ones that completed, and pipelineRecordsFullEveryEnv
// how often such a pass compares every listed run with the database whatever
// it remembers: Go durations (30s, 5m), not under pipelineRecordsEveryMin.
// Unset, the controller's defaults hold (controller.DefaultPipelineEvery,
// controller.DefaultPipelineFullEvery).
const (
	pipelineRecordsEveryEnv     = "AGENTRT_PIPELINE_RECORDS_EVERY"
	pipelineRecordsFullEveryEnv = "AGENTRT_PIPELINE_RECORDS_FULL_EVERY"
	pipelineRecordsEveryMin     = 5 * time.Second
)

// pipelineRecordsDuration reads one of them; zero is the controller's default.
func pipelineRecordsDuration(name string) time.Duration {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return 0
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < pipelineRecordsEveryMin {
		slog.Error("agentrt: "+name+" is not a duration of at least "+pipelineRecordsEveryMin.String()+"; the default holds", "value", v)
		return 0
	}
	return d
}

// wirePipelineRecords sets how often the controller records completed runs
// and has a newly recorded run start a pass of the release recorder, so the
// run's release record is written then and not at the recorder's own next
// interval.
func (s *Server) wirePipelineRecords(c *controller.Controller) {
	c.PipelineEvery = pipelineRecordsDuration(pipelineRecordsEveryEnv)
	c.PipelineFullEvery = pipelineRecordsDuration(pipelineRecordsFullEveryEnv)
	if s.releases != nil && s.releases.Service() != nil {
		c.PipelineRecorded = s.releases.Service().Kick
	}
}

func approvalObservation(a approvals.Approval, now time.Time) (store.ApprovalFact, bool) {
	// Leave time transitions to the sweeping writer, whose DecidedAt is
	// the sweep time. A stale file must never reverse its terminal record.
	if a.Status == approvals.StatusPending && now.After(a.ExpiresAt) {
		return store.ApprovalFact{}, false
	}
	if a.Status == approvals.StatusApproved && a.Execution != nil && !a.Execution.Deadline.IsZero() && now.After(a.Execution.Deadline) {
		return store.ApprovalFact{}, false
	}
	return approvalFact(a)
}

var pipelineReason = regexp.MustCompile("^[A-Za-z]{1,64}$")

func pipelineRunFact(run unstructured.Unstructured) (store.PipelineRun, bool) {
	f := store.PipelineRun{Run: run.GetName(), UID: string(run.GetUID()), Kind: "other", CreatedAt: run.GetCreationTimestamp().Time}
	f.Pipeline, _, _ = unstructured.NestedString(run.Object, "spec", "pipelineRef", "name")
	switch {
	case strings.HasPrefix(f.Pipeline, "bifrost-ci-"):
		f.Kind = "ci"
	case strings.HasPrefix(f.Pipeline, "bifrost-deliver-"):
		f.Kind = "delivery"
	}
	conditions, _, _ := unstructured.NestedSlice(run.Object, "status", "conditions")
	for _, value := range conditions {
		c, ok := value.(map[string]any)
		if !ok || c["type"] != "Succeeded" {
			continue
		}
		switch c["status"] {
		case "True":
			f.Result = "Succeeded"
		case "False":
			f.Result = "Failed"
		default:
			return store.PipelineRun{}, false
		}
		reason, _ := c["reason"].(string)
		if pipelineReason.MatchString(reason) {
			f.Reason = reason
		}
		break
	}
	if f.Result == "" {
		return store.PipelineRun{}, false
	}
	readTime := func(key string) time.Time {
		value, _, _ := unstructured.NestedString(run.Object, "status", key)
		at, _ := time.Parse(time.RFC3339, value)
		return at
	}
	f.StartedAt, f.CompletedAt = readTime("startTime"), readTime("completionTime")
	if f.Result == "Failed" && f.StartedAt.IsZero() {
		f.StartedAt = f.CreatedAt
	}
	f.Repo = run.GetLabels()["bifrost.io/repo"]
	if f.Repo == "" {
		f.Repo = map[string]string{
			"bifrost-deliver-platform":      "bifrost-platform",
			"bifrost-deliver-platform-prod": "bifrost-platform",
		}[f.Pipeline]
	}
	params, _, _ := unstructured.NestedSlice(run.Object, "spec", "params")
	for _, value := range params {
		p, ok := value.(map[string]any)
		if !ok {
			continue
		}
		v, _ := p["value"].(string)
		switch p["name"] {
		case "revision":
			f.Revision = v
		}
	}
	results, _, _ := unstructured.NestedSlice(run.Object, "status", "results")
	inputs, own := map[string]*store.RepoAt{}, map[string]*store.RepoAt{}
	for _, value := range results {
		r, _ := value.(map[string]any)
		v, _ := r["value"].(string)
		name, _ := r["name"].(string)
		switch name {
		case "platform-tree":
			// The tree of the run's own repository: bifrost-ci-infra reports
			// the platform checkout it used under the same name.
			if f.Pipeline != "bifrost-ci-infra" {
				f.Tree = v
			}
		case "tested-by":
			f.TestedBy = v
		case "api-scope":
			f.Scope = v
		}
		// What a CI run tested, for the evidence ledger: <short>-commit and
		// <short>-tree name a repository it used (ciResultRepos), source-* (or
		// repo-*) the repository of the run itself, which wins when both name it.
		short, field, _ := strings.Cut(name, "-")
		repo, into := ciResultRepos[short], inputs
		if short == "source" || short == "repo" {
			repo, into = f.Repo, own
		}
		if f.Kind != "ci" || repo == "" || !commitID.MatchString(v) || (field != "commit" && field != "tree") {
			continue
		}
		if into[repo] == nil {
			into[repo] = &store.RepoAt{Repo: repo}
		}
		if field == "commit" {
			into[repo].Commit = v
		} else {
			into[repo].Tree = v
		}
	}
	for repo, in := range own {
		inputs[repo] = in
	}
	for _, in := range inputs {
		f.Inputs = append(f.Inputs, *in)
	}
	sort.Slice(f.Inputs, func(i, j int) bool { return f.Inputs[i].Repo < f.Inputs[j].Repo })
	return f, true
}

// ciResultRepos is the repository a CI pipeline result names by its prefix
// (ui-commit, infra-tree); a result with another prefix is not an input.
var ciResultRepos = map[string]string{
	"platform": "bifrost-platform", "ui": "bifrost-ui", "infra": "bifrost-trade-infra",
	"core": "bifrost-trade-core", "api": "bifrost-trade-api", "worker": "bifrost-trade-worker",
	"frontend": "bifrost-trade-frontend", "research": "bifrost-research",
}

// giteaCommitTree reads the git tree of a commit of <org>/<repo> from Gitea,
// with the delivery service's read-only credential. Gitea names no tree id
// for a commit (the tree sha of its commit and trees answers is the commit's
// own id, seen on STG 2026-11-13), so the id is computed as git does, from
// the entries of the commit's root tree.
func giteaCommitTree(access func(context.Context) (delivery.GiteaAccess, error), client *http.Client) func(context.Context, string, string) (string, error) {
	c := *client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return func(ctx context.Context, repo, commit string) (string, error) {
		if !giteaName.MatchString(repo) || strings.HasSuffix(repo, ".git") || !commitID.MatchString(commit) {
			return "", fmt.Errorf("gitea: %q of %q is not a commit of a repository", commit, repo)
		}
		acc, err := access(ctx)
		if err != nil {
			return "", fmt.Errorf("gitea access: %w", err)
		}
		base := strings.TrimRight(acc.Base, "/") + "/api/v1/repos/" + url.PathEscape(acc.Org) + "/" + url.PathEscape(repo) + "/git/trees/" + commit
		var entries []commitTreeEntry
		for page := 1; ; page++ {
			req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s?per_page=1000&page=%d", base, page), nil)
			if reqErr != nil {
				return "", reqErr
			}
			if acc.User != "" || acc.Pass != "" {
				req.SetBasicAuth(acc.User, acc.Pass)
			}
			var doc struct {
				Total *int              `json:"total_count"`
				Tree  []commitTreeEntry `json:"tree"`
			}
			err = func() error {
				resp, doErr := c.Do(req)
				if doErr != nil {
					return doErr
				}
				defer func() { _ = resp.Body.Close() }()
				if resp.StatusCode != http.StatusOK {
					return fmt.Errorf("HTTP %d", resp.StatusCode)
				}
				return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&doc)
			}()
			if err != nil || doc.Total == nil {
				return "", fmt.Errorf("gitea: %s/%s tree of %s: %v", acc.Org, repo, commit, err)
			}
			entries = append(entries, doc.Tree...)
			if len(entries) == *doc.Total {
				break
			}
			if len(doc.Tree) == 0 || len(entries) > *doc.Total || page >= 100 {
				return "", fmt.Errorf("gitea: %s/%s tree of %s: %d entries read of %d", acc.Org, repo, commit, len(entries), *doc.Total)
			}
		}
		tree, err := gitTreeID(entries)
		if err != nil {
			return "", fmt.Errorf("gitea: %s/%s tree of %s: %w", acc.Org, repo, commit, err)
		}
		return tree, nil
	}
}

// gitTreeID is the id git gives a tree with these entries (one directory,
// not its subtrees): the sha1 of "tree <size>\0" and, for each entry in git's
// order (by name, a directory as if its name ended in "/"), "<mode> <name>\0"
// and the 20 bytes of its id.
func gitTreeID(entries []commitTreeEntry) (string, error) {
	type entry struct {
		key, mode, name string
		id              []byte
	}
	list := make([]entry, 0, len(entries))
	seen := map[string]bool{}
	for _, e := range entries {
		id, err := hex.DecodeString(e.SHA)
		mode := strings.TrimLeft(e.Mode, "0")
		ok := map[string]bool{"blob 100644": true, "blob 100755": true, "blob 120000": true, "tree 40000": true, "commit 160000": true}[e.Type+" "+mode]
		if err != nil || len(id) != 20 || !ok || e.Path == "" || strings.ContainsAny(e.Path, "/\x00") || seen[e.Path] {
			return "", fmt.Errorf("invalid tree entry %q", e.Path)
		}
		seen[e.Path] = true
		key := e.Path
		if e.Type == "tree" {
			key += "/"
		}
		list = append(list, entry{key, mode, e.Path, id})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].key < list[j].key })
	var body bytes.Buffer
	for _, e := range list {
		body.WriteString(e.mode + " " + e.name + "\x00")
		body.Write(e.id)
	}
	h := gitsha1.New() // #nosec G401 -- git's object id, not a security hash
	_, _ = fmt.Fprintf(h, "tree %d\x00", body.Len())
	h.Write(body.Bytes())
	return hex.EncodeToString(h.Sum(nil)), nil
}
