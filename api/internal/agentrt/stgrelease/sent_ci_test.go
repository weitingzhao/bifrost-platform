package stgrelease

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	clocktesting "k8s.io/utils/clock/testing"
)

// sentCIReader fakes landingci.Client.ReadRelease as B5's check after a
// send sees it: every run of P, however late it was created.
type sentCIReader struct {
	obs   store.StgCIObservation
	err   error
	reads int
	ps    []string
	// main's head as B5 reads it (Sources.Head), and the notices logged.
	head      string
	headErr   error
	headReads int
	notices   []string
}

func (r *sentCIReader) readHead(context.Context) (string, error) {
	r.headReads++
	return r.head, r.headErr
}

func (r *sentCIReader) read(_ context.Context, p string) (store.StgCIObservation, error) {
	r.reads++
	r.ps = append(r.ps, p)
	return r.obs, r.err
}

// landingRun names the landing CI a send rested on, for reads that return it.
const landingRun = "landing-ci-0123456789abcdef01234567-1"

// sentFixture is controlFixture's batch: try 1 started (sent) at start, on a
// CI accepted with created_at = start; main is at P (sha a).
func sentFixture(t *testing.T) (*Controller, *memoryLedger, *sentCIReader, *clocktesting.FakeClock, time.Time) {
	t.Helper()
	c, l, _, _ := controlFixture(t, "Running")
	clock := c.Clock.(*clocktesting.FakeClock)
	start := clock.Now().UTC()
	r := &sentCIReader{head: sha("a")}
	c.Sources.SentCI, c.Sources.Head = r.read, r.readHead
	c.notice = func(msg string, args ...any) {
		r.notices = append(r.notices, fmt.Sprint(append([]any{msg}, args...)...))
	}
	return c, l, r, clock, start
}

func sentObs(status, reason, uid string, created, read time.Time, undecided int) store.StgCIObservation {
	return store.StgCIObservation{Status: status, Reason: reason, UID: uid, Run: strings.TrimSuffix(uid, "-uid") + "-run", CreatedAt: created, ReadAt: read, Undecided: undecided}
}

// sendKey is the watch key of the fixture's single send.
func sendKey(t *testing.T, l *memoryLedger, now time.Time) string {
	t.Helper()
	sends := sentTries(l.state(t), now)
	if len(sends) != 1 {
		t.Fatal("sends", sends)
	}
	return sends[0].key
}

func openBlocks(s store.StgState) []store.StgBlock {
	var out []store.StgBlock
	for _, b := range s.Blocks {
		if b.IsOpen() {
			out = append(out, b)
		}
	}
	return out
}

func TestSentCIRedAfterSendOpensTheGateBlock(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ status, reason, why string }{{"Failed", "", "ci_red"}, {"Unknown", "ci_results", "ci_results"}, {"Unknown", "ci_tie", "ci_results"}} {
		t.Run(tc.why+"/"+tc.reason, func(t *testing.T) {
			t.Parallel()
			c, l, r, clock, start := sentFixture(t)
			clock.Step(17 * time.Minute)
			r.obs = sentObs(tc.status, tc.reason, "main-ci-uid", start.Add(time.Second), clock.Now(), 0)
			commits := l.commits
			pass(t, c) // the pass ends on the block it wrote
			if r.reads != 1 || r.ps[0] != sha("a") {
				t.Fatal("read", r.reads, r.ps)
			}
			if l.commits != commits+1 {
				t.Fatal("the block was not the pass's only write", l.commits-commits)
			}
			s := l.state(t)
			blocks := openBlocks(s)
			if len(blocks) != 1 {
				t.Fatal("blocks", len(blocks))
			}
			opened := blocks[0].Opened.Record
			if opened.String("why") != tc.why || opened.String("P") != sha("a") || object(object(opened, "detail"), "ci").String("uid") != "main-ci-uid" || len(object(opened, "detail")["members"].([]any)) != 1 {
				t.Fatal("block", opened)
			}
			// Marked as found after this send, for the Owner's card.
			sent := object(object(opened, "detail"), "sent")
			run := s.Batches[leaseBatch].Tries[1].Started.Record.String("run")
			if sent.String("batch") != leaseBatch || sent.Int("try") != 1 || sent.Int("permit_seq") != s.Batches[leaseBatch].Tries[1].Permit.Seq || sent.String("run") != run || run == "" || sent.String("ci_run") != "main-ci-run" {
				t.Fatal("post-send marker", sent)
			}
			// The in-flight try is untouched: still occupied, nothing settled.
			if b := s.Batches[leaseBatch]; !b.Tries[1].Occupied() || b.Phase == "CLOSED" {
				t.Fatal("block disturbed the running try", b.Phase)
			}
			// With main still at P later reads open no second interval (the run
			// is already named) and settle nothing; once main moves on, settled.
			key := sendKey(t, l, clock.Now())
			for i := 0; i < 3; i++ {
				clock.Step(sentCIEvery)
				if wrote, err := c.watchSentCI(context.Background(), l.state(t)); wrote || err != nil {
					t.Fatal("after the block", wrote, err)
				}
			}
			if r.reads != 4 || len(l.state(t).BlockHistory) != 1 || c.sent[key].settled {
				t.Fatal("watch after the block", r.reads, len(l.state(t).BlockHistory))
			}
			r.head = sha("d")
			for i := 0; i < 2; i++ {
				clock.Step(sentCIEvery)
				_, _ = c.watchSentCI(context.Background(), l.state(t))
			}
			if r.reads != 5 || !c.sent[key].settled || len(r.notices) != 0 || len(openBlocks(l.state(t))) != 1 {
				t.Fatal("main moved on after the block did not settle quietly", r.reads, r.notices)
			}
			if tc.why != "ci_red" {
				return
			}
			// The existing ci_red rule resolves it: only a newer green of P.
			now := clock.Now()
			proof := func(uid string, created time.Time) store.StgRecord {
				ci := map[string]any{"name": uid, "uid": uid, "created_at": created.Format(time.RFC3339Nano), "read_at": now.Format(time.RFC3339Nano), "platform_commit": sha("a"), "ui_commit": sha("b"), "infra_commit": sha("c"), "pipeline_ref": "bifrost-ci-platform", "repo_url": landing.LandingRepos["bifrost-platform"].Remote}
				return store.StgRecord{"v": "1", "scope": "block", "kind": "ci_observed", "block": opened.String("block"), "interval_seq": blocks[0].Opened.Seq, "P": sha("a"), "ci": ci, "status": "Succeeded"}
			}
			if !store.StgCanObserveCI(s, proof("rerun-uid", start.Add(30*time.Minute)), now) {
				t.Fatal("a newer green cannot resolve the post-send block")
			}
			if store.StgCanObserveCI(s, proof("main-ci-uid", start.Add(time.Second)), now) || store.StgCanObserveCI(s, proof("older-uid", start.Add(-time.Minute)), now) {
				t.Fatal("the rejected run or an older green resolved the post-send block")
			}
		})
	}
}

