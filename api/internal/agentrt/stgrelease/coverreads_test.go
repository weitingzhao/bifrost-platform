package stgrelease

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
)

// coverFakes counts the full reads of one controller by P. The full read
// answers with full(p); by default it covers.
type coverFakes struct {
	fullByP map[string]int
	full    func(p string) (store.MainEvidence, error)
}

func (f *coverFakes) fulls() int {
	n := 0
	for _, v := range f.fullByP {
		n += v
	}
	return n
}

func coverFixture(t *testing.T) (*Controller, *memoryLedger, *externalRuns, *coverFakes) {
	t.Helper()
	c, l, x, _ := externalFixture(t)
	f := &coverFakes{fullByP: map[string]int{}}
	f.full = func(p string) (store.MainEvidence, error) {
		return store.MainEvidence{Head: p, HasR: true, Validity: "effective"}, nil
	}
	c.Sources.ReleaseEvidence = func(_ context.Context, _ store.StgState, _ store.StgMemberKey, p string) (store.MainEvidence, error) {
		f.fullByP[p]++
		return f.full(p)
	}
	return c, l, x, f
}

// newerEvidence lists a second manual run, archived after the first, so its
// evidence (P = sha("d")) is the newer one.
func newerEvidence(t *testing.T, x *externalRuns) {
	t.Helper()
	o := controlObservationUID(t, delivery.NamedDeliverInput{Batch: 3, Try: 1, IntentSeq: 31, P: sha("d"), U: sha("b"), Infra: sha("c")}, "Succeeded", "manual-uid-2")
	o.Run.SetAnnotations(nil)
	x.listed = append(x.listed, o.Run)
	x.byUID["manual-uid-2"] = o
}

// R5:5.7-5: a pair whose complete read says P does not contain R is read
// once; later passes skip it. A new member's pairs are read in full.
func TestCoverReadsReadAnAbsentPairOnce(t *testing.T) {
	t.Parallel()
	c, l, _, f := coverFixture(t)
	f.full = func(p string) (store.MainEvidence, error) { return store.MainEvidence{Head: p, HasR: false}, nil }
	externalLanded(t, l, controlMember)
	externalPass(t, c)
	externalPass(t, c)
	externalPass(t, c)
	if f.fulls() != 1 || len(l.state(t).Covers) != 0 {
		t.Fatal("absent pair", f.fulls(), len(l.state(t).Covers))
	}
	second := store.StgMemberKey{Root: "t_01ARZ3NDEKTSV4RRFFQ69G5FAW", Op: "land:two:1", R: sha("2")}
	externalLanded(t, l, second)
	externalPass(t, c)
	externalPass(t, c)
	if f.fulls() != 2 {
		t.Fatal("new member", f.fulls())
	}
}

// Only a complete "not contained" is remembered. An error, Unreadable, a
// wrong Head, or HasR with a validity that does not cover is read again on
// the next pass, exactly as before.
func TestCoverReadsRememberOnlyCompleteFullReads(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name     string
		ev       store.MainEvidence
		err      error
		remember bool
	}{
		{"absent", store.MainEvidence{HasR: false}, nil, true},
		{"absent with error", store.MainEvidence{HasR: false}, errors.New("landing git fetch failed"), false},
		{"unreadable", store.MainEvidence{Unreadable: true}, nil, false},
		{"contained, validity unknown", store.MainEvidence{HasR: true, Validity: "unknown"}, nil, false},
		{"contained, reverted", store.MainEvidence{HasR: true, Validity: "reverted"}, nil, false},
		{"wrong head", store.MainEvidence{Head: sha("9")}, nil, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ctl, l, _, f := coverFixture(t)
			f.full = func(p string) (store.MainEvidence, error) {
				ev := c.ev
				if ev.Head == "" {
					ev.Head = p
				}
				return ev, c.err
			}
			externalLanded(t, l, controlMember)
			_ = ctl.ExternalPass(context.Background())
			_ = ctl.CoverPass(context.Background())
			_ = ctl.ExternalPass(context.Background())
			_ = ctl.CoverPass(context.Background())
			want := 2
			if c.remember {
				want = 1
			}
			if f.fulls() != want || len(l.state(t).Covers) != 0 {
				t.Fatal("full reads", f.fulls(), "want", want, "covers", len(l.state(t).Covers))
			}
		})
	}
}

