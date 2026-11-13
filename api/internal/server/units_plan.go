package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
)

// GET /api/v1/units/plan?repo=…&commit=… is CI's plan of one commit (CI
// design v4 §3.2, landing stage 2 block 2-B): the commit set (the commit and
// the pins of its bifrost.units.yaml), every unit's task with its key, what
// the evidence ledger says of that key, and so what would have to run.
// Read-only: it computes and reads; it starts nothing and writes nothing.
// With inputs=1 each task carries the document its key is the hash of.

type unitPlanTask struct {
	Unit    string         `json:"unit"`
	Task    string         `json:"task"`
	Module  string         `json:"module"`
	Key     string         `json:"input_key"`
	Shards  int            `json:"shards,omitempty"`
	Verdict string         `json:"verdict"` // the ledger's for this key: green, red, none
	Inputs  map[string]any `json:"inputs,omitempty"`
}

type unitPlanAnswer struct {
	Repo   string             `json:"repo"`
	Commit string             `json:"commit"`
	Tree   string             `json:"tree"`
	Pins   []unitplan.PlanPin `json:"pins"`
	Units  []unitPlanTask     `json:"units"`
	// ToRun counts the tasks whose key the ledger is not green for.
	ToRun  int            `json:"to_run"`
	Counts map[string]int `json:"counts"` // by verdict
}

// giteaTrees reads commits' trees from Gitea for unitplan: the whole listing
// of a commit once, and a file's bytes by blob id, kept (a blob never
// changes) up to blobCacheBytes for the plans that follow.
type giteaTrees struct {
	access func(context.Context) (delivery.GiteaAccess, error)
	client *http.Client
	mu     sync.Mutex
	blobs  map[string][]byte
	size   int
}

const blobCacheBytes = 96 << 20

func (g *giteaTrees) get(ctx context.Context, path string, limit int64) ([]byte, error) {
	acc, err := g.access(ctx)
	if err != nil {
		return nil, fmt.Errorf("gitea access: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(acc.Base, "/")+"/api/v1/repos/"+url.PathEscape(acc.Org)+"/"+path, nil)
	if err != nil {
		return nil, err
	}
	if acc.User != "" || acc.Pass != "" {
		req.SetBasicAuth(acc.User, acc.Pass)
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gitea: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gitea: %s: HTTP %d", path, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || int64(len(body)) > limit {
		return nil, fmt.Errorf("gitea: %s: unreadable or more than %d bytes (%v)", path, limit, err)
	}
	return body, nil
}

// tree is the whole tree of repo at commit. The root id is computed from the
// root's entries (Gitea names no tree id for a commit, see giteaCommitTree).
func (g *giteaTrees) tree(ctx context.Context, repo, commit string) (*unitplan.Tree, error) {
	if !giteaName.MatchString(repo) || strings.HasSuffix(repo, ".git") || !commitID.MatchString(commit) {
		return nil, fmt.Errorf("gitea: %q of %q is not a commit of a repository", commit, repo)
	}
	listing, err := commitTree(func(u string, limit int64) ([]byte, error) { return g.get(ctx, u, limit) }, url.PathEscape(repo)+"/git/trees/"+commit)
	if err != nil {
		return nil, err
	}
	var entries []unitplan.Entry
	var root []commitTreeEntry
	for p, e := range listing {
		if !strings.Contains(p, "/") {
			root = append(root, e)
		}
		if e.Type == "commit" {
			continue // a submodule: not content of this repository
		}
		entries = append(entries, unitplan.Entry{Path: p, Dir: e.Type == "tree", ID: e.SHA})
	}
	rootID, err := gitTreeID(root)
	if err != nil {
		return nil, err
	}
	return unitplan.NewTree(rootID, entries, func(id string) ([]byte, error) { return g.blob(ctx, repo, id) })
}

func (g *giteaTrees) blob(ctx context.Context, repo, id string) ([]byte, error) {
	g.mu.Lock()
	b, ok := g.blobs[id]
	g.mu.Unlock()
	if ok {
		return b, nil
	}
	body, err := g.get(ctx, url.PathEscape(repo)+"/git/blobs/"+id, 16<<20)
	if err != nil {
		return nil, err
	}
	var doc struct {
		SHA, Encoding, Content string
	}
	if err = json.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(doc.Content)
	if err != nil || doc.Encoding != "base64" || gitBlobID(string(raw)) != id {
		return nil, fmt.Errorf("gitea: blob %s of %s is not what was asked for", id, repo)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.size+len(raw) > blobCacheBytes {
		g.blobs, g.size = nil, 0 // start over rather than track ages: a plan reads a few MB
	}
	if g.blobs == nil {
		g.blobs = map[string][]byte{}
	}
	g.blobs[id] = raw
	g.size += len(raw)
	return raw, nil
}

// unitPlan computes the plan and reads the ledger for each of its keys.
func unitPlan(ctx context.Context, db store.DB, trees func(ctx context.Context, repo, commit string) (*unitplan.Tree, error), repo, commit string, withInputs bool) (unitPlanAnswer, error) {
	t, err := trees(ctx, repo, commit)
	if err != nil {
		return unitPlanAnswer{}, err
	}
	plan, err := unitplan.Build(repo, commit, t, unitplan.Source{Pinned: func(pinned, at string) (*unitplan.Tree, error) { return trees(ctx, pinned, at) }})
	if err != nil {
		return unitPlanAnswer{}, err
	}
	out := unitPlanAnswer{Repo: repo, Commit: commit, Tree: plan.Tree, Pins: plan.Pins, Units: make([]unitPlanTask, 0, len(plan.Units)), Counts: map[string]int{}}
	for _, u := range plan.Units {
		rows, err := store.EvidenceByKey(ctx, db, repo, u.Unit, u.Task, u.Key)
		if err != nil {
			return unitPlanAnswer{}, err
		}
		task := unitPlanTask{Unit: u.Unit, Task: u.Task, Module: u.Module, Key: u.Key, Shards: u.Shards, Verdict: store.EvidenceVerdict(rows)}
		if withInputs {
			task.Inputs = u.Inputs
		}
		out.Counts[task.Verdict]++
		if task.Verdict != store.EvidenceGreen {
			out.ToRun++
		}
		out.Units = append(out.Units, task)
	}
	return out, nil
}

func (s *Server) handleUnitPlan(w http.ResponseWriter, r *http.Request) {
	repo, commit := strings.TrimSpace(r.URL.Query().Get("repo")), strings.TrimSpace(r.URL.Query().Get("commit"))
	if !giteaName.MatchString(repo) || strings.HasSuffix(repo, ".git") || !commitID.MatchString(commit) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "repo is a repository name and commit a 40-character lowercase SHA"})
		return
	}
	if s.agentRuntime == nil || s.agentRuntime.api == nil || s.agentRuntime.api.DB == nil || s.delivery == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the evidence ledger is not available on this platform"})
		return
	}
	s.unitTreesOnce.Do(func() {
		s.unitTrees = &giteaTrees{access: s.delivery.GiteaAccess, client: &http.Client{Timeout: 20 * time.Second}}
	})
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()
	out, err := unitPlan(ctx, s.agentRuntime.api.DB, s.unitTrees.tree, repo, commit, r.URL.Query().Get("inputs") == "1")
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}
