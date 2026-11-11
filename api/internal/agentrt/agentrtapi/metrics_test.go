package agentrtapi_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func TestMetricReadingsAPI(t *testing.T) {
	t.Parallel()
	pool, _ := world(t)
	s := server(t, pool)
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	value := "12.25"
	row := store.MetricReading{MetricID: "lead_time_s", DefinitionVersion: 1, DefinitionSHA256: strings.Repeat("a", 64), ScopeKind: "all", Label: "baseline:before", WindowKind: "span", WindowStart: start, WindowEnd: start.Add(24 * time.Hour), Unit: "seconds", Samples: 2, Value: &value, StatsJSON: map[string]any{"p90": 20}, Conclusive: true}
	_, err := store.WriteMetricReadings(ctx, pool, store.MetricComputation{Source: "computed", Readings: []store.MetricReading{row}})
	must(t, err)
	row.Label = "group:after"
	row.WindowStart = row.WindowEnd
	row.WindowEnd = row.WindowEnd.Add(24 * time.Hour)
	current := "15.5"
	row.Value = &current
	_, err = store.WriteMetricReadings(ctx, pool, store.MetricComputation{Source: "computed", Readings: []store.MetricReading{row}})
	must(t, err)
	var readings struct {
		Readings []map[string]any `json:"readings"`
	}
	must(t, json.Unmarshal(get(t, s, "metrics/readings?metric=lead_time_s"), &readings))
	if len(readings.Readings) != 2 || readings.Readings[0]["label"] != "group:after" || readings.Readings[0]["value"] != 15.5 || readings.Readings[0]["source"] != "computed" {
		t.Fatalf("readings: %+v", readings)
	}
	if _, ok := readings.Readings[0]["stats"].(map[string]any); !ok {
		t.Fatal("missing stats object")
	}
	var comparison struct {
		Same       bool     `json:"same_definition"`
		Conclusive bool     `json:"conclusive"`
		Change     *float64 `json:"change"`
	}
	must(t, json.Unmarshal(get(t, s, "metrics/compare?metric=lead_time_s&label=group:after"), &comparison))
	if !comparison.Same || !comparison.Conclusive || comparison.Change == nil || *comparison.Change != 3.25 {
		t.Fatalf("comparison: %+v", comparison)
	}
	// A new version must never silently compare with the older baseline.
	row.DefinitionVersion = 2
	row.DefinitionSHA256 = strings.Repeat("b", 64)
	_, err = store.WriteMetricReadings(ctx, pool, store.MetricComputation{Source: "computed", Readings: []store.MetricReading{row}})
	must(t, err)
	must(t, json.Unmarshal(get(t, s, "metrics/compare?metric=lead_time_s&label=group:after"), &comparison))
	if comparison.Same || comparison.Conclusive || comparison.Change != nil {
		t.Fatalf("different definition: %+v", comparison)
	}
	// The same definition with insufficient evidence is not conclusive.
	row.Label = "group:small"
	row.DefinitionVersion = 1
	row.DefinitionSHA256 = strings.Repeat("a", 64)
	row.Samples = 0
	row.Value = nil
	row.Conclusive = false
	_, err = store.WriteMetricReadings(ctx, pool, store.MetricComputation{Source: "computed", Readings: []store.MetricReading{row}})
	must(t, err)
	must(t, json.Unmarshal(get(t, s, "metrics/compare?metric=lead_time_s&label=group:small"), &comparison))
	if !comparison.Same || comparison.Conclusive || comparison.Change != nil {
		t.Fatalf("too few samples: %+v", comparison)
	}
	row.Label = "group:long"
	row.WindowEnd = row.WindowEnd.Add(time.Hour)
	_, err = store.WriteMetricReadings(ctx, pool, store.MetricComputation{Source: "computed", Readings: []store.MetricReading{row}})
	must(t, err)
	if code, _ := call(t, s, "viewer", "GET", "metrics/compare?metric=lead_time_s&label=group:long", nil); code != 404 {
		t.Fatalf("unequal windows: %d", code)
	}
	row.Label = ""
	row.WindowKind = "day"
	row.WindowEnd = row.WindowEnd.Add(-time.Hour)
	_, err = store.WriteMetricReadings(ctx, pool, store.MetricComputation{Source: "computed", Readings: []store.MetricReading{row}})
	must(t, err)
	must(t, json.Unmarshal(get(t, s, "metrics/compare?metric=lead_time_s&from=2026-11-02T00:00:00Z&to=2026-11-03T00:00:00Z"), &comparison))
	if !comparison.Same || comparison.Conclusive {
		t.Fatalf("explicit window: %+v", comparison)
	}
	for _, path := range []string{"metrics/readings", "metrics/readings?metric=x&limit=501", "metrics/readings?metric=x&limit=0", "metrics/compare?metric=x", "metrics/compare?metric=x&label=baseline:no", "metrics/compare?metric=x&label=group:a&from=bad"} {
		if code, _ := call(t, s, "viewer", "GET", path, nil); code != 400 {
			t.Fatalf("%s: %d", path, code)
		}
	}
	if code, _ := call(t, s, "", "GET", "metrics/readings?metric=x", nil); code != 401 {
		t.Fatalf("anonymous read: %d", code)
	}
}
