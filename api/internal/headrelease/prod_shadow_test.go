package headrelease

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Origin of every test in this file. 2026-11-16: 19 PROD releases of the
// platform in one evening, each by a seat holding the window ~129 s; the
// coordinator asked for a shadow of "the platform releases PROD in batches"
// before anyone turns it on. Each test says below what it guards.

const (
	shadowP = "1111111111111111111111111111111111111111"
	shadowC = "2222222222222222222222222222222222222222"
)

var shadowNow = time.Date(2026, 11, 16, 23, 45, 0, 0, time.UTC)

// greenFacts is a moment at which everything holds: STG has run C for 11
// minutes, PROD runs P since 25 minutes ago, C's full CI passed, the policy
// covers P..C, no card, no window, no freeze.
func greenFacts() ProdFacts {
	return ProdFacts{
		Now: shadowNow, Soak: DefaultProdSoak, Gap: DefaultProdGap,
		Prod: Deployed{Commit: shadowP, Since: shadowNow.Add(-25 * time.Minute), Run: "bifrost-deliver-platform-prod-p"},
		Stg:  Deployed{Commit: shadowC, Since: shadowNow.Add(-11 * time.Minute), Run: "bifrost-deliver-platform-c"},
		CI:   CISucceeded, CIRun: "ci-platform-c",
		PolicyCovered: true, PolicyID: "rp-1",
	}
}