// While main is at P no green settles a send: not the landing green it
// rested on, not main's green. Once main has moved on with nothing of P
// undecided, the send is settled and not read again.
func TestSentCIWaitsWithoutBlocking(t *testing.T) {
	t.Parallel()
	c, l, r, clock, start := sentFixture(t)
	s := l.state(t)
	key := sendKey(t, l, clock.Now())
	// A run going, over the landing green: no block, not settled, main not read.
	r.obs = sentObs("Succeeded", "", "landing-uid", start, clock.Now(), 1)
	r.obs.Run = landingRun
	if wrote, err := c.watchSentCI(context.Background(), s); wrote || err != nil || r.reads != 1 || r.headReads != 0 || c.sent[key].settled {
		t.Fatal(wrote, err, r.reads, r.headReads)
	}
	if wrote, _ := c.watchSentCI(context.Background(), s); wrote || r.reads != 1 {
		t.Fatal("read again within sentCIEvery", r.reads)
	}
	// Greens with nothing undecided and main at P: never settled.
	for i := 0; i < 15; i++ {
		clock.Step(sentCIEvery)
		r.obs = sentObs("Succeeded", "", "main-ci-uid", start.Add(time.Duration(i)*time.Minute), clock.Now(), 0)
		if wrote, err := c.watchSentCI(context.Background(), s); wrote || err != nil || c.sent[key].settled {
			t.Fatal("settled on a green while main is at P", i, wrote, err)
		}
	}
	if r.headReads != 15 {
		t.Fatal("main not read on each decided read", r.headReads)
	}
	// main moves on: settled, no further reads.
	clock.Step(sentCIEvery)
	r.head = sha("d")
	reads := r.reads
	if wrote, _ := c.watchSentCI(context.Background(), s); wrote || r.reads != reads+1 || !c.sent[key].settled {
		t.Fatal("main moved on with nothing undecided did not settle", r.reads)
	}
	clock.Step(time.Hour)
	if _, _ = c.watchSentCI(context.Background(), s); r.reads != reads+1 {
		t.Fatal("settled send read again")
	}
	if len(openBlocks(l.state(t))) != 0 || len(r.notices) != 0 {
		t.Fatal("green opened a block or said something", r.notices)
	}
}

