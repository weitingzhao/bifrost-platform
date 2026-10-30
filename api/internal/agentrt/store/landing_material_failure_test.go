package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// Review 738 R-02: the HTTP waiter has left before Git fails. A later
// request must observe that failure, without restarting the same work.
func TestMaterialCacheSlowFailureIsObservable(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"Git error", "deadline", "panic"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				// Also drain a wrongly restarted flight when the baseline fails.
				t.Cleanup(func() { time.Sleep(materialWorkLimit + time.Second); synctest.Wait() })
				cache := &GrantMaterialCache{}
				var calls atomic.Int32
				var recovered atomic.Bool
				want := "slow Git failure"
				delay := 1100 * time.Millisecond
				if mode == "deadline" {
					want, delay = context.DeadlineExceeded.Error(), materialWorkLimit
				} else if mode == "panic" {
					want = "computation interrupted"
				}
				work := func(ctx context.Context) (grantExt, error) {
					calls.Add(1)
					if recovered.Load() {
						return grantExt{patch: GrantPatch{Shown: "recovered"}}, nil
					}
					if mode == "deadline" {
						<-ctx.Done()
						// A late nominal success still must honor the work deadline.
						return grantExt{patch: GrantPatch{Shown: "late evidence"}}, nil
					}
					time.Sleep(delay)
					if mode == "panic" {
						panic("test material reader")
					}
					return grantExt{patch: GrantPatch{Shown: "partial evidence"}}, errors.New(want)
				}
				if _, err := cache.read(t.Context(), materialKey{}, work); !errors.Is(err, ErrMaterialPending) {
					t.Fatalf("first read must be pending: %v", err)
				}
				time.Sleep(delay - materialWaitLimit + 100*time.Millisecond)
				synctest.Wait()
				for range 3 {
					x, err := cache.read(t.Context(), materialKey{}, work)
					if err == nil || !strings.Contains(err.Error(), want) || x.patch.Shown != "" {
						t.Fatalf("completed failure must be observable without evidence: %+v, %v (calls=%d)", x, err, calls.Load())
					}
				}
				if calls.Load() != 1 || len(cache.flights) != 0 {
					t.Fatalf("failure retried during backoff or leaked a worker: calls=%d flights=%d", calls.Load(), len(cache.flights))
				}
				time.Sleep(6 * time.Second)
				recovered.Store(true)
				x, err := cache.read(t.Context(), materialKey{}, work)
				if err != nil || x.patch.Shown != "recovered" || calls.Load() != 2 {
					t.Fatalf("must recover after short backoff: %+v %v calls=%d", x, err, calls.Load())
				}
			})
		})
	}
}

func TestMaterialCacheFailureCompletionsStayBoundedAndKeyed(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		cache := &GrantMaterialCache{}
		work := func(context.Context) (grantExt, error) {
			return grantExt{patch: GrantPatch{Shown: "partial evidence"}}, errors.New(strings.Repeat("failure", 1000))
		}
		for i := range materialMaxEntries + 1 {
			if x, err := cache.read(t.Context(), materialKey{R: fmt.Sprint(i)}, work); err == nil || x.patch.Shown != "" {
				t.Fatalf("failed work returned evidence: %+v %v", x, err)
			}
		}
		if len(cache.entries) != materialMaxEntries || cache.bytes > materialMaxEntries*4096 {
			t.Fatalf("failure completions unbounded: entries=%d bytes=%d", len(cache.entries), cache.bytes)
		}
		x, err := cache.read(t.Context(), materialKey{R: "new candidate"}, func(context.Context) (grantExt, error) {
			return grantExt{patch: GrantPatch{Shown: "new evidence"}}, nil
		})
		if err != nil || x.patch.Shown != "new evidence" {
			t.Fatalf("old failure blocked a new key: %+v %v", x, err)
		}
		time.Sleep(6 * time.Second)
		cache.mu.Lock()
		cache.expire(time.Now())
		cache.mu.Unlock()
		if len(cache.entries) != 1 || cache.bytes != len("new evidence") {
			t.Fatalf("failure expiry retained payload or removed successful evidence: entries=%d bytes=%d", len(cache.entries), cache.bytes)
		}
	})
}
