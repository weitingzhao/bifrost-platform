package nextgen

import (
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
		for _, e := range p.Events {
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
		Unproven: []string{"B", "C"}, Open: []string{"E", "F"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	done := SummarizeProblems([]ProblemChannel{{ID: "A", State: ProblemConnected, Events: []string{"topic:a"}}, {ID: "G", State: ProblemDeleted}},
		func(id string) Evidence { return Evidence{State: ev[id]} })
	if !done.Done {
		t.Fatalf("every channel proven or deleted is done: %+v", done)
	}
}
