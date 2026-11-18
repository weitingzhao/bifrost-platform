package nextgen

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
)

// The register behind "Problem channels" on the Bus page (the Owner,
// 2026-11-18: first make sure problems can be seen). A row that names a channel
// the table does not have, a topic nobody registered or no receiver would make
// the count claim something the system does not do.
func TestProblemChannelsNameRealThings(t *testing.T) {
	ps, err := Problems()
	if err != nil {
		t.Fatal(err)
	}
	chs, err := Channels()
	if err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	for _, c := range chs {
		known[c.ID] = true
	}
	tenants, err := registry.Load()
	if err != nil {
		t.Fatal(err)
	}
	topics := map[string]bool{}
	for _, tn := range tenants {
		for _, c := range tn.Channels {
			topics[c.Type] = true
		}
	}
	src, err := os.ReadFile("../alerts/problems.go")
	if err != nil {
		t.Fatal(err)
	}
	raised := string(src)
	seen := map[string]bool{}
	for _, p := range ps {
		if seen[p.ID] {
			t.Errorf("%s: listed twice", p.ID)
		}
		seen[p.ID] = true
		if !known[p.ID] && (!strings.HasPrefix(p.ID, "Y-") || p.What == "") {
			t.Errorf("%s: not a row of channels.yaml, and not a Y-n row that says what it is", p.ID)
		}
		switch p.State {
		case ProblemConnected:
			if p.To == "" {
				t.Errorf("%s: connected with no receiver", p.ID)
			}
		case ProblemTodo, ProblemDeleted, ProblemPending:
		default:
			t.Errorf("%s: state %q", p.ID, p.State)
		}
		if p.Owner == "" {
			t.Errorf("%s: no owner", p.ID)
		}
		if p.Drill != "" && (p.Drill != "drill:"+p.ID || p.State != ProblemConnected) {
			t.Errorf("%s: drill %q must be drill:<id> and only on a connected row", p.ID, p.Drill)
		}
		for _, e := range p.Events {
			// 2026-11-18, the Owner's "first make problems visible": a drill must
			// never count as a real run, so no row names a drill stream as events.
			if strings.Contains(e, "drill:") {
				t.Errorf("%s: event %q names a drill stream; a drill goes in drill:", p.ID, e)
			}
			if name, ok := strings.CutPrefix(e, "alert:"); ok {
				if !strings.Contains(raised, `"`+name+`"`) {
					t.Errorf("%s: %q is not an alert the platform raises", p.ID, e)
				}
				continue
			}
			if strings.HasPrefix(e, "stream:") {
				continue
			}
			if rest, ok := strings.CutPrefix(e, "source:"); ok {
				if typ, source, _ := strings.Cut(rest, ":"); !topics[typ] || source == "" {
					t.Errorf("%s: event %q names no registered topic and source", p.ID, e)
				}
				continue
			}
			if typ, ok := strings.CutPrefix(e, "topic:"); !ok || !topics[typ] {
				t.Errorf("%s: event %q is not a registered topic", p.ID, e)
			}
		}
	}
}

// Same origin: connected is not proven until the event was really published,
// and a problem topic that was merely quiet for a while still counts.
func TestProblemReadingCountsOnlyWhatRan(t *testing.T) {
	ev := map[string]string{"topic:a": EvidenceRunning, "topic:quiet": EvidenceStale, "topic:never": EvidenceNone, "topic:unread": EvidenceUnmeasured}
	got := SummarizeProblems([]ProblemChannel{
		{ID: "A", State: ProblemConnected, Events: []string{"topic:a", "topic:quiet"}},
		{ID: "B", State: ProblemConnected, Events: []string{"topic:a", "topic:never"}},
		{ID: "C", State: ProblemConnected},
		{ID: "D", State: ProblemConnected, Events: []string{"topic:unread"}},
		{ID: "E", State: ProblemTodo},
		{ID: "F", State: ProblemPending},
		{ID: "G", State: ProblemDeleted},
	}, func(id string) Evidence { return Evidence{State: ev[id]} })
	want := ProblemProgress{Live: 6, Connected: 4, Proven: 1, Unmeasured: 1, Deleted: 1, Pending: 1, Todo: 1,
		Unproven: []string{"B", "C"}, Open: []string{"E", "F"}, DrilledOnly: []string{}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	done := SummarizeProblems([]ProblemChannel{{ID: "A", State: ProblemConnected, Events: []string{"topic:a"}}, {ID: "G", State: ProblemDeleted}},
		func(id string) Evidence { return Evidence{State: ev[id]} })
	if !done.Done {
		t.Fatalf("every channel proven or deleted is done: %+v", done)
	}
}

// 2026-11-18, the Owner's "first make problems visible": 12 channels never fire
// in normal life and stayed "unproven". A drill proves the event can be written;
// it must show apart from a real run, and a real run must win over it.
func TestProblemReadingSeparatesDrills(t *testing.T) {
	ev := map[string]string{"topic:real": EvidenceRunning, "topic:never": EvidenceNone, "drill:A": EvidenceRunning, "drill:B": EvidenceNone,
		"drill:C": EvidenceStale, "drill:D": EvidenceUnmeasured}
	read := func(id string) Evidence { return Evidence{State: ev[id]} }
	got := SummarizeProblems([]ProblemChannel{
		{ID: "A", State: ProblemConnected, Events: []string{"topic:never"}, Drill: "drill:A"},
		{ID: "B", State: ProblemConnected, Events: []string{"topic:never"}, Drill: "drill:B"},
		{ID: "C", State: ProblemConnected, Events: []string{"topic:real"}, Drill: "drill:C"},
		{ID: "D", State: ProblemConnected, Events: []string{"topic:never"}, Drill: "drill:D"},
		{ID: "E", State: ProblemConnected, Events: []string{"topic:never"}},
	}, read)
	want := ProblemProgress{Live: 5, Connected: 5, Proven: 1, Drilled: 1, DrilledOnly: []string{"A"}, Unmeasured: 1,
		Unproven: []string{"B", "E"}, Open: []string{}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if got.Done {
		t.Fatal("a channel with neither a run nor a drill is not done")
	}
	done := SummarizeProblems([]ProblemChannel{
		{ID: "A", State: ProblemConnected, Events: []string{"topic:never"}, Drill: "drill:A"},
		{ID: "C", State: ProblemConnected, Events: []string{"topic:real"}, Drill: "drill:C"},
	}, read)
	if !done.Done || done.Proven != 1 || done.Drilled != 1 {
		t.Fatalf("every channel proven or drilled is done: %+v", done)
	}
}
