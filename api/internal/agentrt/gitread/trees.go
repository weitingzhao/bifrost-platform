package gitread

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"

	gitsha1 "crypto/sha1" // #nosec G505 -- Git object identity, not a credential or signature.

	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// Trees reads commits' trees and files from Gitea's API: the whole listing of
// a commit once, and a file's bytes by blob id, kept (a blob never changes) up
// to blobCacheBytes for the reads that follow.
type Trees struct {
	Access func(context.Context) (Access, error)
	Client *http.Client
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

// Get reads one answer of the API under the organization's repositories.
func (g *Trees) Get(ctx context.Context, path string, limit int64) ([]byte, error) {
	acc, err := g.Access(ctx)
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
	resp, err := g.Client.Do(req)
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

// Listing is every entry of the tree of a commit of repo, by path.
func (g *Trees) Listing(ctx context.Context, repo, commit string) (map[string]TreeEntry, error) {
	return ListTree(func(u string, limit int64) ([]byte, error) { return g.Get(ctx, u, limit) }, url.PathEscape(repo)+"/git/trees/"+commit)
}

// coldBlobs is how many Go files of a tree may be missing from the cache
// before they are read side by side rather than one after another as the
// plan walks them; prefetchers is how many read at once.
const (
	coldBlobs   = 100
	prefetchers = 12
)

// Prefetch reads the Go files of a tree that the cache does not hold, a few
// at a time, when there are many of them: a process that has just started.
// 2026-11-16 23:05 CST on STG: after a delivery restarted platform-api,
// Work > CI's "who depends on whom" was still "Reading the domains of main"
// after two minutes, because /ci/domains and /units/config read 1,424 files
// one after another. The files are the same and each is read once; only the
// order changes. A file that fails here is read again when it is asked for.
func (g *Trees) Prefetch(ctx context.Context, repo string, listing map[string]TreeEntry) {
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
				_, _ = g.Blob(ctx, repo, id)
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

// Blob is a file's bytes by blob id, from the cache or Gitea.
func (g *Trees) Blob(ctx context.Context, repo, id string) ([]byte, error) {
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
func (g *Trees) readBlob(ctx context.Context, repo, id string) ([]byte, error) {
	body, err := g.Get(ctx, url.PathEscape(repo)+"/git/blobs/"+id, 16<<20)
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
	if err != nil || doc.Encoding != "base64" || blobID(string(raw)) != id {
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

// MainHead is the commit main of repo is at, as Gitea says.
func (g *Trees) MainHead(ctx context.Context, repo string) (string, error) {
	body, err := g.Get(ctx, url.PathEscape(repo)+"/branches/main", 1<<20)
	var branch struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	if err == nil {
		err = json.Unmarshal(body, &branch)
	}
	if err != nil || !CommitID.MatchString(branch.Commit.ID) {
		return "", fmt.Errorf("gitea: main of %s could not be read", repo)
	}
	return branch.Commit.ID, nil
}

// FirstParent is the first parent of a commit as Gitea has it, "" for a
// commit with none. It is the base a landing's commit is compared with: the
// CI run's own changes task compares with the merge base of main, which for a
// commit prepared on main's head is this parent, and has no base at all for a
// commit already on main.
func (g *Trees) FirstParent(ctx context.Context, repo, commit string) (string, error) {
	body, err := g.Get(ctx, url.PathEscape(repo)+"/git/commits/"+commit+"?stat=false&verification=false&files=false", 1<<20)
	if err != nil {
		return "", err
	}
	var doc struct {
		SHA     string `json:"sha"`
		Parents []struct {
			SHA string `json:"sha"`
		} `json:"parents"`
	}
	if err = json.Unmarshal(body, &doc); err != nil || doc.SHA != commit {
		return "", fmt.Errorf("gitea: %s commit %s: not that commit (%v)", repo, commit, err)
	}
	if len(doc.Parents) == 0 {
		return "", nil
	}
	if !CommitID.MatchString(doc.Parents[0].SHA) {
		return "", fmt.Errorf("gitea: %s commit %s: its parent is not a commit id", repo, commit)
	}
	return doc.Parents[0].SHA, nil
}

// ListTree reads the independently generated, paged tree, pinned by commit SHA.
// Gitea 1.21 sets truncated even on the final nonempty page when the total
// exceeds per_page. Completion therefore requires total_count distinct entries;
// a short/empty page, changing total/root, or missing pagination fields fails.
func ListTree(get func(string, int64) ([]byte, error), endpoint string) (map[string]TreeEntry, error) {
	entries := map[string]TreeEntry{}
	total, root := -1, ""
	for page := 1; page <= 1000; page++ {
		body, err := get(fmt.Sprintf("%s?recursive=true&per_page=1000&page=%d", endpoint, page), maxCommitDiff)
		if err != nil {
			return nil, err
		}
		var doc struct {
			SHA       string      `json:"sha"`
			Page      *int        `json:"page"`
			Total     *int        `json:"total_count"`
			Truncated *bool       `json:"truncated"`
			Tree      []TreeEntry `json:"tree"`
		}
		if err := json.Unmarshal(body, &doc); err != nil || doc.Page == nil || *doc.Page != page || doc.Total == nil ||
			*doc.Total < 0 || *doc.Total > 100000 || doc.Truncated == nil || !CommitID.MatchString(doc.SHA) {
			return nil, fmt.Errorf("gitea: invalid tree page %d", page)
		}
		if page == 1 {
			total, root = *doc.Total, doc.SHA
		}
		if total != *doc.Total || root != doc.SHA {
			return nil, fmt.Errorf("gitea: tree changed across pages")
		}
		for _, e := range doc.Tree {
			if e.Path == "" || e.Path == "." || path.Clean(e.Path) != e.Path || strings.HasPrefix(e.Path, "/") || e.Path == ".." || strings.HasPrefix(e.Path, "../") ||
				strings.ContainsAny(e.Path, "\x00\r\n\t") || !CommitID.MatchString(e.SHA) ||
				!((e.Type == "blob" && (e.Mode == "100644" || e.Mode == "100755" || e.Mode == "120000")) ||
					(e.Type == "tree" && e.Mode == "040000") || (e.Type == "commit" && e.Mode == "160000")) {
				return nil, fmt.Errorf("gitea: invalid tree entry")
			}
			if _, exists := entries[e.Path]; exists {
				return nil, fmt.Errorf("gitea: duplicate tree entry")
			}
			entries[e.Path] = e
		}
		if len(entries) > total {
			return nil, fmt.Errorf("gitea: tree exceeds total_count")
		}
		if len(entries) == total {
			return entries, nil
		}
		if len(doc.Tree) == 0 || !*doc.Truncated {
			return nil, fmt.Errorf("gitea: incomplete tree")
		}
	}
	return nil, fmt.Errorf("gitea: tree page limit exceeded")
}

// maxCommitDiff bounds one read of a commit from Gitea.
const maxCommitDiff = 32 << 20

// blobID is the git object id of a file's content.
func blobID(content string) string {
	return fmt.Sprintf("%x", gitsha1.Sum([]byte(fmt.Sprintf("blob %d\x00%s", len(content), content))))
}
