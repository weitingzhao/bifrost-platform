package evalreadings

import (
	"context"
	"sort"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actionrecord"
)

// The action readings: what the lines did on their own, by the law each
// action rested on, and whether it said what undoes it. They answer the
// Owner's "after a relaxation, what happened" (2026-11-20, via the
// coordinator): the actions a signed rule let through without a card, how
// they ended, and how many carried a rollback. Whether a rollback was used is
// not recorded yet (no field links an undo to the action it undoes); that
// part is a proposal (shared notes metrics/relaxation-before-after-2026-11-20.md).

// actionLister reads action records newest first (actionrecord.List).
type actionLister func(ctx context.Context, f actionrecord.Filter) ([]actionrecord.Record, bool, error)

// maxActionPages bounds a window's read: 20 pages of 1000.
const maxActionPages = 20

// windowActions reads every record of the window, page by page from the
// newest; a record at a page's boundary instant is read again and kept once.
// cut is true when the window has more than maxActionPages pages.
func windowActions(ctx context.Context, list actionLister, w Window) (recs []actionrecord.Record, cut bool, err error) {
	seen := map[int64]bool{}
	until := w.To
	for page := 0; page < maxActionPages; page++ {
		got, more, err := list(ctx, actionrecord.Filter{Since: w.From, Until: until, Limit: 1000})
		if err != nil {
			return nil, false, err
		}
		added := 0
		oldest := until
		for _, r := range got {
			if !seen[r.ID] {
				seen[r.ID] = true
				recs = append(recs, r)
				added++
			}
			if r.At.Before(oldest) {
				oldest = r.At
			}
		}
		if !more || added == 0 {
			return recs, false, nil
		}
		until = oldest.Add(time.Microsecond)
	}
	return recs, true, nil
}

// agentActionReadings: agent_actions per law and outcome (count), and
// action_rollback_declared_ratio per law (records whose regret_state is
// declared or ready, over all records of that law).
func agentActionReadings(ctx context.Context, list actionLister, w Window) []Reading {
	dc := def("agentrt.action_record (actionrecord.List)", "records in the window, by law and outcome")
	dr := def("agentrt.action_record (actionrecord.List)", "records with regret_state declared or ready over all records, by law")
	if list == nil {
		return []Reading{
			finish(Reading{Name: "agent_actions", Unit: "count", Window: w, Definition: dc, Note: "the action record is not connected on this platform"}, false),
			finish(Reading{Name: "action_rollback_declared_ratio", Unit: "ratio", Window: w, Definition: dr, Note: "the action record is not connected on this platform"}, false),
		}
	}
	recs, cut, err := windowActions(ctx, list, w)
	if err != nil {
		note := "action record read failed: " + err.Error()
		return []Reading{
			finish(Reading{Name: "agent_actions", Unit: "count", Window: w, Definition: dc, Note: note}, false),
			finish(Reading{Name: "action_rollback_declared_ratio", Unit: "ratio", Window: w, Definition: dr, Note: note}, false),
		}
	}
	type key struct{ law, outcome string }
	counts := map[key]int{}
	total := map[string]int{}
	declared := map[string]int{}
	for _, r := range recs {
		counts[key{r.Law, r.Outcome}]++
		total[r.Law]++
		if r.RegretState == actionrecord.RegretDeclared || r.RegretState == actionrecord.RegretReady {
			declared[r.Law]++
		}
	}
	var out []Reading
	keys := make([]key, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].law != keys[j].law {
			return keys[i].law < keys[j].law
		}
		return keys[i].outcome < keys[j].outcome
	})
	for _, k := range keys {
		out = append(out, finish(Reading{Name: "agent_actions", Labels: map[string]string{"law": k.law, "outcome": k.outcome}, Value: count(counts[k]), Unit: "count", Samples: counts[k], Window: w, Definition: dc}, false))
	}
	laws := make([]string, 0, len(total))
	for l := range total {
		laws = append(laws, l)
	}
	sort.Strings(laws)
	for _, l := range laws {
		out = append(out, finish(Reading{Name: "action_rollback_declared_ratio", Labels: map[string]string{"law": l}, Value: ratio(declared[l], total[l]), Unit: "ratio", Samples: total[l], Window: w, Definition: dr}, false))
	}
	if len(out) == 0 {
		r := finish(Reading{Name: "agent_actions", Unit: "count", Window: w, Definition: dc}, false)
		measuredZero(&r)
		out = append(out, r)
	}
	if cut {
		for i := range out {
			out[i].Status, out[i].Value = StatusTooFew, nil
			out[i].Note = "more records in the window than the read takes"
		}
	}
	return out
}
