package unitwatch

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/regressions"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
)

const (
	good = "version: 1\nmodules:\n  - name: svc\n    kind: go\n    root: svc\n    tasks: {test: {per: package}}\n    inputs:\n      svc/a: [\"svc/a\"]\n"
	// What a25c27aa did: inputs for a package with no tests.
	bad = good + "      svc/b: [\"svc/b\"]\n"
)

func sha(n int) string { return fmt.Sprintf("%040x", n) }

// history is a main: commit n's parent is n-1, commit 1 has none.
type history struct {
	manifest map[int]string // by commit; the rest are good
	head     int
	down     bool         // Gitea answers nothing
	blobDown atomic.Bool  // the tree is listed, its files are not served
	reads    atomic.Int32 // trees read
}

func (h *history) number(commit string) int {
	var n int
	_, _ = fmt.Sscanf(commit, "%x", &n)
	return n
}

func (h *history) watch(db eventlog.DB) Watch {
	return Watch{
		DB: db,
		Head: func(context.Context, string) (string, error) {
			if h.down {
				return "", errors.New("gitea: connection refused")
			}
			return sha(h.head), nil
		},
		FirstParent: func(_ context.Context, _, commit string) (string, error) {
			if n := h.number(commit); n > 1 {
				return sha(n - 1), nil
			}
			return "", nil
		},
		Tree: func(_ context.Context, _, commit string) (*unitplan.Tree, error) {
			h.reads.Add(1)
			manifest, ok := h.manifest[h.number(commit)]
			if !ok {
				manifest = good
			}
			files := map[string]string{"bifrost.units.yaml": manifest, "svc/go.mod": "module example.com/m\n\ngo 1.25\n",
				"svc/a/a.go": "package a\n", "svc/a/a_test.go": "package a\n", "svc/b/b.go": "package b\n"}
			entries := []unitplan.Entry{{Path: "svc", Dir: true, ID: "d-svc"}, {Path: "svc/a", Dir: true, ID: "d-a"}, {Path: "svc/b", Dir: true, ID: "d-b"}}
			for p := range files {
				entries = append(entries, unitplan.Entry{Path: p, ID: p})
			}
			return unitplan.NewTree("root", entries, func(id string) ([]byte, error) {
				if h.blobDown.Load() && strings.HasSuffix(id, ".go") {
					return nil, errors.New("gitea: HTTP 502")
				}
				return []byte(files[id]), nil
			})
		},
	}
}