// R5:5.7-2 pre-review of round 2: a land/prod branch push of P runs the same
// CI as main's push, under the same labels, and can be created after the
// landing CI. Its green must not settle the send: main's push CI, created
// late, ends red with main still at P, and is blocked. Both cases: the
// branch run still going at the send (B5 rested on the landing CI), and the
// branch run already green at the send (B5 rested on it).
func TestSentCIBranchPushGreenDoesNotSettle(t *testing.T) {
	t.Parallel()
	for name, branchCreated := range map[string]time.Duration{"created after the landing CI": 30 * time.Second, "the run the send rested on": 0} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c, l, r, clock, start := sentFixture(t)
			clock.Step(3 * time.Minute)
			r.obs = sentObs("Succeeded", "", "branch-uid", start.Add(branchCreated), clock.Now(), 0)
			r.obs.Run = "ci-platform-b4nch"
			pass(t, c)
			clock.Step(30 * time.Minute)
			r.obs = sentObs("Failed", "", "main-ci-uid", start.Add(12*time.Minute), clock.Now(), 0)
			pass(t, c)
			blocks := openBlocks(l.state(t))
			if r.reads != 2 || len(blocks) != 1 || object(object(blocks[0].Opened.Record, "detail"), "ci").String("uid") != "main-ci-uid" {
				t.Fatal("main's late red after a branch-push green was not blocked", r.reads, len(blocks))
			}
		})
	}
}

// R5:5.7-2 review round 1: main stays at P, its push CI is created after
// the ten minutes the first version admitted, and then fails. It is blocked.
func TestSentCIMainRunCreatedLateThenRedIsBlocked(t *testing.T) {
	t.Parallel()
	c, l, r, clock, start := sentFixture(t)
	landingGreen := func(undecided int) {
		r.obs = sentObs("Succeeded", "", "landing-uid", start, clock.Now(), undecided)
		r.obs.Run = landingRun
	}
	clock.Step(5 * time.Minute)
	landingGreen(0) // the webhook has not arrived
	pass(t, c)
	clock.Step(5*time.Minute + 30*time.Second)
	landingGreen(0) // 10.5 minutes: still no run of main, the landing green newest
	pass(t, c)
	clock.Step(sentCIEvery)
	landingGreen(1) // main's run, created 11 minutes after the send, is running
	pass(t, c)
	clock.Step(18 * time.Minute)
	r.obs = sentObs("Failed", "", "main-ci-uid", start.Add(11*time.Minute), clock.Now(), 0)
	pass(t, c)
	blocks := openBlocks(l.state(t))
	if r.reads != 4 || r.headReads != 3 || len(blocks) != 1 || blocks[0].Opened.Record.String("why") != "ci_red" ||
		object(object(blocks[0].Opened.Record, "detail"), "ci").String("uid") != "main-ci-uid" {
		t.Fatal("main's late red was not blocked", r.reads, r.headReads, len(blocks))
	}
}

// What settles a send with no refusal: main moved on from P and nothing of P
// undecided. A failed or missing main read settles nothing and is an error.
func TestSentCISettlesOnlyOnceMainHasMovedOn(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		status    string
		undecided int
		head      string
		headErr   error
		noReader  bool
		settled   bool
		err       bool
		headReads int
	}{
		"green, main at P":             {status: "Succeeded", head: sha("a"), headReads: 1},
		"missing, main at P":           {status: "Missing", head: sha("a"), headReads: 1},
		"green, main moved":            {status: "Succeeded", head: sha("d"), settled: true, headReads: 1},
		"missing, main moved":          {status: "Missing", head: sha("d"), settled: true, headReads: 1},
		"going, main moved":            {status: "Succeeded", undecided: 1, head: sha("d")},
		"main unreadable":              {status: "Succeeded", headErr: errors.New("gitea unreachable"), err: true, headReads: 1},
		"main read empty":              {status: "Succeeded", head: "", err: true, headReads: 1},
		"no main reader":               {status: "Succeeded", noReader: true, err: true},
		"older unusable, main at P":    {status: "Unknown", head: sha("a"), headReads: 1},
		"older unusable, main moved":   {status: "Unknown", head: sha("d"), settled: true, headReads: 1},
		"older red, main moved":        {status: "Failed", head: sha("d"), settled: true, headReads: 1},
		"older red, main at P (kept)":  {status: "Failed", head: sha("a"), headReads: 1},
		"going, main at P (not asked)": {status: "Succeeded", undecided: 2, head: sha("a")},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c, l, r, clock, start := sentFixture(t)
			clock.Step(20 * time.Minute)
			// Not newer than the CI the send rested on: never a refusal.
			r.obs = sentObs(tc.status, "", "u", start.Add(-time.Minute), clock.Now(), tc.undecided)
			if tc.status == "Unknown" {
				r.obs.Reason = "ci_identity"
			}
			r.head, r.headErr = tc.head, tc.headErr
			if tc.noReader {
				c.Sources.Head = nil
			}
			key := sendKey(t, l, clock.Now())
			wrote, err := c.watchSentCI(context.Background(), l.state(t))
			if wrote || (err != nil) != tc.err || c.sent[key].settled != tc.settled || r.headReads != tc.headReads || len(r.notices) != 0 {
				t.Fatal(wrote, err, c.sent[key].settled, r.headReads, r.notices)
			}
		})
	}
}

