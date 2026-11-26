package evalreadings

import (
	"context"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actionrecord"
)

// 决定 2026-11-20: after a relaxation, count what
// the lines did on their own, by law, and how many said what undoes it. PROD
// that day: 948 records, 145 under "signed release policy", 48 with a
// declared rollback. The fake pages by Limit like actionrecord.List and puts
// two records at one instant across a page boundary: each is counted once.
func TestAgentActionsByLawAcrossPages(t *testing.T) {
	w := ClosedWindow(time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC), 7)
	base := w.To.Add(-time.Hour)
	var all []actionrecord.Record
	for i := 0; i < 1500; i++ {
		r := actionrecord.Record{ID: int64(2000 - i), At: base.Add(-time.Duration(i/2) * time.Second), Law: "action catalog: tier B", Outcome: "done", RegretState: "undeclared"}
		if i%3 == 0 {
			r.Law = "signed release policy"
		}
		if i%10 == 0 {
			r.RegretState = "declared"
		}
		if i == 7 {
			r.Outcome = "failed"
		}
		all = append(all, r)
	}
	list := func(_ context.Context, f actionrecord.Filter) ([]actionrecord.Record, bool, error) {
		var got []actionrecord.Record
		for _, r := range all {
			if !r.At.Before(f.Until) || r.At.Before(f.Since) {
				continue
			}
			got = append(got, r)
			if len(got) > f.Limit {
				return got[:f.Limit], true, nil
			}
		}
		return got, false, nil
	}
	rs := agentActionReadings(context.Background(), list, w)
	byKey := map[string]Reading{}
	for _, r := range rs {
		byKey[r.Name+"|"+r.Labels["law"]+"|"+r.Labels["outcome"]] = r
	}
	if r := byKey["agent_actions|signed release policy|done"]; r.Status != StatusOK || *r.Value != 500 {
		t.Fatalf("signed done: %+v", r)
	}
	if r := byKey["agent_actions|action catalog: tier B|failed"]; *r.Value != 1 {
		t.Fatalf("tier B failed: %+v", r)
	}
	if r := byKey["agent_actions|action catalog: tier B|done"]; *r.Value != 999 {
		t.Fatalf("tier B done: %+v", r)
	}
	// i%10==0 and i%3==0 → i%30==0: 50 of the 500 signed records.
	if r := byKey["action_rollback_declared_ratio|signed release policy|"]; *r.Value != 0.1 || r.Samples != 500 {
		t.Fatalf("signed rollback ratio: %+v", r)
	}
	if rs := agentActionReadings(context.Background(), nil, w); rs[0].Status != StatusUnmeasured {
		t.Fatalf("no record: %+v", rs[0])
	}
	empty := func(context.Context, actionrecord.Filter) ([]actionrecord.Record, bool, error) {
		return nil, false, nil
	}
	if rs := agentActionReadings(context.Background(), empty, w); rs[0].Status != StatusOK || *rs[0].Value != 0 {
		t.Fatalf("no action in the window is 0: %+v", rs[0])
	}
}