// Origin: see the top of the file. Guards the decision itself: all green is
// the only way to "would release"; each condition failing alone is the one
// listed; two failing are both listed (all are judged, not the first); an
// unsettled environment is a wait that names it and does not judge what
// depends on the commit it cannot know; a read that failed is "unknown" and
// never a release.
func TestDecideProd(t *testing.T) {
	cases := []struct {
		name        string
		edit        func(f *ProdFacts)
		would       bool
		failing     []string
		unknown     []string
		skipped     []string
		verdictHas  string
		reasonOf    string
		reasonHas   string
		from, to    string
		emptyFromTo bool
	}{
		{name: "all green", edit: func(*ProdFacts) {}, would: true, verdictHas: "would release 22222222", from: shadowP, to: shadowC},
		{name: "stg unsettled", edit: func(f *ProdFacts) {
			f.Stg = Deployed{Unsettled: "run bifrost-deliver-platform-x may have changed platform/stg"}
			f.CI, f.CIRun, f.PolicyCovered = "", "", false // nothing was asked about a candidate
		}, failing: []string{CondStgSettled}, skipped: []string{CondSoaked, CondDiffers, CondCIStrict, CondPolicy},
			verdictHas: "wait: stg is not settled", reasonOf: CondStgSettled, reasonHas: "may have changed", from: shadowP},
		{name: "prod unsettled", edit: func(f *ProdFacts) { f.Prod = Deployed{Unsettled: "a delivery is newer than the record"} },
			failing: []string{CondProdSettled}, skipped: []string{CondDiffers, CondSpaced}, verdictHas: "wait: prod is not settled", to: shadowC},
		{name: "not soaked", edit: func(f *ProdFacts) { f.Stg.Since = shadowNow.Add(-599 * time.Second) },
			failing: []string{CondSoaked}, verdictHas: "wait: soaked", reasonOf: CondSoaked, reasonHas: "599s of 600s", from: shadowP, to: shadowC},
		{name: "prod runs it already", edit: func(f *ProdFacts) { f.Stg.Commit = shadowP },
			failing: []string{CondDiffers}, verdictHas: "wait: differs_from_prod", from: shadowP, to: shadowP},
		{name: "full CI has no verdict", edit: func(f *ProdFacts) { f.CI, f.CIRun = "", "" },
			failing: []string{CondCIStrict}, reasonOf: CondCIStrict, reasonHas: "no verdict yet", from: shadowP, to: shadowC},
		{name: "full CI failed", edit: func(f *ProdFacts) { f.CI = CIFailed },
			failing: []string{CondCIStrict}, reasonOf: CondCIStrict, reasonHas: "Failed ci-platform-c", from: shadowP, to: shadowC},
		{name: "policy does not cover", edit: func(f *ProdFacts) {
			f.PolicyCovered, f.PolicyReasons = false, []string{"bifrost-platform 11111111..22222222 hits no_ddl: api/migrations/0042.sql"}
		}, failing: []string{CondPolicy}, reasonOf: CondPolicy, reasonHas: "hits no_ddl", from: shadowP, to: shadowC},
		{name: "a release card is open", edit: func(f *ProdFacts) { f.OpenCards = []string{"#524 pending"} },
			failing: []string{CondNoOpenCard}, reasonOf: CondNoOpenCard, reasonHas: "#524 pending", from: shadowP, to: shadowC},
		{name: "window held", edit: func(f *ProdFacts) { f.WindowHeld = "ci-w1 (bifrost-platform)" },
			failing: []string{CondWindowFree}, reasonOf: CondWindowFree, reasonHas: "held by ci-w1", from: shadowP, to: shadowC},
		{name: "too soon after the last prod delivery", edit: func(f *ProdFacts) { f.Prod.Since = shadowNow.Add(-1199 * time.Second) },
			failing: []string{CondSpaced}, reasonOf: CondSpaced, reasonHas: "1199s ago, of 1200s", from: shadowP, to: shadowC},
		{name: "frozen", edit: func(f *ProdFacts) { f.Frozen, f.FreezeReason = true, "incident 12" },
			failing: []string{CondNotFrozen}, reasonOf: CondNotFrozen, reasonHas: "incident 12", from: shadowP, to: shadowC},
		{name: "two fail, both listed", edit: func(f *ProdFacts) {
			f.Stg.Since = shadowNow.Add(-time.Minute)
			f.WindowHeld = "biz-align (bifrost-platform)"
		}, failing: []string{CondSoaked, CondWindowFree}, verdictHas: "wait: soaked, window_free", from: shadowP, to: shadowC},
		{name: "policy could not be evaluated", edit: func(f *ProdFacts) {
			f.PolicyCovered, f.PolicyErr = true, "the policy's evaluation did not finish: context deadline exceeded"
		}, failing: []string{CondPolicy}, unknown: []string{CondPolicy}, verdictHas: "unknown: policy_covers: the policy's evaluation did not finish",
			from: shadowP, to: shadowC},
		{name: "the records could not be read", edit: func(f *ProdFacts) { f.Prod = Deployed{Err: "the release records of platform/prod: connection refused"} },
			failing: []string{CondProdSettled}, unknown: []string{CondProdSettled}, skipped: []string{CondDiffers, CondSpaced},
			verdictHas: "unknown: prod_settled", to: shadowC},
		{name: "the window, the cards and the ledger could not be read", edit: func(f *ProdFacts) {
			f.WindowErr, f.CardsErr, f.CIErr = "the release window: forbidden", "the approvals: disk", "the ledger on bifrost-platform: down"
		}, failing: []string{CondCIStrict, CondNoOpenCard, CondWindowFree}, unknown: []string{CondCIStrict, CondNoOpenCard, CondWindowFree},
			verdictHas: "unknown: ci_strict", from: shadowP, to: shadowC},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := greenFacts()
			c.edit(&f)
			d := DecideProd(f)
			if d.WouldRelease != c.would {
				t.Fatalf("would release = %v: %+v", d.WouldRelease, d)
			}
			if !sameSet(d.Failing, c.failing) || !sameSet(d.Unknown, c.unknown) || !sameSet(d.Skipped, c.skipped) {
				t.Fatalf("failing %v unknown %v skipped %v; want %v %v %v", d.Failing, d.Unknown, d.Skipped, c.failing, c.unknown, c.skipped)
			}
			if d.From != c.from || d.To != c.to {
				t.Fatalf("from %q to %q, want %q %q", d.From, d.To, c.from, c.to)
			}
			if c.verdictHas != "" && !strings.Contains(d.Verdict, c.verdictHas) {
				t.Fatalf("verdict %q lacks %q", d.Verdict, c.verdictHas)
			}
			if c.reasonOf != "" && !strings.Contains(d.Reasons[c.reasonOf], c.reasonHas) {
				t.Fatalf("reason of %s = %q, lacks %q", c.reasonOf, d.Reasons[c.reasonOf], c.reasonHas)
			}
			if d.WouldRelease && (len(d.Failing)+len(d.Unknown)+len(d.Skipped) > 0 || d.From == "" || d.To == "" || d.From == d.To) {
				t.Fatalf("a release with something failing or nothing to release: %+v", d)
			}
			for _, cond := range append(append([]string{}, d.Failing...), d.Skipped...) {
				if d.Reasons[cond] == "" {
					t.Fatalf("%s has no reason", cond)
				}
			}
		})
	}
}