// After an hour a send still waiting is read every sentCIEverySlow.
func TestSentCIReadsLessOftenAfterAnHour(t *testing.T) {
	t.Parallel()
	c, l, r, clock, start := sentFixture(t)
	r.obs = sentObs("Succeeded", "", "landing-uid", start, clock.Now(), 0)
	r.obs.Run = landingRun
	clock.SetTime(start.Add(30 * time.Minute))
	if _, err := c.watchSentCI(context.Background(), l.state(t)); err != nil || r.reads != 1 {
		t.Fatal("control", err, r.reads)
	}
	clock.Step(sentCIEvery)
	if _, _ = c.watchSentCI(context.Background(), l.state(t)); r.reads != 2 {
		t.Fatal("not read every sentCIEvery within the first hour", r.reads)
	}
	clock.SetTime(start.Add(sentCISlowAfter + 10*time.Minute))
	if _, _ = c.watchSentCI(context.Background(), l.state(t)); r.reads != 3 {
		t.Fatal("control after the first hour", r.reads)
	}
	clock.Step(sentCIEvery)
	if _, _ = c.watchSentCI(context.Background(), l.state(t)); r.reads != 3 {
		t.Fatal("read every sentCIEvery after the first hour", r.reads)
	}
	clock.Step(sentCIEverySlow - sentCIEvery)
	if _, _ = c.watchSentCI(context.Background(), l.state(t)); r.reads != 4 {
		t.Fatal("not read after sentCIEverySlow", r.reads)
	}
}

func TestSentCIRefusalOnlyForNewerVerdicts(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 1, 8, 37, 18, 0, time.UTC)
	for name, tc := range map[string]struct {
		obs  store.StgCIObservation
		want string
	}{
		"newer red":            {sentObs("Failed", "", "u", at.Add(time.Second), at, 0), "ci_red"},
		"newer unusable":       {sentObs("Unknown", "ci_identity", "u", at.Add(time.Second), at, 0), "ci_results"},
		"accepted itself":      {sentObs("Failed", "", "u", at, at, 0), ""},
		"older red":            {sentObs("Failed", "", "u", at.Add(-time.Second), at, 0), ""},
		"newer green":          {sentObs("Succeeded", "", "u", at.Add(time.Second), at, 0), ""},
		"still running":        {sentObs("Running", "", "u", at.Add(time.Second), at, 0), ""},
		"no verdict no reason": {sentObs("Unknown", "", "u", at.Add(time.Second), at, 0), ""},
		"no identity":          {sentObs("Unknown", "ci_identity", "", time.Time{}, at, 0), ""},
		"missing":              {sentObs("Missing", "", "", time.Time{}, at, 0), ""},
	} {
		if got := sentCIRefusal(tc.obs, at); got != tc.want {
			t.Fatalf("%s: got %q want %q", name, got, tc.want)
		}
	}
}

func TestSentCIBoundsAndFailures(t *testing.T) {
	t.Parallel()
	t.Run("nil reader", func(t *testing.T) {
		t.Parallel()
		c, l, _, _, _ := sentFixture(t)
		c.Sources.SentCI = nil
		if wrote, err := c.watchSentCI(context.Background(), l.state(t)); wrote || err != nil {
			t.Fatal(wrote, err)
		}
	})
	t.Run("horizon", func(t *testing.T) {
		t.Parallel()
		c, l, r, clock, _ := sentFixture(t)
		clock.Step(sentCIHorizon + time.Second)
		if wrote, err := c.watchSentCI(context.Background(), l.state(t)); wrote || err != nil || r.reads != 0 {
			t.Fatal("read a send beyond the horizon", r.reads)
		}
	})
	t.Run("read error", func(t *testing.T) {
		t.Parallel()
		c, l, r, clock, _ := sentFixture(t)
		r.err = errors.New("list failed")
		commits := l.commits
		if err := c.Pass(context.Background()); err == nil || !errors.Is(err, r.err) {
			t.Fatal("read error hidden", err)
		}
		if l.commits != commits || len(openBlocks(l.state(t))) != 0 {
			t.Fatal("a failed read wrote")
		}
		// The pass went on to observe the running try (no I/O gate on it).
		clock.Step(sentCIEvery)
		r.err = nil
		r.obs = sentObs("Failed", "", "main-ci-uid", clock.Now().Add(-sentCIEvery).Add(time.Second), clock.Now(), 0)
		pass(t, c)
		if len(openBlocks(l.state(t))) != 1 {
			t.Fatal("red after a failed read was not blocked")
		}
	})
	t.Run("already named", func(t *testing.T) {
		t.Parallel()
		c, l, r, clock, start := sentFixture(t)
		clock.Step(20 * time.Minute)
		r.obs = sentObs("Failed", "", "main-ci-uid", start.Add(time.Second), clock.Now(), 0)
		pass(t, c)
		s := l.state(t)
		// A fresh process (empty memory) sees the same red: no second block.
		fresh := &Controller{Ledger: c.Ledger, Runs: c.Runs, Windows: c.Windows, Policy: c.Policy, Clock: c.Clock, Cluster: c.Cluster, Sources: c.Sources}
		t.Cleanup(func() { _ = fresh.Close() })
		if wrote, err := fresh.watchSentCI(context.Background(), s); wrote || err != nil || len(s.BlockHistory) != 1 {
			t.Fatal("reopened for a run already named", wrote, err, len(s.BlockHistory))
		}
	})
	t.Run("lost race", func(t *testing.T) {
		t.Parallel()
		c, l, r, clock, start := sentFixture(t)
		clock.Step(20 * time.Minute)
		r.obs = sentObs("Failed", "", "main-ci-uid", start.Add(time.Second), clock.Now(), 0)
		stale := l.state(t)
		stale.LastSeq-- // another writer moved the stream since this read
		if wrote, err := c.watchSentCI(context.Background(), stale); wrote || err != nil || c.sent[sendKey(t, l, clock.Now())].settled {
			t.Fatal("a lost compare-and-append counted as written", wrote, err)
		}
		if wrote, err := c.watchSentCI(context.Background(), l.state(t)); !wrote || err != nil {
			t.Fatal("not retried on the next pass", wrote, err)
		}
	})
}

