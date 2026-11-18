package telemetry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

const mockVectorResponse = `{
  "status": "success",
  "data": {
    "resultType": "vector",
    "result": [
      {
        "metric": {"service": "api-monitor", "namespace": "bifrost-stg"},
        "value": [1710000000, "12.5"]
      }
    ]
  }
}`

func TestClientQueryInstant(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query" {
			t.Fatalf("path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("query") == "" {
			t.Fatal("expected query param")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mockVectorResponse))
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	points, err := client.QueryInstant(context.Background(), `up{job="test"}`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(points) != 1 {
		t.Fatalf("points: got %d want 1", len(points))
	}
	if points[0].Value != 12.5 {
		t.Fatalf("value: got %v want 12.5", points[0].Value)
	}
	if points[0].Labels["service"] != "api-monitor" {
		t.Fatalf("labels: %+v", points[0].Labels)
	}
}

func TestVectorToPointsEmpty(t *testing.T) {
	points := vectorToPoints(nil)
	if len(points) != 0 {
		t.Fatalf("expected empty, got %d", len(points))
	}
}

func TestResolveNamespace(t *testing.T) {
	if ResolveNamespace("") != DefaultNamespace() {
		t.Fatal("empty ns should default")
	}
	if ResolveNamespace("bifrost-prod") != "bifrost-prod" {
		t.Fatal("explicit ns")
	}
	// Non-whitelisted values must never reach PromQL interpolation.
	for _, raw := range []string{
		`bifrost-stg"} or up{`,
		"data",
		"monitoring",
		"bifrost-stg ",
	} {
		if got := ResolveNamespace(raw); got != DefaultNamespace() && got != "bifrost-stg" {
			t.Fatalf("injection candidate %q resolved to %q", raw, got)
		}
	}
	if ResolveNamespace(`evil"}`) != DefaultNamespace() {
		t.Fatal("injection candidate should fall back to default")
	}
}

func TestHandlerOverviewPrometheusMissing(t *testing.T) {
	h := NewHandler(nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/telemetry/overview?ns=bifrost-stg", nil)
	rec := httptest.NewRecorder()
	h.HandleOverview(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d want 503", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["hint"] == "" {
		t.Fatalf("expected hint, got %+v", body)
	}
}

func TestRunQueryEmptySeries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	}))
	defer srv.Close()

	svc := &Service{}
	spec := PresetQueries()[0]
	metric := svc.runQuery(context.Background(), NewClient(srv.URL), spec, "bifrost-stg")
	if metric.Status != "empty" {
		t.Fatalf("status: got %s want empty", metric.Status)
	}
}