// The loop keeps its order: newest evidence first, the break after a cover,
// and older evidence read only when the newer cannot cover.
func TestCoverReadsKeepNewestFirstAndTheBreak(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name      string
		newest    store.MainEvidence // full read of the newest P
		wantOld   int
		wantCover string
	}{
		{"newest covers", store.MainEvidence{HasR: true, Validity: "effective"}, 0, sha("d")},
		{"newest absent", store.MainEvidence{HasR: false}, 1, sha("a")},
		{"newest contained but not effective", store.MainEvidence{HasR: true, Validity: "unknown"}, 1, sha("a")},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ctl, l, x, f := coverFixture(t)
			newerEvidence(t, x)
			f.full = func(p string) (store.MainEvidence, error) {
				if p == sha("d") {
					ev := c.newest
					ev.Head = p
					return ev, nil
				}
				return store.MainEvidence{Head: p, HasR: true, Validity: "effective"}, nil
			}
			externalPass(t, ctl) // archives both runs; no member yet
			if n := len(l.state(t).Evidence); n != 2 {
				t.Fatal("evidence", n)
			}
			externalLanded(t, l, controlMember)
			externalPass(t, ctl)
			s := l.state(t)
			if f.fullByP[sha("d")] != 1 || f.fullByP[sha("a")] != c.wantOld || len(s.Covers) != 1 {
				t.Fatal("reads newest", f.fullByP[sha("d")], "older", f.fullByP[sha("a")], "covers", len(s.Covers))
			}
			for _, cv := range s.Covers {
				if got := s.Evidence[cv.Record.String("evidence")].Record.String("P"); got != c.wantCover {
					t.Fatal("covered by", got, "want", c.wantCover)
				}
			}
		})
	}
}

// refusingCover is a ledger whose Cover writes nothing: false, or an error.
type refusingCover struct {
	*memoryLedger
	err          error
	calls, reads int
}

func (r *refusingCover) Read(ctx context.Context) (store.StgState, error) {
	r.reads++
	return r.memoryLedger.Read(ctx)
}

func (r *refusingCover) Cover(context.Context, int64, string, store.StgMemberKey, store.MainEvidence) (bool, error) {
	r.calls++
	return false, r.err
}

// After Cover, refused or failed, the ledger is reread and the member's loop
// ends, as before: the older evidence is not tried for it in this pass.
func TestCoverReadsBreakAfterARefusedCover(t *testing.T) {
	t.Parallel()
	for _, err := range []error{nil, errors.New("cover conflict")} {
		ctl, l, x, f := coverFixture(t)
		newerEvidence(t, x)
		externalPass(t, ctl)
		ledger := &refusingCover{memoryLedger: l, err: err}
		ctl.Ledger = ledger
		externalLanded(t, l, controlMember)
		passErr := ctl.CoverPass(context.Background()) // the run is archived: only coverage reads the ledger
		if ledger.calls != 1 || f.fullByP[sha("d")] != 1 || f.fullByP[sha("a")] != 0 || len(l.state(t).Covers) != 0 {
			t.Fatal("refused cover", err, ledger.calls, f.fullByP)
		}
		if ledger.reads != 2 { // the pass's first read and the reread after Cover
			t.Fatal("ledger reads", ledger.reads)
		}
		if (err != nil) != (passErr != nil) {
			t.Fatal("pass error", passErr, "cover error", err)
		}
	}
}

// Remembering a pair already known never empties a full memory; a new pair
// past the bound starts it over.
func TestCoverReadsMemoryBound(t *testing.T) {
	t.Parallel()
	c := &Controller{}
	c.rememberAbsent(sha("1"), sha("a"))
	for i := 1; len(c.absent) < absentMax; i++ {
		c.rememberAbsent(sha("2"), string(rune('a'+i%26))+string(rune(i)))
	}
	c.rememberAbsent(sha("1"), sha("a"))
	if len(c.absent) != absentMax || !c.knownAbsent(sha("1"), sha("a")) {
		t.Fatal("repeat emptied the memory", len(c.absent))
	}
	c.rememberAbsent(sha("3"), sha("b"))
	if len(c.absent) != 1 || !c.knownAbsent(sha("3"), sha("b")) || c.knownAbsent(sha("1"), sha("a")) {
		t.Fatal("past the bound", len(c.absent))
	}
}