func TestSentTriesOnlyTriesThatMayHaveReachedSTG(t *testing.T) {
	t.Parallel()
	_, l, _, _, start := sentFixture(t)
	s := l.state(t)
	t1 := s.Batches[leaseBatch].Tries[1]
	got := sentTries(s, start)
	if len(got) != 1 || got[0].id != leaseBatch || got[0].n != 1 || got[0].p != sha("a") || !got[0].sent.Equal(start) || !got[0].accepted.Equal(start) ||
		got[0].permit != t1.Permit.Seq || got[0].key != fmt.Sprintf("%s/1/%d", leaseBatch, t1.Permit.Seq) || got[0].run != t1.Started.Record.String("run") {
		t.Fatal("control", got)
	}
	for name, mutate := range map[string]func(*store.StgTry){
		"unsent":      func(tr *store.StgTry) { tr.Unsent.Seq = 99 },
		"not started": func(tr *store.StgTry) { tr.Started = store.StgEvent{} },
	} {
		mutated := l.state(t)
		mutate(mutated.Batches[leaseBatch].Tries[1])
		if got := sentTries(mutated, start); len(got) != 0 {
			t.Fatalf("%s: watched %v", name, got)
		}
	}
	s = l.state(t)
	s.Batches[leaseBatch].Tries[1].Quarantined.Seq = 99
	if got := sentTries(s, start); len(got) != 1 {
		t.Fatal("a quarantined try may have reached STG and is watched")
	}
}

// The common case, as measured on 3e7b8e48: B5 sends 1–3 minutes after the
// main push and main's full CI ends about 16 minutes after the push. With
// that run still undecided the send is not settled; when it then ends red,
// B5 blocks.
func TestSentCIUndecidedMainRunKeepsWatching(t *testing.T) {
	t.Parallel()
	c, l, r, clock, start := sentFixture(t)
	key := sendKey(t, l, start)
	clock.Step(11 * time.Minute)
	r.obs = sentObs("Succeeded", "", "landing-uid", start, clock.Now(), 1)
	r.obs.Run = landingRun
	for i := 0; i < 2; i++ {
		if wrote, err := c.watchSentCI(context.Background(), l.state(t)); wrote || err != nil || c.sent[key].settled {
			t.Fatal("settled while main's run was still undecided", wrote, err)
		}
		clock.Step(sentCIEvery)
	}
	// main's run (created a second after the CI the send rested on) has
	// now ended red.
	r.obs = sentObs("Failed", "", "main-ci-uid", start.Add(time.Second), clock.Now(), 0)
	pass(t, c)
	if r.reads != 3 || len(openBlocks(l.state(t))) != 1 || openBlocks(l.state(t))[0].Opened.Record.String("why") != "ci_red" {
		t.Fatal("main's red was not blocked", r.reads)
	}
}

// withTry2 is the batch after try 1 failed verification and a retry started
// try 2 on the same frozen inputs, at sent (its own started permit).
func withTry2(s store.StgState, sent time.Time) store.StgState {
	b := s.Batches[leaseBatch]
	t1 := b.Tries[1]
	permit := store.StgEvent{Seq: s.LastSeq + 7, At: sent, Record: t1.Started.Record}
	b.Tries[2] = &store.StgTry{Snapshot: t1.Snapshot, Started: permit, Permit: permit}
	b.Try = 2
	return s
}

