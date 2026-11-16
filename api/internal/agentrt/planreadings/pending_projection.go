package planreadings

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

type PendingNeed struct {
	Event                                      int64
	Stream, Root, Work, Reason, Question, Kind string
	At                                         time.Time
	Payload                                    any
}
type NeedLabels struct{ Apply, Refuse, RecorderApply, RecorderRefuse, ImportApply, ImportRefuse string }

func ProjectPendingNeeds(list []PendingNeed, limit int, now time.Time, labels NeedLabels, resolveBlocks func(any) (any, error), missions func(map[string]map[string]any) ([]WaitingCard, bool, error)) (map[string]any, error) {
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
		if it, seen := byQuestion[p.Question]; seen {
			it["also_refs"] = append(it["also_refs"].([]string), fmt.Sprint(p.Event))
			continue
		}
		root := p.Root
		title, options := "Plan change to "+root+": "+firstLine(p.Reason), []string{labels.Apply, labels.Refuse}
		after := "Apply: the controller lands the change on its next pass (about 15 seconds). Do not apply: the plan stays as it is."
		var blocks any
		if p.Kind == "recorder" {
			// A recorder log is history: no plan or mission waits on it.
			work := p.Work
			title, options = "Import recorder log "+work+": "+firstLine(p.Reason), []string{labels.RecorderApply, labels.RecorderRefuse}
			after = "Import: the controller imports the log on its next pass (about 15 seconds). Do not import: nothing is written."
			blocks = map[string]any{"unknown": "a recorder log is history; no plan or mission waits on it"}
		} else if p.Kind == "import" {
			// An import adds a plan no mission's work sits on yet.
			title, options = "Import plan "+root+": "+firstLine(p.Reason), []string{labels.ImportApply, labels.ImportRefuse}
			after = "Import: the controller imports the plan on its next pass (about 15 seconds). Do not import: nothing is written."
			blocks = map[string]any{"unknown": "an import adds a plan; no mission's work sits on it yet"}
		} else {
			var berr error
			if blocks, berr = resolveBlocks(p.Payload); berr != nil {
				return nil, berr
			}
		}
		it := map[string]any{"id": "question:" + p.Question, "kind": "plan_change", "ref": fmt.Sprint(p.Event), "also_refs": []string{},
			"title": title, "since": p.At.UTC().Format(time.RFC3339Nano),
			"waiting_min": int64(now.Sub(p.At) / time.Minute),
			"action":      map[string]any{"where": "console", "question_id": p.Question, "options": options},
			"can_act_now": nil, "can_act_now_reason": "the card's state is the question store's: open there, it can be answered now",
			"deadline": nil, "blocks": blocks, "after": after}
		byQuestion[p.Question] = it
		entries = append(entries, entry{p.At, p.Question, it})
	}
	cards, more, err := missions(byQuestion)
	if err != nil {
		return nil, err
	}
	for _, c := range cards {
		entries = append(entries, entry{c.At, c.Question, c.Item})
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
func firstLine(s string) string { line, _, _ := strings.Cut(s, "\n"); return line }