// refreshingCover covers through the memory ledger, counts the ledger reads,
// and after its first cover appends one more evidence, as another writer
// would between two members.
type refreshingCover struct {
	*memoryLedger
	extra store.StgRecord
	added bool
	reads int
}

func (r *refreshingCover) Read(ctx context.Context) (store.StgState, error) {
	r.reads++
	return r.memoryLedger.Read(ctx)
}

func (r *refreshingCover) Cover(ctx context.Context, seq int64, id string, m store.StgMemberKey, ev store.MainEvidence) (bool, error) {
	ok, err := r.memoryLedger.Cover(ctx, seq, id, m, ev)
	if ok && !r.added {
		r.added = true
		if _, e := r.memoryLedger.Commit(ctx, int64(len(r.memoryLedger.entries)), []store.StgRecord{r.extra}); e != nil {
			return ok, e
		}
	}
	return ok, err
}

// Review of R5:5.7-5 round 3: after each Cover the ledger is reread, so the
// next member reads the evidence that appeared meanwhile, newest first.
func TestCoverReadsRereadAfterCoverSeesNewEvidence(t *testing.T) {
	t.Parallel()
	ctl, l, _, f := coverFixture(t)
	externalPass(t, ctl) // archives the first manual run; no member yet
	s := l.state(t)
	if len(s.Evidence) != 1 {
		t.Fatal("evidence", len(s.Evidence))
	}
	extra := store.StgRecord{}
	for _, e := range s.Evidence {
		for k, v := range e.Record {
			extra[k] = v
		}
	}
	extra["evidence"], extra["run"], extra["P"] = "cluster/cicd/manual-uid-3", "manual-run-3", sha("e")
	ledger := &refreshingCover{memoryLedger: l, extra: extra}
	ctl.Ledger = ledger
	second := store.StgMemberKey{Root: "t_01ARZ3NDEKTSV4RRFFQ69G5FAW", Op: "land:two:1", R: sha("2")}
	externalLanded(t, l, controlMember)
	externalLanded(t, l, second)
	if err := ctl.CoverPass(context.Background()); err != nil { // the run is archived: only coverage reads the ledger
		t.Fatal(err)
	}
	s = l.state(t)
	if !ledger.added || len(s.Covers) != 2 || f.fullByP[sha("a")] != 1 || f.fullByP[sha("e")] != 1 {
		t.Fatal("covers", len(s.Covers), "reads by P", f.fullByP)
	}
	if ledger.reads != 3 { // the pass's first read and one after each Cover
		t.Fatal("ledger reads", ledger.reads)
	}
	byP := map[string]int{}
	for _, cv := range s.Covers {
		byP[s.Evidence[cv.Record.String("evidence")].Record.String("P")]++
	}
	if byP[sha("a")] != 1 || byP[sha("e")] != 1 {
		t.Fatal("covered by", byP)
	}
}

// The coverage line is written for a slow pass, and for a fast one at most
// once in coverSample.
func TestCoverReadsSampleTheFastPasses(t *testing.T) {
	t.Parallel()
	c := &Controller{}
	c.logCover(time.Now(), coverStats{})
	first := c.coverLogged
	if first.IsZero() {
		t.Fatal("first fast pass not logged")
	}
	for range 100 {
		c.logCover(time.Now(), coverStats{})
	}
	if !c.coverLogged.Equal(first) {
		t.Fatal("fast passes logged within the sample")
	}
	c.logCover(time.Now().Add(-slowPassLog), coverStats{})
	if c.coverLogged.Equal(first) {
		t.Fatal("slow pass not logged")
	}
	slow := c.coverLogged
	c.logCover(time.Now(), coverStats{})
	if !c.coverLogged.Equal(slow) {
		t.Fatal("fast pass right after a slow one logged")
	}
	c.coverLogged = time.Now().Add(-coverSample - time.Second)
	c.logCover(time.Now(), coverStats{})
	if time.Since(c.coverLogged) > time.Second {
		t.Fatal("fast pass after the sample not logged")
	}
}

