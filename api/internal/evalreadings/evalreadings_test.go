package evalreadings

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// 2026-11-20, LAW-8: "the same window read on any day gives the same value".
// The window must be the same for two reads on different times of the same
// day, and must end at the Owner's midnight, including across the DST change
// (2026-11-01 in America/Chicago).
func TestClosedWindowIsTheSameAllDayAndEndsAtChicagoMidnight(t *testing.T) {
	a := ClosedWindow(time.Date(2026, 11, 20, 6, 30, 0, 0, time.UTC), 7) // 00:30 Chicago
	b := ClosedWindow(time.Date(2026, 11, 21, 5, 59, 0, 0, time.UTC), 7) // 23:59 Chicago, same day
	if a.From != b.From || a.To != b.To {
		t.Fatalf("two reads on the same Chicago day gave different windows: %v %v", a, b)
	}
	if got := a.To.In(Zone); got.Hour() != 0 || got.Day() != 20 {
		t.Fatalf("window does not end at Chicago midnight: %v", got)
	}
	// Across the DST end: 7 days back from 11-05 midnight is 10-29 midnight, 169 hours.
	c := ClosedWindow(time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC), 7)
	if h := c.To.Sub(c.From).Hours(); h != 169 {
		t.Fatalf("DST week is %v hours, want 169", h)
	}
}

// LAW-8 floor: fewer samples than the floor gives no percentile
// (the brief: "样本数低于口径写的下限：不给高分位，显示样本太少").
func TestFinishDropsAPercentileUnderTheFloorAndMarksNoRecordsUnmeasured(t *testing.T) {
	v := 12.0
	r := finish(Reading{Name: "x", Value: &v, Samples: 3}, true)
	if r.Status != StatusTooFew || r.Value != nil || r.Samples != 3 || r.MinSamples != MinSamples {
		t.Fatalf("under the floor: %+v", r)
	}
	r = finish(Reading{Name: "x", Value: &v, Samples: MinSamples}, true)
	if r.Status != StatusOK || r.Value == nil || *r.Value != 12 {
		t.Fatalf("at the floor: %+v", r)
	}
	r = finish(Reading{Name: "x"}, false)
	if r.Status != StatusUnmeasured || r.Value != nil || r.Provenance != "system" {
		t.Fatalf("no records: %+v", r)
	}
	// Positive control for measuredZero: a kept count with nothing in the window is 0, ok.
	r = finish(Reading{Name: "prod_deploys"}, false)
	measuredZero(&r)
	if r.Status != StatusOK || r.Value == nil || *r.Value != 0 {
		t.Fatalf("measured zero: %+v", r)
	}
}

// Prometheus keeps 10 days (2026-11-20 reading of the retention); a longer
// window must say unmeasured, not compute over a partial range.
func TestPromReadings(t *testing.T) {
	w := ClosedWindow(time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC), 7)
	var asked []string
	prom := func(_ context.Context, expr string) ([]Sample, error) {
		asked = append(asked, expr)
		if strings.HasPrefix(expr, "count_over_time") {
			return []Sample{{Labels: map[string]string{"scope": "bifrost"}, Value: 2016}, {Labels: map[string]string{"scope": "infra"}, Value: 1}}, nil
		}
		return []Sample{{Labels: map[string]string{"scope": "infra"}, Value: 0.263}, {Labels: map[string]string{"scope": "bifrost"}, Value: 0.565}}, nil
	}
	rs := promReadings(context.Background(), prom, w)
	if len(rs) != 2 || rs[0].Labels["scope"] != "bifrost" || rs[0].Status != StatusOK || *rs[0].Value != 0.565 || rs[0].Samples != 2016 {
		t.Fatalf("readings: %+v", rs)
	}
	// 2026-11-20, the first PROD read: 1 sample in a 7-day window (the record
	// started that morning) must not pass as the window's average. bifrost
	// above is its positive control (a full window, 7 days x 288 a day).
	if rs[1].Labels["scope"] != "infra" || rs[1].Status != StatusTooFew || rs[1].Value != nil || rs[1].Samples != 1 || rs[1].MinSamples != 1814 {
		t.Fatalf("a record covering part of the window must be too_few: %+v", rs[1])
	}
	if !strings.Contains(asked[0], "@ "+itoa(w.To.Unix())) {
		t.Fatalf("query is not pinned to the window end: %s", asked[0])
	}
	if rs := promReadings(context.Background(), prom, ClosedWindow(time.Now(), 14)); rs[0].Status != StatusUnmeasured {
		t.Fatalf("14 days must be unmeasured: %+v", rs[0])
	}
	if rs := promReadings(context.Background(), nil, w); rs[0].Status != StatusUnmeasured {
		t.Fatalf("no Prometheus must be unmeasured: %+v", rs[0])
	}
	failing := func(context.Context, string) ([]Sample, error) { return nil, errors.New("down") }
	if rs := promReadings(context.Background(), failing, w); rs[0].Status != StatusUnmeasured || !strings.Contains(rs[0].Note, "down") {
		t.Fatalf("failed query must be unmeasured with the reason: %+v", rs[0])
	}
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