// A retry is a send of its own: try 1 settling does not settle try 2 (the
// final gate passes over undecided runs for try 2 too, so its send needs the
// same backstop).
func TestSentCIRetryIsASendOfItsOwn(t *testing.T) {
	t.Parallel()
	c, l, r, clock, start := sentFixture(t)
	clock.Step(17 * time.Minute)
	r.obs = sentObs("Succeeded", "", "main-ci-uid", start.Add(time.Minute), clock.Now(), 0)
	if wrote, err := c.watchSentCI(context.Background(), l.state(t)); wrote || err != nil || r.reads != 1 {
		t.Fatal("try 1 control was not read", wrote, err)
	}
	sent2 := start.Add(40 * time.Minute)
	clock.SetTime(sent2.Add(15 * time.Minute))
	// A rerun of P has ended red; try 2's send, never read yet, goes first.
	r.obs = sentObs("Failed", "", "rerun-uid", start.Add(30*time.Minute), clock.Now(), 0)
	s := withTry2(l.state(t), sent2)
	wrote, err := c.watchSentCI(context.Background(), s)
	if !wrote || err != nil || r.reads != 2 {
		t.Fatal("try 2's send was not watched", wrote, err, r.reads)
	}
	sent := object(object(openBlocks(l.state(t))[0].Opened.Record, "detail"), "sent")
	if sent.Int("try") != 2 || sent.Int("permit_seq") != s.Batches[leaseBatch].Tries[2].Permit.Seq {
		t.Fatal("marker", sent)
	}
}

// A resend replaces the permit: the send watched is the latest permit's,
// and a block names it.
func TestSentCIResendIsTheSendWatched(t *testing.T) {
	t.Parallel()
	c, l, r, clock, start := sentFixture(t)
	resent := start.Add(20 * time.Minute)
	clock.SetTime(resent.Add(11 * time.Minute))
	// main's run, created 15 minutes after the first permit, has ended red.
	r.obs = sentObs("Failed", "", "main-ci-uid", start.Add(15*time.Minute), clock.Now(), 0)
	s := l.state(t)
	tr := s.Batches[leaseBatch].Tries[1]
	tr.Permit = store.StgEvent{Seq: s.LastSeq + 3, At: resent, Record: tr.Started.Record}
	wrote, err := c.watchSentCI(context.Background(), s)
	if !wrote || err != nil || r.reads != 1 {
		t.Fatal("the resend's send was not read", wrote, err, r.reads)
	}
	if sent := object(object(openBlocks(l.state(t))[0].Opened.Record, "detail"), "sent"); sent.Int("permit_seq") != tr.Permit.Seq {
		t.Fatal("marker", sent)
	}
}

// An interval the existing rules resolved stays resolved across a restart:
// a rerun C ended green and closed it. A fresh process that reads C settles;
// one that still reads the red B (C pruned, say) must not reopen it (C is
// already recorded against the old interval and cannot resolve a new one).
func TestSentCIResolvedIntervalIsNotReopenedAfterRestart(t *testing.T) {
	t.Parallel()
	c, l, r, clock, start := sentFixture(t)
	clock.Step(17 * time.Minute)
	r.obs = sentObs("Failed", "", "main-ci-uid", start.Add(time.Second), clock.Now(), 0)
	pass(t, c)
	opened := openBlocks(l.state(t))
	if len(opened) != 1 {
		t.Fatal("control: no interval")
	}
	block := opened[0]
	clock.SetTime(start.Add(35 * time.Minute)) // the rerun ended after it was created
	now := clock.Now().Format(time.RFC3339Nano)
	rerun := map[string]any{"name": "rerun", "uid": "rerun-uid", "created_at": start.Add(30 * time.Minute).Format(time.RFC3339Nano), "read_at": now, "platform_commit": sha("a"), "ui_commit": sha("b"), "infra_commit": sha("c"), "pipeline_ref": "bifrost-ci-platform", "repo_url": landing.LandingRepos["bifrost-platform"].Remote}
	proof := store.StgRecord{"v": "1", "scope": "block", "kind": "ci_observed", "block": block.Opened.Record.String("block"), "interval_seq": block.Opened.Seq, "P": sha("a"), "ci": rerun, "status": "Succeeded"}
	l.add(t, proof, unblockRecord(block.Opened.Record.String("block"), "inputs_changed", int64(len(l.entries)+1)))
	if len(openBlocks(l.state(t))) != 0 {
		t.Fatal("control: the rerun did not resolve the interval")
	}
	commits := l.commits
	for _, obs := range []store.StgCIObservation{
		sentObs("Succeeded", "", "rerun-uid", start.Add(30*time.Minute), clock.Now(), 0),
		sentObs("Failed", "", "main-ci-uid", start.Add(time.Second), clock.Now(), 0),
	} {
		r.obs = obs
		fresh := &Controller{Ledger: c.Ledger, Runs: c.Runs, Windows: c.Windows, Policy: c.Policy, Clock: c.Clock, Cluster: c.Cluster, Sources: c.Sources}
		t.Cleanup(func() { _ = fresh.Close() })
		wrote, err := fresh.watchSentCI(context.Background(), l.state(t))
		if wrote || err != nil || l.commits != commits || len(openBlocks(l.state(t))) != 0 {
			t.Fatal("a resolved interval was reopened after a restart", obs.UID, wrote, err, l.commits-commits)
		}
		// Neither settles while main is at P.
		if fresh.sent[sendKey(t, l, clock.Now())].settled {
			t.Fatal("settled while main is at P", obs.UID)
		}
	}
}