// sessionFakes counts the opens, reads and closes of a coverage workspace
// whose reads answer as the controller's full read does.
type sessionFakes struct {
	opens, reads, closes int
	openErr, closeErr    error
}

func withSession(c *Controller, f *coverFakes, sf *sessionFakes) {
	c.Sources.EvidenceSession = func(context.Context) (EvidenceRead, func() error, error) {
		sf.opens++
		if sf.openErr != nil {
			return nil, nil, sf.openErr
		}
		read := func(_ context.Context, _ store.StgState, _ store.StgMemberKey, p string) (store.MainEvidence, error) {
			sf.reads++
			return f.full(p)
		}
		return read, func() error { sf.closes++; return sf.closeErr }, nil
	}
}

// R5:5.7-5 slice 4: a pass's full reads go through one workspace, opened at
// the first full read and closed at the end of the pass; the answers and
// the cover are the same as reading pair by pair.
func TestCoverReadsOneWorkspacePerPass(t *testing.T) {
	t.Parallel()
	ctl, l, x, f := coverFixture(t)
	newerEvidence(t, x)
	f.full = func(p string) (store.MainEvidence, error) {
		if p == sha("d") {
			return store.MainEvidence{Head: p, HasR: false}, nil
		}
		return store.MainEvidence{Head: p, HasR: true, Validity: "effective"}, nil
	}
	sf := &sessionFakes{}
	withSession(ctl, f, sf)
	externalPass(t, ctl) // archives both runs; no member, no full read
	if sf.opens != 0 {
		t.Fatal("opened a workspace with nothing to read", sf.opens)
	}
	externalLanded(t, l, controlMember)
	externalPass(t, ctl)
	s := l.state(t)
	if sf.opens != 1 || sf.reads != 2 || sf.closes != 1 || f.fulls() != 0 {
		t.Fatal("workspace", sf.opens, sf.reads, sf.closes, "pair by pair", f.fulls())
	}
	if len(s.Covers) != 1 {
		t.Fatal("covers", len(s.Covers))
	}
	for _, cv := range s.Covers {
		if got := s.Evidence[cv.Record.String("evidence")].Record.String("P"); got != sha("a") {
			t.Fatal("covered by", got)
		}
	}
}

// A workspace that fails to open is tried once per pass; the pass reads
// pair by pair and covers as before.
func TestCoverReadsWorkspaceThatFailsToOpenReadsPairByPair(t *testing.T) {
	t.Parallel()
	ctl, l, x, f := coverFixture(t)
	newerEvidence(t, x)
	f.full = func(p string) (store.MainEvidence, error) {
		if p == sha("d") {
			return store.MainEvidence{Head: p, HasR: false}, nil
		}
		return store.MainEvidence{Head: p, HasR: true, Validity: "effective"}, nil
	}
	sf := &sessionFakes{openErr: errors.New("main's history is not complete")}
	withSession(ctl, f, sf)
	externalPass(t, ctl)
	externalLanded(t, l, controlMember)
	externalPass(t, ctl)
	if sf.opens != 1 || sf.reads != 0 || sf.closes != 0 || f.fulls() != 2 || len(l.state(t).Covers) != 1 {
		t.Fatal("fallback", sf.opens, sf.reads, sf.closes, f.fulls(), len(l.state(t).Covers))
	}
}