// Without the ledger every ledger reading is still listed, as unmeasured with
// the reason (LAW-8: "读口里没有的数写没量"), and the days parameter is checked.
func TestHandlerWithoutLedger(t *testing.T) {
	h := Handler(nil, nil, nil, func() time.Time { return time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC) })
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/api/v1/readings/evaluation", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var body struct {
		Window   Window    `json:"window"`
		Readings []Reading `json:"readings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Window.Days != 7 || len(body.Readings) == 0 {
		t.Fatalf("body: %+v", body)
	}
	names := map[string]bool{}
	for _, r := range body.Readings {
		names[r.Name] = true
		if r.Status != StatusUnmeasured || r.Value != nil {
			t.Fatalf("without the ledger %s must be unmeasured: %+v", r.Name, r)
		}
	}
	for _, want := range []string{"ci_run_p50_seconds", "owner_actions", "prod_deploys", "main_to_prod_p50_seconds", "metric_coverage_ratio", "post_release_fixes", "plan_nodes_done"} {
		if !names[want] {
			t.Errorf("reading %s missing", want)
		}
	}
	rec = httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/api/v1/readings/evaluation?days=0", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("days=0 gave %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/api/v1/readings/evaluation?name=owner_actions", nil))
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if len(body.Readings) != 1 || body.Readings[0].Name != "owner_actions" {
		t.Fatalf("name filter: %+v", body.Readings)
	}
}

// 2026-11-20, the CI line: the queue is the pods' creation-to-start wait from
// kube-state-metrics, longest per run, median per run prefix; the PipelineRun's
// created_at -> started_at read 0 seconds and was not a queue. The fake gives
// what PROD gave that morning for one day (ci-platform 197 runs, median 4 s)
// and a series with no prefix, which is dropped.
func TestCIQueueFromPods(t *testing.T) {
	w := ClosedWindow(time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC), 7)
	var asked []string
	prom := func(_ context.Context, expr string) ([]Sample, error) {
		asked = append(asked, expr)
		if strings.HasPrefix(expr, "count") {
			return []Sample{{Labels: map[string]string{"prefix": "ci-platform"}, Value: 197}, {Labels: map[string]string{"prefix": "land"}, Value: 3}}, nil
		}
		return []Sample{{Labels: map[string]string{"prefix": "land"}, Value: 0}, {Labels: map[string]string{}, Value: 113}, {Labels: map[string]string{"prefix": "ci-platform"}, Value: 4}}, nil
	}
	rs := ciQueueReadings(context.Background(), prom, w)
	if len(rs) != 2 {
		t.Fatalf("readings: %+v", rs)
	}
	if r := rs[0]; r.Labels["run_prefix"] != "ci-platform" || r.Status != StatusOK || *r.Value != 4 || r.Samples != 197 || r.Definition.Version != QueueDefinitionVersion {
		t.Fatalf("ci-platform: %+v", r)
	}
	if r := rs[1]; r.Labels["run_prefix"] != "land" || r.Status != StatusTooFew || r.Samples != 3 {
		t.Fatalf("3 runs must be too_few: %+v", r)
	}
	if !strings.Contains(asked[0], "@ "+itoa(w.To.Unix())) || !strings.Contains(asked[0], `namespace="cicd"`) {
		t.Fatalf("query: %s", asked[0])
	}
	if rs := ciQueueReadings(context.Background(), nil, w); rs[0].Status != StatusUnmeasured {
		t.Fatalf("no Prometheus: %+v", rs[0])
	}
}
