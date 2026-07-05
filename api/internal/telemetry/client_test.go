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

func TestFindQuery(t *testing.T) {
	spec, ok := FindQuery("api_request_rate")
	if !ok || spec.ID != "api_request_rate" {
		t.Fatalf("find: %+v ok=%v", spec, ok)
	}
	_, ok = FindQuery("missing")
	if ok {
		t.Fatal("expected missing query to fail")
	}
}

func TestResolveNamespace(t *testing.T) {
	if ResolveNamespace("") != DefaultNamespace() {
		t.Fatal("empty ns should default")
	}
	if ResolveNamespace("bifrost-prod") != "bifrost-prod" {
		t.Fatal("explicit ns")
	}
}

func TestHandlerOverviewPrometheusMissing(t *testing.T) {
	h := NewHandler(nil)
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

func TestHandlerQueryMissingParam(t *testing.T) {
	h := NewHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/telemetry/query?ns=bifrost-stg", nil)
	rec := httptest.NewRecorder()
	h.HandleQuery(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", rec.Code)
	}
}

func TestRunQueryEmptySeries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	}))
	defer srv.Close()

	svc := &Service{}
	spec, _ := FindQuery("api_request_rate")
	metric := svc.runQuery(context.Background(), NewClient(srv.URL), spec, "bifrost-stg")
	if metric.Status != "empty" {
		t.Fatalf("status: got %s want empty", metric.Status)
	}
}