// Origin: see the top of the file. Guards "every condition is judged": taken
// one at a time, each of the ten can be the only one that fails. A condition
// added to ProdConditions without a case here fails this test.
func TestEachProdConditionCanFailAlone(t *testing.T) {
	alone := map[string]func(f *ProdFacts){
		CondProdSettled: func(f *ProdFacts) { f.Prod = Deployed{Unsettled: "x"} },
		CondStgSettled:  func(f *ProdFacts) { f.Stg = Deployed{Unsettled: "x"} },
		CondSoaked:      func(f *ProdFacts) { f.Stg.Since = f.Now },
		CondDiffers:     func(f *ProdFacts) { f.Stg.Commit = f.Prod.Commit },
		CondCIStrict:    func(f *ProdFacts) { f.CI = "" },
		CondPolicy:      func(f *ProdFacts) { f.PolicyCovered = false },
		CondNoOpenCard:  func(f *ProdFacts) { f.OpenCards = []string{"#1 approved"} },
		CondWindowFree:  func(f *ProdFacts) { f.WindowHeld = "someone" },
		CondSpaced:      func(f *ProdFacts) { f.Prod.Since = f.Now },
		CondNotFrozen:   func(f *ProdFacts) { f.Frozen = true },
	}
	if len(alone) != len(ProdConditions) {
		t.Fatalf("%d conditions, %d cases", len(ProdConditions), len(alone))
	}
	for _, cond := range ProdConditions {
		f := greenFacts()
		alone[cond](&f)
		d := DecideProd(f)
		if d.WouldRelease || len(d.Failing) != 1 || d.Failing[0] != cond {
			t.Errorf("%s alone: would=%v failing=%v", cond, d.WouldRelease, d.Failing)
		}
	}
}

type shadowHarness struct {
	loop     *ProdShadowLoop
	facts    ProdFacts
	gathered int
	batches  int
	records  []string // result + " " + detail
}

func newShadowHarness(mode string) *shadowHarness {
	h := &shadowHarness{facts: greenFacts()}
	h.loop = &ProdShadowLoop{
		Mode:   ProdMode(mode),
		Gather: func(context.Context) ProdFacts { h.gathered++; return h.facts },
		Batch: func(_ context.Context, from, to string) ProdBatch {
			h.batches++
			return ProdBatch{Count: 3, Known: true, IDs: []string{"22222222", "33333333", "44444444"}, Work: []string{"LANE-ci-w1", "TD-9"}}
		},
		Record: func(result, detail string) { h.records = append(h.records, result+" "+detail) },
	}
	return h
}

