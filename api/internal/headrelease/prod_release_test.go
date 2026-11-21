package headrelease

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// Origin: 2026-11-21, CI line. The shadow judged "would release e6f7fad5" at
// 07:52Z and no seat released it for nine hours; the user's goal is PROD at
// least once a day on a schedule. These guard mode on: only mode on calls
// Release, only for a "would release" read twice the same, and every outcome
// is recorded.

type releaseCall struct {
	revision string
	params   map[string]string
}

func onHarness(mode string) (*shadowHarness, *[]releaseCall) {
	h := newShadowHarness(mode)
	h.facts.Beside = map[string]string{"uiRevision": "3333333333333333333333333333333333333333"}
	var calls []releaseCall
	h.loop.Release = func(_ context.Context, revision string, params map[string]string) (string, string, error) {
		calls = append(calls, releaseCall{revision, params})
		return "bifrost-deliver-platform-prod-1", "", nil
	}
	return h, &calls
}

func TestProdModeOnReleasesAWouldReleaseReadTwice(t *testing.T) {
	h, calls := onHarness("on")
	d, recorded := h.loop.Pass(context.Background())
	if !recorded || !d.WouldRelease || len(*calls) != 1 || h.gathered != 2 {
		t.Fatalf("recorded=%v would=%v calls=%v gathered=%d", recorded, d.WouldRelease, *calls, h.gathered)
	}
	if c := (*calls)[0]; c.revision != shadowC || c.params["uiRevision"] != "3333333333333333333333333333333333333333" {
		t.Fatalf("released %+v", c)
	}
	rec := h.records[0]
	if !strings.HasPrefix(rec, ProdReleased+" ") || !strings.Contains(rec, `"run":"bifrost-deliver-platform-prod-1"`) {
		t.Fatalf("record %s", rec)
	}
}

func TestProdModeShadowNeverReleases(t *testing.T) {
	// Positive control is the test above: the same harness in mode on releases.
	h, calls := onHarness("shadow")
	if d, recorded := h.loop.Pass(context.Background()); !recorded || !d.WouldRelease || len(*calls) != 0 {
		t.Fatalf("shadow: recorded=%v would=%v calls=%v", recorded, d.WouldRelease, *calls)
	}
	if !strings.HasPrefix(h.records[0], "would_release ") {
		t.Fatalf("shadow record %s", h.records[0])
	}
}

func TestProdModeOnReleasesNothingThatWouldNot(t *testing.T) {
	for name, change := range map[string]func(f *ProdFacts){
		"window held":    func(f *ProdFacts) { f.WindowHeld = "someone" },
		"before 08:00":   func(f *ProdFacts) { f.HourNow, f.HoursFrom, f.HoursTo = 7, 8, 22 },
		"at 22:00":       func(f *ProdFacts) { f.HourNow, f.HoursFrom, f.HoursTo = 22, 8, 22 },
		"zone not known": func(f *ProdFacts) { f.HoursUnknown = true },
		"no full CI":     func(f *ProdFacts) { f.CI = "" },
	} {
		h, calls := onHarness("on")
		change(&h.facts)
		if d, _ := h.loop.Pass(context.Background()); d.WouldRelease || len(*calls) != 0 {
			t.Errorf("%s: would=%v calls=%v", name, d.WouldRelease, *calls)
		}
	}
	// Inside the hours it releases (positive control of the hours cases).
	h, calls := onHarness("on")
	h.facts.HourNow, h.facts.HoursFrom, h.facts.HoursTo = 8, 8, 22
	if h.loop.Pass(context.Background()); len(*calls) != 1 {
		t.Errorf("08:00 inside 8-22: calls %v", *calls)
	}
}

func TestProdModeOnWaitsWhenTheSecondReadingDiffers(t *testing.T) {
	h, calls := onHarness("on")
	n := 0
	h.loop.Gather = func(context.Context) ProdFacts {
		n++
		f := h.facts
		if n == 2 {
			f.WindowHeld = "a seat that started meanwhile"
		}
		return f
	}
	h.loop.Pass(context.Background())
	if len(*calls) != 0 || !strings.HasPrefix(h.records[0], ProdWaiting+" ") || !strings.Contains(h.records[0], "second reading differs") {
		t.Fatalf("calls %v record %s", *calls, h.records[0])
	}
}

func TestProdModeOnRecordsAWaitAndAnError(t *testing.T) {
	h, _ := onHarness("on")
	h.loop.Release = func(context.Context, string, map[string]string) (string, string, error) {
		return "", "not started: held by someone", nil
	}
	h.loop.Pass(context.Background())
	if !strings.HasPrefix(h.records[0], ProdWaiting+" ") || !strings.Contains(h.records[0], "held by someone") {
		t.Fatalf("wait record %s", h.records[0])
	}
	h, _ = onHarness("on")
	h.loop.Release = func(context.Context, string, map[string]string) (string, string, error) {
		return "", "", errors.New("the cluster said no")
	}
	h.loop.Pass(context.Background())
	if !strings.HasPrefix(h.records[0], ProdError+" ") || !strings.Contains(h.records[0], "the cluster said no") {
		t.Fatalf("error record %s", h.records[0])
	}
	h, _ = onHarness("on")
	h.loop.Release = nil
	h.loop.Pass(context.Background())
	if !strings.HasPrefix(h.records[0], ProdError+" ") {
		t.Fatalf("no hand record %s", h.records[0])
	}
}

func TestProdHours(t *testing.T) {
	for value, want := range map[string][2]int{
		"": {8, 22}, "8-22": {8, 22}, " 9 - 17 ": {9, 17}, "0-24": {0, 24},
		"22-8": {8, 22}, "8-25": {8, 22}, "x-9": {8, 22}, "8": {8, 22}, "-1-5": {8, 22},
	} {
		if f, to := ProdHours(value); [2]int{f, to} != want {
			t.Errorf("ProdHours(%q) = %d-%d, want %v", value, f, to, want)
		}
	}
	// The hours are read in the zone; without one, the hours are unknown.
	reads := ProdReads{Now: func() time.Time { return time.Date(2026, 11, 21, 13, 30, 0, 0, time.UTC) }, HoursFrom: 8, HoursTo: 22}
	if f := reads.Gather(context.Background(), Deliverable{}, Deliverable{}, 0, 0); !f.HoursUnknown {
		t.Errorf("no zone: %+v", f)
	}
	chicago, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Skip("no zone data")
	}
	reads.Zone = chicago
	if f := reads.Gather(context.Background(), Deliverable{}, Deliverable{}, 0, 0); f.HoursUnknown || f.HourNow != 7 {
		t.Errorf("13:30Z in Chicago (CST): hour %d unknown %v", f.HourNow, f.HoursUnknown)
	}
}
