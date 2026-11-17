package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/evidence"
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
	// flight is the blobs being read now: a second asker waits for the first
	// one's answer.
	flight map[string]*blobRead
}

type blobRead struct {
	done chan struct{}
	raw  []byte
	err  error
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
	g.prefetch(ctx, repo, listing)
	return unitplan.NewTree(rootID, entries, func(id string) ([]byte, error) { return g.blob(ctx, repo, id) })
}

// coldBlobs is how many Go files of a tree may be missing from the cache
// before they are read side by side rather than one after another as the
// plan walks them; prefetchers is how many read at once.
const (
	coldBlobs   = 100
	prefetchers = 12
)

// prefetch reads the Go files of a tree that the cache does not hold, a few
// at a time, when there are many of them: a process that has just started.
// 2026-11-16 23:05 CST on STG: after a delivery restarted platform-api,
// Work > CI's "who depends on whom" was still "Reading the domains of main"
// after two minutes, because /ci/domains and /units/config read 1,424 files
// one after another. The files are the same and each is read once; only the
// order changes. A file that fails here is read again when it is asked for.
func (g *giteaTrees) prefetch(ctx context.Context, repo string, listing map[string]commitTreeEntry) {
	var missing []string
	g.mu.Lock()
	for p, e := range listing {
		if e.Type != "blob" || !(strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "/go.mod") || p == "go.mod") {
			continue
		}
		if _, ok := g.blobs[e.SHA]; !ok {
			missing = append(missing, e.SHA)
		}
	}
	g.mu.Unlock()
	if len(missing) < coldBlobs {
		return
	}
	next := make(chan string)
	var wg sync.WaitGroup
	for i := 0; i < prefetchers; i++ {
		wg.Add(1)
		go func() {
			defer safego.Recover("gitea-prefetch")
			defer wg.Done()
			for id := range next {
				_, _ = g.blob(ctx, repo, id)
			}
		}()
	}
feed:
	for _, id := range missing {
		select {
		case next <- id:
		case <-ctx.Done():
			break feed
		}
	}
	close(next)
	wg.Wait()
}

func (g *giteaTrees) blob(ctx context.Context, repo, id string) ([]byte, error) {
	// One read a blob at a time. 2026-11-16 21:59 CST: the redesigned CI page
	// asks for /units/config and /ci/domains together, each walks every Go file
	// of main's head, and on a platform-api that had just started both missed
	// the cache for all 1,424 blobs: Gitea served 2,795 blob reads in two
	// minutes (the communication line's count).
	g.mu.Lock()
	if b, ok := g.blobs[id]; ok {
		g.mu.Unlock()
		return b, nil
	}
	if other, ok := g.flight[id]; ok {
		g.mu.Unlock()
		select {
		case <-other.done:
			return other.raw, other.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	mine := &blobRead{done: make(chan struct{})}
	if g.flight == nil {
		g.flight = map[string]*blobRead{}
	}
	g.flight[id] = mine
	g.mu.Unlock()
	mine.raw, mine.err = g.readBlob(ctx, repo, id)
	g.mu.Lock()
	delete(g.flight, id)
	g.mu.Unlock()
	close(mine.done)
	return mine.raw, mine.err
}

// readBlob reads one blob from Gitea and keeps it.
func (g *giteaTrees) readBlob(ctx context.Context, repo, id string) ([]byte, error) {
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

// buildUnitPlan computes the plan of a commit from its tree and its pins'.
func buildUnitPlan(ctx context.Context, trees func(ctx context.Context, repo, commit string) (*unitplan.Tree, error), repo, commit string) (*unitplan.Plan, error) {
	t, err := trees(ctx, repo, commit)
	if err != nil {
		return nil, err
	}
	return unitplan.Build(repo, commit, t, unitplan.Source{Pinned: func(pinned, at string) (*unitplan.Tree, error) { return trees(ctx, pinned, at) }})
}

// unitPlan computes the plan and reads the ledger for each of its keys.
func unitPlan(ctx context.Context, db store.DB, trees func(ctx context.Context, repo, commit string) (*unitplan.Tree, error), repo, commit string, withInputs bool) (unitPlanAnswer, error) {
	plan, err := buildUnitPlan(ctx, trees, repo, commit)
	if err != nil {
		return unitPlanAnswer{}, err
	}
	out := unitPlanAnswer{Repo: repo, Commit: commit, Tree: plan.Tree, Pins: plan.Pins, Units: make([]unitPlanTask, 0, len(plan.Units)), Counts: map[string]int{}}
	for _, u := range plan.Units {
		rows, err := evidence.EvidenceByKey(ctx, db, repo, u.Unit, u.Task, u.Key)
		if err != nil {
			return unitPlanAnswer{}, err
		}
		task := unitPlanTask{Unit: u.Unit, Task: u.Task, Module: u.Module, Key: u.Key, Shards: u.Shards, Verdict: evidence.EvidenceVerdict(rows)}
		if withInputs {
			task.Inputs = u.Inputs
		}
		out.Counts[task.Verdict]++
		if task.Verdict != evidence.EvidenceGreen {
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
