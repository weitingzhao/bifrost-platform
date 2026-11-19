package gitread

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Origin: 2026-11-16 23:05 CST on STG. A delivery had just restarted
// platform-api; Work > CI's "who depends on whom" still said "Reading the
// domains of main" after two minutes, because the 1,424 Go files of main were
// read from Gitea one after another. A tree whose Go files are mostly not in
// the cache has them read several at a time, each once; a tree that misses
// only a few (main moved by a commit) is read as before, a file when it is
// asked for.
func TestAColdTreeReadsItsGoFilesSideBySide(t *testing.T) {
	t.Parallel()
	commit := func(n int) string { return strings.Repeat(fmt.Sprint(n), 40) }
	blobs := map[string]string{}
	file := func(i int) (TreeEntry, string) {
		content := fmt.Sprintf("package p%d\n", i)
		return TreeEntry{Path: fmt.Sprintf("f%d.go", i), Mode: "100644", Type: "blob", SHA: blobID(content)}, content
	}
	trees := map[string][]TreeEntry{}
	add := func(c string, from, to int, extra ...TreeEntry) {
		for i := from; i < to; i++ {
			e, content := file(i)
			blobs[e.SHA] = content
			trees[c] = append(trees[c], e)
		}
		trees[c] = append(trees[c], extra...)
	}
	readme := "# not Go\n"
	blobs[blobID(readme)] = readme
	add(commit(1), 0, 150, TreeEntry{Path: "README.md", Mode: "100644", Type: "blob", SHA: blobID(readme)})
	add(commit(2), 0, 190) // main moved: forty files the cache does not hold
	var reads, now, most atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "/api/v1/repos/bifrost/r/git/"
		switch {
		case strings.HasPrefix(r.URL.Path, prefix+"trees/"):
			entries := trees[strings.TrimPrefix(r.URL.Path, prefix+"trees/")]
			_ = json.NewEncoder(w).Encode(map[string]any{"sha": strings.TrimPrefix(r.URL.Path, prefix+"trees/"), "page": 1, "total_count": len(entries), "truncated": false, "tree": entries})
		case strings.HasPrefix(r.URL.Path, prefix+"blobs/"):
			id := strings.TrimPrefix(r.URL.Path, prefix+"blobs/")
			reads.Add(1)
			if n := now.Add(1); n > most.Load() {
				most.Store(n)
			}
			time.Sleep(5 * time.Millisecond)
			now.Add(-1)
			_ = json.NewEncoder(w).Encode(map[string]any{"sha": id, "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(blobs[id]))})
		default:
			http.Error(w, "no", http.StatusNotFound)
		}
	}))
	defer srv.Close()
	g := &Trees{Access: func(context.Context) (Access, error) {
		return Access{Base: srv.URL, Org: "bifrost"}, nil
	}, Client: srv.Client()}
	ctx := context.Background()
	// A tree as its readers take it: listed, prefetched, then read by path.
	read := func(c string) (func(string) ([]byte, error), error) {
		listing, err := g.Listing(ctx, "r", c)
		if err != nil {
			return nil, err
		}
		g.Prefetch(ctx, "r", listing)
		return func(p string) ([]byte, error) { return g.Blob(ctx, "r", listing[p].SHA) }, nil
	}

	tree, err := read(commit(1))
	if err != nil {
		t.Fatal(err)
	}
	// Every Go file was read, once, more than one at a time; what is not Go was left.
	if reads.Load() != 150 || most.Load() < 2 || most.Load() > prefetchers {
		t.Fatalf("a cold tree: %d reads, %d at once at most (want 150, 2..%d)", reads.Load(), most.Load(), prefetchers)
	}
	if b, err := tree("f7.go"); err != nil || string(b) != "package p7\n" || reads.Load() != 150 {
		t.Fatalf("a prefetched file was read again: %q %v, %d reads", b, err, reads.Load())
	}
	if b, err := tree("README.md"); err != nil || string(b) != readme || reads.Load() != 151 {
		t.Fatalf("a file that is not Go: %q %v, %d reads", b, err, reads.Load())
	}
	// The control: a tree that misses forty files reads none of them until asked.
	moved, err := read(commit(2))
	if err != nil || reads.Load() != 151 {
		t.Fatalf("main moved by forty files: %d reads before any was asked for (%v)", reads.Load(), err)
	}
	if b, err := moved("f180.go"); err != nil || string(b) != "package p180\n" || reads.Load() != 152 {
		t.Fatalf("a new file: %q %v, %d reads", b, err, reads.Load())
	}
}