// The workspace is closed on every way out of the pass: the end, a close
// that fails (the cover stands), and the return when the rule turns none
// during a read.
func TestCoverReadsWorkspaceClosesOnEveryReturn(t *testing.T) {
	t.Parallel()
	for _, way := range []string{"end", "close fails", "rule turns none"} {
		t.Run(way, func(t *testing.T) {
			t.Parallel()
			ctl, l, _, f := coverFixture(t)
			sf := &sessionFakes{}
			if way == "close fails" {
				sf.closeErr = errors.New("remove failed")
			}
			p := ctl.Policy()
			rule := *p.Rule
			land := *rule.Landing
			land.Repos = append(land.Repos[:0:0], land.Repos...)
			rule.Landing = &land
			p.Rule = &rule
			ctl.Policy = func() WindowPolicy { return p }
			if way == "rule turns none" {
				f.full = func(p string) (store.MainEvidence, error) {
					land.Repos[0].STGRelease = "none"
					return store.MainEvidence{Head: p, HasR: true, Validity: "effective"}, nil
				}
			}
			withSession(ctl, f, sf)
			externalLanded(t, l, controlMember)
			_ = ctl.ExternalPass(context.Background())
			_ = ctl.CoverPass(context.Background())
			if sf.opens != 1 || sf.closes != 1 {
				t.Fatal("opens", sf.opens, "closes", sf.closes)
			}
			want := 1
			if way == "rule turns none" {
				want = 0
			}
			if n := len(l.state(t).Covers); n != want {
				t.Fatal("covers", n, "want", want)
			}
		})
	}
}

// A read through the workspace that fails is that pair's error, as before:
// not remembered as absent, not covered, and the next evidence is read with
// the same workspace.
func TestCoverReadsWorkspaceReadErrorIsThatPairsOnly(t *testing.T) {
	t.Parallel()
	ctl, l, x, f := coverFixture(t)
	newerEvidence(t, x)
	f.full = func(p string) (store.MainEvidence, error) {
		if p == sha("d") {
			return store.MainEvidence{}, errors.New("landing git fetch failed")
		}
		return store.MainEvidence{Head: p, HasR: true, Validity: "effective"}, nil
	}
	sf := &sessionFakes{}
	withSession(ctl, f, sf)
	externalPass(t, ctl)
	externalLanded(t, l, controlMember)
	if err := errors.Join(ctl.ExternalPass(context.Background()), ctl.CoverPass(context.Background())); err == nil {
		t.Fatal("the failed read's error was not returned")
	}
	s := l.state(t)
	if sf.opens != 1 || sf.reads != 2 || sf.closes != 1 || len(s.Covers) != 1 || ctl.knownAbsent(sha("d"), controlMember.R) {
		t.Fatal("workspace", sf.opens, sf.reads, sf.closes, "covers", len(s.Covers), "remembered the error", ctl.knownAbsent(sha("d"), controlMember.R))
	}
	for _, cv := range s.Covers {
		if got := s.Evidence[cv.Record.String("evidence")].Record.String("P"); got != sha("a") {
			t.Fatal("covered by", got)
		}
	}
}

// R5:5.7-11: coverage needs no manual-run reader. With no ExternalExecution
// on Runs, CoverPass covers a landed member from B5's own archived run.
func TestCoverPassCoversFromAutomaticEvidenceWithoutExternalReader(t *testing.T) {
	t.Parallel()
	c, l, _, _ := controlFixture(t, "Succeeded")
	if _, ok := c.Runs.(ExternalExecution); ok {
		t.Fatal("fixture Runs reads manual runs")
	}
	pass(t, c)
	pass(t, c)
	auto := l.state(t).Evidence["cluster/cicd/run-uid"]
	if auto.Record.String("source") != "automatic" || auto.Record.String("P") != sha("a") {
		t.Fatal("no automatic evidence", auto.Record)
	}
	second := store.StgMemberKey{Root: "t_01ARZ3NDEKTSV4RRFFQ69G5FAW", Op: "land:two:1", R: sha("2")}
	externalLanded(t, l, second)
	c.Sources.ReleaseEvidence = func(_ context.Context, _ store.StgState, _ store.StgMemberKey, p string) (store.MainEvidence, error) {
		return store.MainEvidence{Head: p, HasR: p == sha("a"), Validity: "effective"}, nil
	}
	if err := c.CoverPass(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := l.state(t)
	var covered []string
	for k := range s.Covers {
		if k.Member == second {
			covered = append(covered, k.Evidence)
		}
	}
	if len(covered) != 1 || covered[0] != "cluster/cicd/run-uid" {
		t.Fatal("covers of the landed member", covered, s.Covers)
	}
}