// R5:5.7-2 pre-review: a block settles nothing while main is at P. The
// land/prod branch CI of P ends red and is blocked; a rerun's green resolves
// it; main's push CI, created late, then ends red with main still at P. The
// same running process opens a second interval for it.
func TestSentCIRedAfterAResolvedBlockIsBlockedAgain(t *testing.T) {
	t.Parallel()
	c, l, r, clock, start := sentFixture(t)
	clock.Step(17 * time.Minute)
	r.obs = sentObs("Failed", "", "branch-uid", start.Add(30*time.Second), clock.Now(), 0)
	pass(t, c)
	opened := openBlocks(l.state(t))
	if len(opened) != 1 {
		t.Fatal("control: no interval")
	}
	block := opened[0]
	clock.SetTime(start.Add(35 * time.Minute))
	now := clock.Now().Format(time.RFC3339Nano)
	rerun := map[string]any{"name": "rerun", "uid": "rerun-uid", "created_at": start.Add(18 * time.Minute).Format(time.RFC3339Nano), "read_at": now, "platform_commit": sha("a"), "ui_commit": sha("b"), "infra_commit": sha("c"), "pipeline_ref": "bifrost-ci-platform", "repo_url": landing.LandingRepos["bifrost-platform"].Remote}
	proof := store.StgRecord{"v": "1", "scope": "block", "kind": "ci_observed", "block": block.Opened.Record.String("block"), "interval_seq": block.Opened.Seq, "P": sha("a"), "ci": rerun, "status": "Succeeded"}
	l.add(t, proof, unblockRecord(block.Opened.Record.String("block"), "inputs_changed", int64(len(l.entries)+1)))
	if len(openBlocks(l.state(t))) != 0 {
		t.Fatal("control: the rerun did not resolve the interval")
	}
	// The rerun's green, read with main at P: nothing opened, not settled.
	r.obs = sentObs("Succeeded", "", "rerun-uid", start.Add(18*time.Minute), clock.Now(), 0)
	if wrote, err := c.watchSentCI(context.Background(), l.state(t)); wrote || err != nil || c.sent[sendKey(t, l, clock.Now())].settled {
		t.Fatal("the rerun's green settled the send", wrote, err)
	}
	clock.SetTime(start.Add(50 * time.Minute))
	r.obs = sentObs("Failed", "", "main-ci-uid", start.Add(25*time.Minute), clock.Now(), 0)
	wrote, err := c.watchSentCI(context.Background(), l.state(t))
	s := l.state(t)
	again := openBlocks(s)
	if !wrote || err != nil || len(s.BlockHistory) != 2 || len(again) != 1 || object(object(again[0].Opened.Record, "detail"), "ci").String("uid") != "main-ci-uid" {
		t.Fatal("main's late red after a resolved block was not blocked", wrote, err, len(s.BlockHistory), len(again))
	}
}

// As the final gate does, a different red of P found while an interval for
// P is open adds nothing: no second interval, no second card, not settled.
func TestSentCIDifferentRedWhileOpenAddsNothing(t *testing.T) {
	t.Parallel()
	c, l, r, clock, start := sentFixture(t)
	clock.Step(17 * time.Minute)
	r.obs = sentObs("Failed", "", "main-ci-uid", start.Add(time.Second), clock.Now(), 0)
	pass(t, c)
	if len(openBlocks(l.state(t))) != 1 {
		t.Fatal("control: no interval")
	}
	clock.Step(20 * time.Minute)
	r.obs = sentObs("Failed", "", "rerun-uid", start.Add(19*time.Minute), clock.Now(), 0)
	commits := l.commits
	key := sendKey(t, l, clock.Now())
	if wrote, err := c.watchSentCI(context.Background(), l.state(t)); wrote || err != nil || l.commits != commits || len(l.state(t).BlockHistory) != 1 || c.sent[key].settled {
		t.Fatal("a second red while open opened another interval", wrote, err, len(l.state(t).BlockHistory))
	}
}

