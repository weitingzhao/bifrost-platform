package marketdata

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
)

// envDBMapping maps environment IDs to their PostgreSQL database names.
// dev-local shares the dev database, so we skip it.
var envDBMapping = map[string]string{
	"dev":  "bifrost_dev",
	"stg":  "bifrost_stg",
	"prod": "bifrost_prod",
}

const watchlistSQL = `SELECT DISTINCT symbol FROM public.watchlist WHERE sec_type = 'STK' AND optionable = true AND symbol IS NOT NULL AND trim(symbol) <> ''`

// EnvWatchlistResult holds the watchlist query result for a single environment.
type EnvWatchlistResult struct {
	Count  int    `json:"count"`
	Status string `json:"status"` // ok | error
	Error  string `json:"error,omitempty"`
}

// WatchlistUnionResponse is the JSON response for GET /api/v1/watchlist/union.
type WatchlistUnionResponse struct {
	OK          bool                          `json:"ok"`
	Symbols     []string                      `json:"symbols"`
	Count       int                           `json:"count"`
	Sources     map[string]EnvWatchlistResult `json:"sources"`
	GeneratedAt string                        `json:"generated_at"`
}

// WatchlistUnion queries the watchlist from each Trade environment's database,
// deduplicates, and returns the union set.
func (s *Service) WatchlistUnion(ctx context.Context) WatchlistUnionResponse {
	now := time.Now().UTC()
	resp := WatchlistUnionResponse{
		OK:          false,
		Symbols:     []string{},
		Sources:     make(map[string]EnvWatchlistResult),
		GeneratedAt: now.Format(time.RFC3339),
	}

	if s.cluster == nil {
		for envID := range envDBMapping {
			resp.Sources[envID] = EnvWatchlistResult{
				Status: "error",
				Error:  "cluster service unavailable",
			}
		}
		return resp
	}

	type result struct {
		envID   string
		symbols []string
		err     error
	}

	var wg sync.WaitGroup
	results := make(chan result, len(envDBMapping))

	for envID, db := range envDBMapping {
		wg.Add(1)
		go func(eid, database string) {
			defer wg.Done()
			syms, err := s.queryWatchlist(ctx, database)
			results <- result{envID: eid, symbols: syms, err: err}
		}(envID, db)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	unionSet := make(map[string]struct{})
	anyOK := false

	for r := range results {
		if r.err != nil {
			resp.Sources[r.envID] = EnvWatchlistResult{
				Status: "error",
				Error:  r.err.Error(),
			}
			continue
		}
		anyOK = true
		resp.Sources[r.envID] = EnvWatchlistResult{
			Count:  len(r.symbols),
			Status: "ok",
		}
		for _, sym := range r.symbols {
			unionSet[sym] = struct{}{}
		}
	}

	symbols := make([]string, 0, len(unionSet))
	for sym := range unionSet {
		symbols = append(symbols, sym)
	}
	sort.Strings(symbols)

	resp.Symbols = symbols
	resp.Count = len(symbols)
	resp.OK = anyOK
	return resp
}

// queryWatchlist runs the watchlist SELECT on the given database via CNPG pod exec.
func (s *Service) queryWatchlist(ctx context.Context, database string) ([]string, error) {
	out, err := s.cluster.ExecSQLOnPrimary(ctx, database, watchlistSQL)
	if err != nil {
		return nil, err
	}
	return parseSymbolOutput(out), nil
}

// parseSymbolOutput parses psql -tA output (one symbol per line).
func parseSymbolOutput(out string) []string {
	var symbols []string
	for _, line := range strings.Split(out, "\n") {
		sym := strings.TrimSpace(line)
		if sym == "" {
			continue
		}
		symbols = append(symbols, strings.ToUpper(sym))
	}
	return symbols
}
