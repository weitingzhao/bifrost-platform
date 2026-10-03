package marketdata

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// watchlistEnvs are the application environments whose watchlists are unioned. dev-local
// shares the dev database, so it is not listed.
var watchlistEnvs = []string{"dev", "stg", "prod"}

// EnvWatchlistResult holds the watchlist read for a single environment.
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

// WatchlistUnion reads each environment's watchlist from the application's data probe
// (GET <env gateway>/api/ops/ops/data-probe → watchlist.symbols), deduplicates, and returns
// the union. The platform does not select from the application's tables (Owner 2026-10-03,
// option A): an environment whose probe is unreachable, or that does not publish a watchlist,
// is reported as an error under sources, never as an empty list, and nothing falls back to SQL.
// ok is true when at least one environment answered, as before.
func (s *Service) WatchlistUnion(ctx context.Context) WatchlistUnionResponse {
	now := time.Now().UTC()
	resp := WatchlistUnionResponse{
		OK:          false,
		Symbols:     []string{},
		Sources:     make(map[string]EnvWatchlistResult),
		GeneratedAt: now.Format(time.RFC3339),
	}

	read := s.watchlistProbe
	if read == nil {
		if s.cluster == nil {
			for _, envID := range watchlistEnvs {
				resp.Sources[envID] = EnvWatchlistResult{
					Status: "error",
					Error:  "cluster service unavailable",
				}
			}
			return resp
		}
		read = s.cluster.WatchlistSymbols
	}

	type result struct {
		envID   string
		symbols []string
		err     error
	}

	var wg sync.WaitGroup
	results := make(chan result, len(watchlistEnvs))

	for _, envID := range watchlistEnvs {
		wg.Add(1)
		go func(eid string) {
			defer safego.Recover("marketdata.readWatchlist")
			defer wg.Done()
			syms, err := read(ctx, eid)
			results <- result{envID: eid, symbols: syms, err: err}
		}(envID)
	}

	go func() {
		defer safego.Recover("marketdata.watchlistUnion.close")
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
