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

	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
)

// The unit plan route (GET /api/v1/units/plan) lives in agentrt/evidence
// (PlanRoute); this file is the Gitea tree reader it and the other unit reads
// stand on.

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
	return unitplan.BuildFrom(ctx, trees, repo, commit)
}