func ledger(t *testing.T) eventlog.DB {
	t.Helper()
	db := agentrttest.Migrated(t)
	if _, err := actor.Seed(context.Background(), db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	return db.Pool(t, db.Runtime)
}

func pass(t *testing.T, w Watch) {
	t.Helper()
	if err := w.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func all(t *testing.T, db eventlog.DB, open bool) []regressions.Regression {
	t.Helper()
	list, err := regressions.List(context.Background(), db, watchedRepo, open)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func knownEvents(t *testing.T, db eventlog.DB) int {
	t.Helper()
	var n int
	err := eventlog.InTx(context.Background(), db, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM agentrt.event WHERE event_type = $1`, "main.regression.known").Scan(&n)
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// Origin: 2026-11-18, a25c27aa: main's declaration stopped reading and the
// ledger said nothing. Several commits landed in one push, so the regression
// must name the first bad one, not the head, or the wake reaches the wrong seat.
func TestDeclarationThatStopsReadingIsKnownAtItsFirstBadCommit(t *testing.T) {
	db := ledger(t)
	h := &history{head: 7, manifest: map[int]string{5: bad, 6: bad, 7: bad}}
	pass(t, h.watch(db))
	open := all(t, db, true)
	if len(open) != 1 || open[0].Introduced != sha(5) || open[0].By != "controller" {
		t.Fatalf("open = %+v, want one introduced by commit 5, by controller", open)
	}
	if !strings.HasPrefix(open[0].Impact, impactPrefix) || !strings.Contains(open[0].Impact, "svc/b") {
		t.Fatalf("impact = %q", open[0].Impact)
	}
}

// Origin: 2026-11-18, the communication line: events are delivered again and
// one push moves main by several commits, so a pass must add nothing the
// second time, even with main further on and still broken.
func TestRedeliveryRecordsNothingNew(t *testing.T) {
	db := ledger(t)
	h := &history{head: 3, manifest: map[int]string{3: bad, 4: bad}}
	w := h.watch(db)
	pass(t, w)
	pass(t, w)
	h.head = 4
	pass(t, w)
	if n := knownEvents(t, db); n != 1 {
		t.Fatalf("%d known events, want 1", n)
	}
}

// Origin: 2026-11-18, fde7d1df repaired the declaration and the record of the
// break must close by itself, at the commit that repaired it.
func TestReadingAgainRecordsTheFix(t *testing.T) {
	db := ledger(t)
	h := &history{head: 3, manifest: map[int]string{3: bad}}
	w := h.watch(db)
	pass(t, w)
	h.head = 4
	pass(t, w)
	got := all(t, db, false)
	if len(got) != 1 || got[0].Fixed != sha(4) || len(all(t, db, true)) != 0 {
		t.Fatalf("regressions = %+v, want one fixed by commit 4", got)
	}
}

// Origin: 2026-11-18, the coordinator's brief: seats record regressions of
// their own on the same repository; the watch closes only what it recorded.
func TestOtherRegressionsAreLeftOpen(t *testing.T) {
	db := ledger(t)
	seat := sha(900)
	if _, err := regressions.Known(context.Background(), db, writer, watchedRepo, seat, "the orders page is blank"); err != nil {
		t.Fatal(err)
	}
	h := &history{head: 3, manifest: map[int]string{3: bad}}
	w := h.watch(db)
	pass(t, w)
	if n := len(all(t, db, true)); n != 2 {
		t.Fatalf("%d open, want the seat's and the watch's", n)
	}
	h.head = 4
	pass(t, w)
	open := all(t, db, true)
	if len(open) != 1 || open[0].Introduced != seat {
		t.Fatalf("open = %+v, want only the seat's", open)
	}
}

// Origin: 2026-11-18, the coordinator's brief: an unreachable Gitea says
// nothing about the declaration. Both ways it shows: no head at all, and a
// tree listed whose files are not served, which go/build reports as a broken
// package; that one would otherwise be recorded as a regression of main.
// The recovery half: the first pass after it concludes correctly.
func TestReadFailureRecordsNothingAndThePassAfterItConcludes(t *testing.T) {
	db := ledger(t)
	h := &history{head: 3, manifest: map[int]string{3: bad}}
	w := h.watch(db)
	h.down = true
	if err := w.Pass(context.Background()); err == nil {
		t.Fatal("no head: the pass concluded")
	}
	h.down = false
	h.blobDown.Store(true)
	if err := w.Pass(context.Background()); err == nil {
		t.Fatal("files not served: the pass concluded")
	}
	if n := knownEvents(t, db); n != 0 {
		t.Fatalf("%d known events from a read failure", n)
	}
	h.blobDown.Store(false)
	pass(t, w)
	if open := all(t, db, true); len(open) != 1 || open[0].Introduced != sha(3) {
		t.Fatalf("after recovery open = %+v", open)
	}
}

// Origin: 2026-11-18, the same read failure on the other side: a good main
// whose files are not served must not close what is open.
func TestReadFailureClosesNothing(t *testing.T) {
	db := ledger(t)
	h := &history{head: 3, manifest: map[int]string{3: bad}}
	w := h.watch(db)
	pass(t, w)
	h.head = 4
	h.blobDown.Store(true)
	if err := w.Pass(context.Background()); err == nil {
		t.Fatal("files not served: the pass concluded")
	}
	if n := len(all(t, db, true)); n != 1 {
		t.Fatalf("%d open, want 1", n)
	}
}

// Origin: 2026-11-18, the incident ran 34 commits; a longer run must still be
// recorded, at the oldest commit read, and say that it may begin earlier.
func TestWalkStopsAtItsLimitAndSaysSo(t *testing.T) {
	db := ledger(t)
	h := &history{head: walkLimit + 50, manifest: map[int]string{}}
	for n := 1; n <= h.head; n++ {
		h.manifest[n] = bad
	}
	pass(t, h.watch(db))
	open := all(t, db, true)
	if len(open) != 1 || open[0].Introduced != sha(h.head-walkLimit+1) {
		t.Fatalf("open = %+v", open)
	}
	if !strings.Contains(open[0].Impact, "may begin earlier") || len([]rune(open[0].Impact)) > regressions.MaxImpact {
		t.Fatalf("impact = %q", open[0].Impact)
	}
	if got := int(h.reads.Load()); got != walkLimit {
		t.Fatalf("%d trees read, want %d", got, walkLimit)
	}
}

// Origin: 2026-11-18: main can stay quiet for hours after a push; a pass that
// failed on Gitea must not wait for the next push to try again.
func TestRunRetriesWithoutAnotherKick(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	kicks := make(chan struct{}, 1)
	kicks <- struct{}{}
	done := make(chan struct{})
	var passes atomic.Int32
	go Run(ctx, kicks, time.Millisecond, func(context.Context) error {
		if passes.Add(1) == 1 {
			return errors.New("gitea: connection refused")
		}
		close(done)
		return nil
	})
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("the failed pass was not run again")
	}
}
