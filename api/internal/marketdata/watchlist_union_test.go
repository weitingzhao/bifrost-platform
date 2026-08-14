package marketdata

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseSymbolOutput(t *testing.T) {
	out := "AAPL\nNVDA\nMSFT\n\n"
	syms := parseSymbolOutput(out)
	if len(syms) != 3 {
		t.Fatalf("expected 3 symbols, got %d: %v", len(syms), syms)
	}
	if syms[0] != "AAPL" || syms[1] != "NVDA" || syms[2] != "MSFT" {
		t.Fatalf("unexpected symbols: %v", syms)
	}
}

func TestParseSymbolOutputEmpty(t *testing.T) {
	syms := parseSymbolOutput("")
	if len(syms) != 0 {
		t.Fatalf("expected 0 symbols, got %d", len(syms))
	}
}

func TestParseSymbolOutputLowercase(t *testing.T) {
	syms := parseSymbolOutput("aapl\nNvDa\n")
	if len(syms) != 2 || syms[0] != "AAPL" || syms[1] != "NVDA" {
		t.Fatalf("expected uppercase, got %v", syms)
	}
}

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
	for envID, src := range resp.Sources {
		if src.Status != "error" {
			t.Fatalf("env %s: expected error status, got %s", envID, src.Status)
		}
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
