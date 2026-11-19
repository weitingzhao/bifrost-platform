package gitread

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// Origin: 2026-11-16 21:59 CST. The redesigned Work > CI page asks for
// /units/config and /ci/domains together; each walks every Go file of main's
// head, and on a platform-api that had just started both missed the blob cache
// for all 1,424 files, so Gitea served 2,795 blob reads in two minutes (counted
// by the communication line). Askers of one blob at the same time make one
// read; a read that failed is not kept and the next asker reads again.
func TestABlobAskedForTogetherIsReadOnce(t *testing.T) {
	t.Parallel()
	const content = "package a\n"
	id := blobID(content)
	var reads atomic.Int32
	release := make(chan struct{})
	fail := atomic.Bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/git/blobs/"+id) {
			http.Error(w, "no", http.StatusNotFound)
			return
		}
		reads.Add(1)
		<-release
		if fail.Load() {
			http.Error(w, "gitea is restarting", http.StatusBadGateway)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"sha": id, "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(content))})
	}))
	defer srv.Close()
	g := &Trees{Access: func(context.Context) (Access, error) {
		return Access{Base: srv.URL, Org: "bifrost"}, nil
	}, Client: srv.Client()}
	ask := func(n int) (ok, failed int32) {
		var wg sync.WaitGroup
		var good, bad atomic.Int32
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if b, err := g.Blob(context.Background(), "r", id); err == nil && string(b) == content {
					good.Add(1)
				} else {
					bad.Add(1)
				}
			}()
		}
		// Let every asker arrive before the one read answers.
		for {
			g.mu.Lock()
			waiting := g.flight[id] != nil
			g.mu.Unlock()
			if waiting && reads.Load() >= 1 {
				break
			}
		}
		release <- struct{}{}
		wg.Wait()
		return good.Load(), bad.Load()
	}
	// A failed read: every asker of that moment gets the failure, nothing is kept.
	fail.Store(true)
	if _, bad := ask(1); bad != 1 || reads.Load() != 1 {
		t.Fatalf("a failed read: %d failures, %d reads", bad, reads.Load())
	}
	fail.Store(false)
	if good, bad := ask(1); good != 1 || bad != 0 || reads.Load() != 2 {
		t.Fatalf("after the failure: %d good, %d bad, %d reads", good, bad, reads.Load())
	}
	// Kept: twenty more askers read nothing.
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = g.Blob(context.Background(), "r", id) }()
	}
	wg.Wait()
	if reads.Load() != 2 {
		t.Fatalf("a kept blob was read again: %d reads", reads.Load())
	}
}

// The askers that arrive while one read is under way wait for it: on a tree
// of its own, twenty at once make one read. Its control is the count itself:
// without the wait each of the twenty would read.
func TestTwentyAskersOfAColdBlobMakeOneRead(t *testing.T) {
	t.Parallel()
	const content = "package b\n"
	id := blobID(content)
	var reads atomic.Int32
	arrived := make(chan struct{}, 1)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reads.Add(1)
		select {
		case arrived <- struct{}{}:
		default:
		}
		<-release
		_ = json.NewEncoder(w).Encode(map[string]any{"sha": id, "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(content))})
	}))
	defer srv.Close()
	g := &Trees{Access: func(context.Context) (Access, error) {
		return Access{Base: srv.URL, Org: "bifrost"}, nil
	}, Client: srv.Client()}
	var wg sync.WaitGroup
	var good atomic.Int32
	first := make(chan struct{})
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i > 0 {
				<-first // the other nineteen start once the first read is at Gitea
			}
			if b, err := g.Blob(context.Background(), "r", id); err == nil && string(b) == content {
				good.Add(1)
			}
		}(i)
	}
	<-arrived
	close(first)
	// The nineteen find the read under way (or, if they are late, the kept blob).
	close(release)
	wg.Wait()
	if good.Load() != 20 || reads.Load() != 1 {
		t.Fatalf("%d good answers from %d reads, want 20 from 1", good.Load(), reads.Load())
	}
}
