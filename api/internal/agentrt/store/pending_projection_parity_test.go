package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/externalwork"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planreadings"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
)

// missionCard is a mission's card as the frozen projection below takes one
// from its caller: the three fields of the plan code's own type of this name
// (planstore's progressneeds.go), which this file used while it shared its
// package.
type missionCard struct {
	at       time.Time
	question string
	it       map[string]any
}

type frozenPending struct {
	event                    int64
	stream, reason, question string
	at                       time.Time
	changes                  []planstore.PlanChange
}

func frozenPendingProjection(list []frozenPending, limit int, now time.Time, resolveBlocks func([]planstore.PlanChange) (any, error), missions func(map[string]map[string]any) ([]missionCard, bool, error)) (map[string]any, error) {
	truncated := len(list) > limit
	if truncated {
		list = list[:limit]
	}
	// One card is one item: a second proposal naming a card already listed
	// adds its event to that item. Plan changes and missions share one
	// bound, oldest first (review 190 B4).
	type entry struct {
		at time.Time
		id string
		it map[string]any
	}
	var entries []entry
	byQuestion := map[string]map[string]any{}
	for _, p := range list {
		if it, seen := byQuestion[p.question]; seen {
			it["also_refs"] = append(it["also_refs"].([]string), fmt.Sprint(p.event))
			continue
		}
		root := planstore.StreamRoot(p.stream)
		title, options := "Plan change to "+root+": "+firstLine(p.reason), []string{planstore.CardApply, planstore.CardRefuse}
		after := "Apply: the controller lands the change on its next pass (about 15 seconds). Do not apply: the plan stays as it is."
		var blocks any
		if _, isRecorder := recorderImportOf(p.changes); isRecorder {
			// A recorder log is history: no plan or mission waits on it.
			work := strings.TrimPrefix(p.stream, externalwork.RecorderStream(""))
			title, options = "Import recorder log "+work+": "+firstLine(p.reason), []string{RecorderApply, RecorderRefuse}
			after = "Import: the controller imports the log on its next pass (about 15 seconds). Do not import: nothing is written."
			blocks = map[string]any{"unknown": "a recorder log is history; no plan or mission waits on it"}
		} else if _, isImport := planstore.ImportOf(p.changes); isImport {
			// An import adds a plan no mission's work sits on yet.
			title, options = "Import plan "+root+": "+firstLine(p.reason), []string{planstore.ImportApply, planstore.ImportRefuse}
			after = "Import: the controller imports the plan on its next pass (about 15 seconds). Do not import: nothing is written."
			blocks = map[string]any{"unknown": "an import adds a plan; no mission's work sits on it yet"}
		} else {
			var berr error
			if blocks, berr = resolveBlocks(p.changes); berr != nil {
				return nil, berr
			}
		}
		it := map[string]any{"id": "question:" + p.question, "kind": "plan_change", "ref": fmt.Sprint(p.event), "also_refs": []string{},
			"title": title, "since": p.at.UTC().Format(time.RFC3339Nano),
			"waiting_min": int64(now.Sub(p.at) / time.Minute),
			"action":      map[string]any{"where": "console", "question_id": p.question, "options": options},
			"can_act_now": nil, "can_act_now_reason": "the card's state is the question store's: open there, it can be answered now",
			"deadline": nil, "blocks": blocks, "after": after}
		byQuestion[p.question] = it
		entries = append(entries, entry{p.at, p.question, it})
	}
	cards, more, err := missions(byQuestion)
	if err != nil {
		return nil, err
	}
	for _, c := range cards {
		entries = append(entries, entry{c.at, c.question, c.it})
	}
	slices.SortFunc(entries, func(a, b entry) int {
		if c := a.at.Compare(b.at); c != 0 {
			return c
		}
		return strings.Compare(a.id, b.id)
	})
	truncated = truncated || more
	if len(entries) > limit {
		entries, truncated = entries[:limit], true
	}
	items := make([]any, 0, len(entries))
	for _, e := range entries {
		items = append(items, e.it)
	}
	return map[string]any{"count": len(items), "items": items, "truncated": truncated}, nil
}

func TestPendingProjectionFrozenParity(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	labels := planreadings.NeedLabels{Apply: planstore.CardApply, Refuse: planstore.CardRefuse, RecorderApply: RecorderApply, RecorderRefuse: RecorderRefuse, ImportApply: planstore.ImportApply, ImportRefuse: planstore.ImportRefuse}
	for count := 0; count < 12; count++ {
		for limit := 0; limit < 9; limit++ {
			for fail := 0; fail < 3; fail++ {
				var old []frozenPending
				var in []planreadings.PendingNeed
				for i := 0; i < count; i++ {
					p := frozenPending{event: int64(i), stream: planstore.PlanStream("p"), reason: "why\nline", question: fmt.Sprint(i % 4), at: now.Add(time.Duration(i%3-5) * time.Minute), changes: []planstore.PlanChange{{Op: "retitle", Node: fmt.Sprint(i)}}}
					kind := "change"
					if i%3 == 1 {
						p.changes = []planstore.PlanChange{{Op: planstore.OpImport, Artifact: "artifact"}}
						kind = "import"
					}
					if i%3 == 2 {
						p.changes = []planstore.PlanChange{{Op: OpRecorderImport, Artifact: "artifact"}}
						p.stream = externalwork.RecorderStream("W-1")
						kind = "recorder"
					}
					old = append(old, p)
					in = append(in, planreadings.PendingNeed{Event: p.event, Stream: p.stream, Root: planstore.StreamRoot(p.stream), Work: strings.TrimPrefix(p.stream, externalwork.RecorderStream("")), Reason: p.reason, Question: p.question, At: p.at, Kind: kind, Payload: p.changes})
				}
				var traceOld, traceNew []string
				errSentinel := errors.New("read failed")
				block := func(trace *[]string, c []planstore.PlanChange) (any, error) {
					*trace = append(*trace, "block:"+c[0].Node)
					if fail == 1 {
						return nil, errSentinel
					}
					return map[string]any{"missions": []string{c[0].Node}}, nil
				}
				mission := func(trace *[]string, m map[string]map[string]any) ([]missionCard, bool, error) {
					raw, _ := json.Marshal(m)
					*trace = append(*trace, string(raw))
					if fail == 2 {
						return nil, false, errSentinel
					}
					return []missionCard{{at: now.Add(-9 * time.Minute), question: "m", it: map[string]any{"id": "mission"}}}, count%2 == 0, nil
				}
				want, we := frozenPendingProjection(old, limit, now, func(c []planstore.PlanChange) (any, error) { return block(&traceOld, c) }, func(m map[string]map[string]any) ([]missionCard, bool, error) { return mission(&traceOld, m) })
				got, ge := planreadings.ProjectPendingNeeds(in, limit, now, labels, func(c any) (any, error) { return block(&traceNew, c.([]planstore.PlanChange)) }, func(m map[string]map[string]any) ([]planreadings.WaitingCard, bool, error) {
					cards, more, e := mission(&traceNew, m)
					var out []planreadings.WaitingCard
					for _, c := range cards {
						out = append(out, planreadings.WaitingCard{At: c.at, Question: c.question, Item: c.it})
					}
					return out, more, e
				})
				if !reflect.DeepEqual(want, got) || we != ge || !reflect.DeepEqual(traceOld, traceNew) {
					t.Fatalf("count=%d limit=%d fail=%d: old=%#v %v %v new=%#v %v %v", count, limit, fail, want, we, traceOld, got, ge, traceNew)
				}
			}
		}
	}
}
