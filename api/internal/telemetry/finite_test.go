package telemetry

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The wire shape Prometheus gave on 2026-11-13 for the p99 latency of a
// service with no requests in the window: the value is the string "NaN".
const vectorWithNaN = `{"status":"success","data":{"resultType":"vector","result":[
  {"metric":{"service":"api-market"},"value":[1794552474.5,"NaN"]},
  {"metric":{"service":"api-ops"},"value":[1794552474.5,"0.25"]},
  {"metric":{"service":"api-research"},"value":[1794552474.5,"+Inf"]},
  {"metric":{"service":"api-account"},"value":[1794552474.5,"-Inf"]}
]}}`

func TestOverviewWithANaNSampleStillHasABody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(vectorWithNaN))
	}))
	defer srv.Close()
	t.Setenv("PLATFORM_PROMETHEUS_URL", srv.URL)
	h := &Handler{svc: NewService(testConfigWithCluster(t))}

	for _, target := range []string{"/telemetry/overview?ns=bifrost-stg", "/telemetry/overview?group=compute", "/telemetry/query?q=api_latency_p99", "/telemetry/promql?q=up"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, target, nil)
		switch {
		case req.URL.Path == "/telemetry/query":
			h.HandleQuery(rec, req)
		case req.URL.Path == "/telemetry/promql":
			h.HandlePromQL(rec, req)
		default:
			h.HandleOverview(rec, req)
		}
		if rec.Code != http.StatusOK || rec.Body.Len() == 0 {
			t.Fatalf("%s: status %d, %d bytes; a NaN sample emptied the body", target, rec.Code, rec.Body.Len())
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: body is not JSON: %v", target, err)
		}
	}
}

func TestVectorToPointsKeepsOnlyFiniteSamples(t *testing.T) {
	var parsed promAPIResponse
	if err := json.Unmarshal([]byte(vectorWithNaN), &parsed); err != nil {
		t.Fatal(err)
	}
	points := vectorToPoints(parsed.Data.Result)
	if len(points) != 1 || points[0].Labels["service"] != "api-ops" || points[0].Value != 0.25 {
		t.Fatalf("got %+v, want only api-ops 0.25", points)
	}
}
