package marketdata

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestWatchlistUnionNoCluster(t *testing.T) {
	svc := &Service{
		cfg:     Config{},
		cluster: nil,
		client:  http.DefaultClient,
	}
	resp := svc.WatchlistUnion(t.Context())
	if resp.OK {
		t.Fatal("expected OK=false without cluster")
	}
	if len(resp.Symbols) != 0 {
		t.Fatalf("expected 0 symbols, got %d", len(resp.Symbols))
	}
	if len(resp.Sources) != 3 {
		t.Fatalf("want an entry per env, got %v", resp.Sources)
	}
	for envID, src := range resp.Sources {
		if src.Status != "error" {
			t.Fatalf("env %s: expected error status, got %s", envID, src.Status)
		}
	}
}

// Symbols are invented; the probe read is scripted per env.
func TestWatchlistUnionUnionsWhatEachEnvPublishes(t *testing.T) {
	svc := &Service{watchlistProbe: func(_ context.Context, env string) ([]string, error) {
		switch env {
		case "dev":
			return []string{"QZAA", "QZBB"}, nil
		case "stg":
			return []string{}, nil // watches nothing: ok with count 0
		case "prod":
			return []string{"QZBB", "QZCC"}, nil
		}
		return nil, errors.New("unexpected env " + env)
	}}
	resp := svc.WatchlistUnion(t.Context())
	if !resp.OK || resp.Count != 3 || !reflect.DeepEqual(resp.Symbols, []string{"QZAA", "QZBB", "QZCC"}) {
		t.Fatalf("union %+v", resp)
	}
	want := map[string]EnvWatchlistResult{
		"dev":  {Count: 2, Status: "ok"},
		"stg":  {Count: 0, Status: "ok"},
		"prod": {Count: 2, Status: "ok"},
	}
	if !reflect.DeepEqual(resp.Sources, want) {
		t.Fatalf("sources %+v", resp.Sources)
	}
}

func TestWatchlistUnionReportsAnEnvItCannotReadAsAnError(t *testing.T) {
	svc := &Service{watchlistProbe: func(_ context.Context, env string) ([]string, error) {
		if env == "stg" {
			return nil, errors.New("data-probe has no watchlist (the application does not publish it yet)")
		}
		return []string{"QZAA"}, nil
	}}
	resp := svc.WatchlistUnion(t.Context())
	if !resp.OK || resp.Count != 1 {
		t.Fatalf("union %+v", resp)
	}
	stg := resp.Sources["stg"]
	if stg.Status != "error" || !strings.Contains(stg.Error, "no watchlist") || stg.Count != 0 {
		t.Fatalf("stg %+v", stg)
	}
}

func TestWatchlistUnionAllEnvsDownIsNotOK(t *testing.T) {
	svc := &Service{watchlistProbe: func(context.Context, string) ([]string, error) {
		return nil, errors.New("data-probe unavailable")
	}}
	resp := svc.WatchlistUnion(t.Context())
	if resp.OK || resp.Symbols == nil || len(resp.Symbols) != 0 || resp.Count != 0 {
		t.Fatalf("union %+v", resp)
	}
}

func TestHandleWatchlistUnionResponse(t *testing.T) {
	svc := &Service{
		cfg:     Config{},
		cluster: nil,
		client:  http.DefaultClient,
	}
	h := &Handler{svc: svc}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/watchlist/union", nil)
	rr := httptest.NewRecorder()
	h.HandleWatchlistUnion(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d", rr.Code)
	}
	var body WatchlistUnionResponse
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.GeneratedAt == "" {
		t.Fatal("missing generated_at")
	}
	if body.Sources == nil {
		t.Fatal("missing sources")
	}
}

// The wire shape the market-data plugin reads (ok, symbols, count, sources, generated_at)
// is unchanged.
func TestWatchlistUnionWireShape(t *testing.T) {
	svc := &Service{watchlistProbe: func(_ context.Context, env string) ([]string, error) {
		if env == "prod" {
			return nil, errors.New("HTTP 503: data_probe: database unavailable")
		}
		return []string{"QZAA"}, nil
	}}
	raw, err := json.Marshal(svc.WatchlistUnion(t.Context()))
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(body))
	for k := range body {
		keys = append(keys, k)
	}
	for _, k := range []string{"ok", "symbols", "count", "sources", "generated_at"} {
		if _, ok := body[k]; !ok {
			t.Fatalf("missing %q in %v", k, keys)
		}
	}
	if len(body) != 5 {
		t.Fatalf("unexpected keys %v", keys)
	}
	prod := body["sources"].(map[string]any)["prod"].(map[string]any)
	if prod["status"] != "error" || prod["error"] != "HTTP 503: data_probe: database unavailable" || prod["count"] != float64(0) {
		t.Fatalf("prod %v", prod)
	}
	dev := body["sources"].(map[string]any)["dev"].(map[string]any)
	if _, has := dev["error"]; has || dev["status"] != "ok" || dev["count"] != float64(1) {
		t.Fatalf("dev %v", dev)
	}
}