// Origin: see the top of the file. Guards the record: one per decision, not
// one per pass (the loop passes every 30 s to 5 min; the morning's reading
// wants the moments the answer changed), a second record when the decision
// changes, and the detail the comparison reads (would_release, from, to,
// commits, commit_ids, failing, reasons, at).
func TestProdShadowRecordsADecisionOnceAndAgainWhenItChanges(t *testing.T) {
	h := newShadowHarness("shadow")
	ctx := context.Background()
	if d, recorded := h.loop.Pass(ctx); !recorded || !d.WouldRelease {
		t.Fatalf("first pass: recorded=%v %+v", recorded, d)
	}
	// The same decision a pass later, the clock 30 s on: nothing is written.
	h.facts.Now = h.facts.Now.Add(30 * time.Second)
	if _, recorded := h.loop.Pass(ctx); recorded || len(h.records) != 1 || h.batches != 1 {
		t.Fatalf("same decision: recorded=%v records=%d batch reads=%d", recorded, len(h.records), h.batches)
	}
	if !strings.HasPrefix(h.records[0], "would_release ") {
		t.Fatalf("record = %s", h.records[0])
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(strings.TrimPrefix(h.records[0], "would_release ")), &rec); err != nil {
		t.Fatal(err)
	}
	if rec["would_release"] != true || rec["from"] != shadowP || rec["to"] != shadowC || rec["commits"] != float64(3) ||
		len(rec["commit_ids"].([]any)) != 3 || len(rec["failing"].([]any)) != 0 || rec["at"] != "2026-11-16T23:45:00Z" || rec["reasons"] == nil {
		t.Fatalf("detail = %s", h.records[0])
	}
	// Someone takes the window: the decision changed, a second record.
	h.facts.WindowHeld = "comm (bifrost-platform)"
	if d, recorded := h.loop.Pass(ctx); !recorded || d.WouldRelease || len(h.records) != 2 || !strings.HasPrefix(h.records[1], "wait ") ||
		!strings.Contains(h.records[1], `"failing":["window_free"]`) {
		t.Fatalf("changed decision: recorded=%v records=%v", recorded, h.records)
	}
	// And back: a third.
	h.facts.WindowHeld = ""
	if _, recorded := h.loop.Pass(ctx); !recorded || len(h.records) != 3 {
		t.Fatalf("back to release: %d records", len(h.records))
	}
}

// Origin: see the top of the file. Guards "a read that fails is a decision":
// it is recorded once as unknown with what could not be read, however the
// error's words change from pass to pass, and it is never a release. The
// commits are "unknown" when the batch could not be counted.
func TestProdShadowRecordsAnUnreadableFactOnceAsUnknown(t *testing.T) {
	h := newShadowHarness("shadow")
	h.loop.Batch = func(context.Context, string, string) ProdBatch { return ProdBatch{Note: "gitea: HTTP 502"} }
	h.facts.PolicyErr = "the policy's evaluation did not finish: context deadline exceeded"
	d, recorded := h.loop.Pass(context.Background())
	if !recorded || d.WouldRelease || !strings.HasPrefix(h.records[0], "unknown ") ||
		!strings.Contains(h.records[0], `"commits":"unknown"`) || !strings.Contains(h.records[0], "gitea: HTTP 502") ||
		!strings.Contains(h.records[0], "unknown: the policy's evaluation did not finish") {
		t.Fatalf("recorded=%v %v", recorded, h.records)
	}
	h.facts.PolicyErr = "the policy's evaluation did not finish: context canceled"
	if _, recorded := h.loop.Pass(context.Background()); recorded || len(h.records) != 1 {
		t.Fatalf("the same unknown in other words was recorded again: %v", h.records)
	}
}

// Origin: see the top of the file. Guards the switch: off, unset, a typing
// mistake and "on" (which is not written) all read nothing and record
// nothing. Positive control: the same world with "shadow" gathers and
// records, so the silence above is the switch and not a harness that cannot
// record.
func TestProdShadowOffReadsAndRecordsNothing(t *testing.T) {
	for _, value := range []string{"", "off", "on", "ON", "true", "stg", "shadow-ish", "prod"} {
		h := newShadowHarness(value)
		if _, recorded := h.loop.Pass(context.Background()); recorded || h.gathered != 0 || h.batches != 0 || len(h.records) != 0 {
			t.Errorf("PROD_HEAD_RELEASE=%q: gathered %d, batch reads %d, records %v", value, h.gathered, h.batches, h.records)
		}
		if ProdMode(value) != ProdOff {
			t.Errorf("ProdMode(%q) = %s", value, ProdMode(value))
		}
	}
	for _, value := range []string{"shadow", " Shadow "} {
		h := newShadowHarness(value)
		if _, recorded := h.loop.Pass(context.Background()); !recorded || h.gathered != 1 || len(h.records) != 1 {
			t.Errorf("positive control PROD_HEAD_RELEASE=%q: gathered %d records %v", value, h.gathered, h.records)
		}
	}
	var none *ProdShadowLoop
	if _, recorded := none.Pass(context.Background()); recorded {
		t.Error("a nil shadow recorded")
	}
}