// Option A (coordinator 11-01): a refusal found after the send blocks only
// while main is still at P. Once main has moved on it is handed to the later
// commit's own checks: nothing is written, a notice says so. A failed read
// of main opens nothing, is not taken as main having moved, and is retried.
func TestSentCIBlocksOnlyWhileMainIsStillAtP(t *testing.T) {
	t.Parallel()
	red := func(r *sentCIReader, start time.Time, now time.Time) {
		r.obs = sentObs("Failed", "", "main-ci-uid", start.Add(time.Second), now, 0)
	}
	t.Run("still at P", func(t *testing.T) {
		t.Parallel()
		c, l, r, clock, start := sentFixture(t)
		clock.Step(17 * time.Minute)
		red(r, start, clock.Now())
		if wrote, err := c.watchSentCI(context.Background(), l.state(t)); !wrote || err != nil || r.headReads != 1 || len(openBlocks(l.state(t))) != 1 || len(r.notices) != 0 {
			t.Fatal("main still at P: not blocked", wrote, err, r.headReads, r.notices)
		}
	})
	t.Run("moved on", func(t *testing.T) {
		t.Parallel()
		c, l, r, clock, start := sentFixture(t)
		clock.Step(17 * time.Minute)
		red(r, start, clock.Now())
		r.head = sha("d")
		commits := l.commits
		key := sendKey(t, l, clock.Now())
		if wrote, err := c.watchSentCI(context.Background(), l.state(t)); wrote || err != nil || l.commits != commits || len(openBlocks(l.state(t))) != 0 {
			t.Fatal("blocked P after main moved on", wrote, err)
		}
		if !c.sent[key].settled || len(r.notices) != 1 || !strings.Contains(r.notices[0], "main has moved on") || !strings.Contains(r.notices[0], sha("d")) || !strings.Contains(r.notices[0], "main-ci-run") {
			t.Fatal("hand-off not settled or not said", c.sent[key].settled, r.notices)
		}
		clock.Step(time.Hour)
		if _, _ = c.watchSentCI(context.Background(), l.state(t)); r.reads != 1 || r.headReads != 1 {
			t.Fatal("handed-off send read again", r.reads, r.headReads)
		}
	})
	t.Run("main read empty", func(t *testing.T) {
		t.Parallel()
		c, l, r, clock, start := sentFixture(t)
		clock.Step(17 * time.Minute)
		red(r, start, clock.Now())
		r.head = "" // an ls-remote that printed nothing: not main having moved
		key := sendKey(t, l, clock.Now())
		if wrote, err := c.watchSentCI(context.Background(), l.state(t)); wrote || err == nil || c.sent[key].settled || len(openBlocks(l.state(t))) != 0 || len(r.notices) != 0 {
			t.Fatal("an empty main read was taken as an answer", wrote, err, r.notices)
		}
	})
	t.Run("main unreadable", func(t *testing.T) {
		t.Parallel()
		c, l, r, clock, start := sentFixture(t)
		clock.Step(17 * time.Minute)
		red(r, start, clock.Now())
		r.headErr = errors.New("gitea unreachable")
		key := sendKey(t, l, clock.Now())
		if wrote, err := c.watchSentCI(context.Background(), l.state(t)); wrote || !errors.Is(err, r.headErr) || c.sent[key].settled || len(openBlocks(l.state(t))) != 0 || len(r.notices) != 0 {
			t.Fatal("a failed main read was taken as an answer", wrote, err, r.notices)
		}
		clock.Step(sentCIEvery)
		r.headErr = nil
		if wrote, err := c.watchSentCI(context.Background(), l.state(t)); !wrote || err != nil || r.headReads != 2 || len(openBlocks(l.state(t))) != 1 {
			t.Fatal("not read again after a failed main read", wrote, err, r.headReads)
		}
		// Without a main reader nothing is opened either.
		c2, l2, r2, clock2, start2 := sentFixture(t)
		clock2.Step(17 * time.Minute)
		red(r2, start2, clock2.Now())
		c2.Sources.Head = nil
		if wrote, err := c2.watchSentCI(context.Background(), l2.state(t)); wrote || err == nil || len(openBlocks(l2.state(t))) != 0 {
			t.Fatal("blocked without reading main", wrote, err)
		}
	})
}

// The watch keeps state only for sends still watched: a send past the
// horizon, one a resend replaced and one proven unsent are dropped.
func TestSentCIWatchForgetsSendsNoLongerWatched(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"past the horizon", "replaced by a resend", "proven unsent"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			c, l, r, clock, start := sentFixture(t)
			r.obs = sentObs("Succeeded", "", "ci-uid", start, clock.Now(), 1) // main's run undecided
			first := sendKey(t, l, clock.Now())
			if _, err := c.watchSentCI(context.Background(), l.state(t)); err != nil || c.sent[first] == nil || c.sent[first].settled {
				t.Fatal("control: the send is not watched", err)
			}
			s := l.state(t)
			tr := s.Batches[leaseBatch].Tries[1]
			want := 0
			switch mode {
			case "past the horizon":
				clock.SetTime(start.Add(sentCIHorizon + time.Minute))
			case "replaced by a resend":
				clock.Step(sentCIEvery)
				tr.Permit = store.StgEvent{Seq: s.LastSeq + 3, At: clock.Now(), Record: tr.Started.Record}
				want = 1
			case "proven unsent":
				tr.Unsent = store.StgEvent{Seq: s.LastSeq + 3, At: clock.Now()}
			}
			if _, err := c.watchSentCI(context.Background(), s); err != nil {
				t.Fatal(err)
			}
			if _, kept := c.sent[first]; kept || len(c.sent) != want {
				t.Fatalf("watch state kept: %v", c.sent)
			}
		})
	}
}