// Origin: see the top of the file. Guards the two numbers the coordinator is
// asked to settle (soak 10 min, gap 20 min): they are read from the
// environment and a value that is not a whole number of seconds is the
// default, not zero (a zero soak would say "would release" the moment STG
// has a commit).
func TestProdShadowSeconds(t *testing.T) {
	for value, want := range map[string]time.Duration{
		"": DefaultProdSoak, "900": 900 * time.Second, " 60 ": time.Minute, "0": 0,
		"-5": DefaultProdSoak, "10m": DefaultProdSoak, "1e3": DefaultProdSoak, "99999999999999999999": DefaultProdSoak,
	} {
		if got := Seconds(value, DefaultProdSoak); got != want {
			t.Errorf("Seconds(%q) = %s, want %s", value, got, want)
		}
	}
}

// Origin: see the top of the file; and the design's "不起运行、不占窗口、不建卡".
// Guards, by the shape of the code, that the shadow cannot act:
//   - ProdFacts is data all the way down: no function, channel or interface
//     through which a decision could reach anything;
//   - ProdShadowLoop's only functions are Gather, Batch and Record;
//   - prod_shadow.go names nothing that starts a run, holds the window or
//     makes an approval, and not World (whose Start is the loop's one act).
//
// Positive control: the same scan of headrelease.go finds World and Start.
func TestProdShadowCannotAct(t *testing.T) {
	var walk func(reflect.Type, string)
	walk = func(typ reflect.Type, path string) {
		switch typ.Kind() {
		case reflect.Func, reflect.Chan, reflect.Interface, reflect.Pointer, reflect.UnsafePointer:
			t.Errorf("%s is a %s: the facts are data only", path, typ.Kind())
		case reflect.Struct:
			if typ == reflect.TypeOf(time.Time{}) {
				return
			}
			for i := 0; i < typ.NumField(); i++ {
				walk(typ.Field(i).Type, path+"."+typ.Field(i).Name)
			}
		case reflect.Slice, reflect.Array, reflect.Map:
			walk(typ.Elem(), path+"[]")
		}
	}
	walk(reflect.TypeOf(ProdFacts{}), "ProdFacts")
	walk(reflect.TypeOf(ProdDecision{}), "ProdDecision")

	var funcs []string
	loop := reflect.TypeOf(ProdShadowLoop{})
	for i := 0; i < loop.NumField(); i++ {
		if loop.Field(i).Type.Kind() == reflect.Func {
			funcs = append(funcs, loop.Field(i).Name)
		}
	}
	if strings.Join(funcs, ",") != "Gather,Batch,Record" {
		t.Errorf("ProdShadowLoop's functions are %v; a new one needs a reason it cannot act", funcs)
	}

	forbidden := []string{"World", "Start", "Step", "StartDeliveryIfFree", "StartPipelineRun", "start_pipeline_run",
		"release-window", "HoldReleaseWindow", "PutReleaseWindow", "approvals", "Create", "HandleCreate", "http", "exec"}
	if hits := namesIn(t, "prod_shadow.go", forbidden); len(hits) > 0 {
		t.Errorf("prod_shadow.go names %v: the shadow reads and records, nothing else", hits)
	}
	if hits := namesIn(t, "headrelease.go", forbidden); !contains(hits, "World") || !contains(hits, "Start") {
		t.Errorf("positive control: the scan of headrelease.go found only %v", hits)
	}
}

// namesIn parses a file of this package and returns which of the names occur
// in it as an identifier, an import path or inside a string literal.
// Comments are not read: they may say what the code does not do.
func namesIn(t *testing.T, file string, names []string) []string {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	ast.Inspect(parsed, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.Ident:
			if contains(names, x.Name) {
				found[x.Name] = true
			}
		case *ast.BasicLit:
			if x.Kind != token.STRING {
				return true
			}
			text, _ := strconv.Unquote(x.Value)
			for _, name := range names {
				if strings.Contains(text, name) {
					found[name] = true
				}
			}
		}
		return true
	})
	var out []string
	for name := range found {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func sameSet(a, b []string) bool {
	a, b = append([]string{}, a...), append([]string{}, b...)
	sort.Strings(a)
	sort.Strings(b)
	return strings.Join(a, ",") == strings.Join(b, ",")
}
