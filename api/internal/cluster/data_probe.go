package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// The platform never names an application's tables (D13, double flywheel). What it may know
// about an environment's application database — when it last changed, one sample count, and
// which tables belong together for a selective clone — the application publishes itself at
// GET <env gateway>/api/monitor/ops/data-probe:
//
//	{"generated_at": "…Z",
//	 "activity": [{"source": "<name>", "last_ts": "…Z" | null, "detail"?: "…"}],
//	 "sample": {"label": "<name>", "rows": 12},
//	 "clone_groups": [{"name": "<name>", "tables": ["<table>", …], "note": "…"}],
//	 "watchlist": {"label": "<name>", "symbols": ["<SYMBOL>", …] | null, "count": 2, "detail"?: "…"}}
//
// watchlist is the stocks the application wants option data for; the platform unions it across
// environments for the market-data plugin (GET /api/v1/watchlist/union). symbols null means the
// application could not say (detail tells why); an empty list means it watches nothing.
//
// An application without that endpoint (or one that is down) is "unknown" everywhere it is
// read. Nothing falls back to querying tables.

// dataProbePaths are tried in order on each env gateway; api-monitor serves the probe at both
// /ops/data-probe and /data-probe, and both answer through its own prefix /api/monitor (measured
// DEV 2026-10-04). The /api/ops/… alias is not used: TD-55 B2 retires it once its Traefik
// traffic is zero. An unrouted path falls through to the SPA and answers index.html with 200,
// which is why every response is checked for the probe's shape rather than for a 200.
var dataProbePaths = []string{"/api/monitor/ops/data-probe", "/api/monitor/data-probe"}

const (
	dataProbeTimeout  = 6 * time.Second
	dataProbeMaxBytes = 1 << 20
)

// dataProbeDatabases maps the clone/freshness databases to the environment whose gateway
// publishes their probe.
var dataProbeDatabases = map[string]string{
	"bifrost_dev":  "dev",
	"bifrost_stg":  "stg",
	"bifrost_prod": "prod",
}

type DataProbeActivity struct {
	Source string  `json:"source"`
	LastTS *string `json:"last_ts"`
	Detail string  `json:"detail,omitempty"`
}

type DataProbeSample struct {
	Label string `json:"label"`
	Rows  *int   `json:"rows"`
}

// DataCloneGroup is a set of tables the application says can be cloned together: a group's
// tables include every table that references them, so a selective clone of one group passes
// the FK closure check.
type DataCloneGroup struct {
	Name   string   `json:"name"`
	Tables []string `json:"tables"`
	Note   string   `json:"note,omitempty"`
}

// DataProbeWatchlist is the application's list of stocks it wants option data for. Symbols is
// nil when the application sent null (it could not read its list) and empty when it watches none.
type DataProbeWatchlist struct {
	Label   string   `json:"label"`
	Symbols []string `json:"symbols"`
	Count   *int     `json:"count"`
	Detail  string   `json:"detail,omitempty"`
}

type DataProbe struct {
	GeneratedAt string              `json:"generated_at"`
	Activity    []DataProbeActivity `json:"activity"`
	Sample      *DataProbeSample    `json:"sample"`
	CloneGroups []DataCloneGroup    `json:"clone_groups"`
	Watchlist   *DataProbeWatchlist `json:"watchlist"`
}

// dataProbeGateway resolves an environment's gateway base URL and its Host-header rule from
// the cluster entry (the same gateways the smoke probes use).
func (s *Service) dataProbeGateway(env string) (string, func(*http.Request)) {
	e := s.entry
	switch env {
	case "dev":
		return e.ResolvedDevGatewayURL(), e.ApplyDevGatewayHost
	case "stg":
		return e.ResolvedStgGatewayURL(), e.ApplyStgGatewayHost
	case "prod":
		return e.ResolvedProdGatewayURL(), e.ApplyProdGatewayHost
	}
	return "", nil
}

// fetchDataProbe reads the environment's data probe. Any failure — no gateway, unreachable,
// non-200, or a body that is not a probe — is returned as an error; callers show "unknown".
func (s *Service) fetchDataProbe(ctx context.Context, env string) (*DataProbe, error) {
	base, applyHost := s.dataProbeGateway(env)
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return nil, fmt.Errorf("no %s gateway configured", env)
	}
	var errs []string
	for _, path := range dataProbePaths {
		probe, err := getDataProbe(ctx, base+path, applyHost)
		if err == nil {
			return probe, nil
		}
		errs = append(errs, path+": "+err.Error())
		if ctx.Err() != nil {
			break
		}
	}
	return nil, errors.New("data-probe unavailable (" + strings.Join(errs, "; ") + ")")
}

func getDataProbe(ctx context.Context, url string, applyHost func(*http.Request)) (*DataProbe, error) {
	ctx, cancel := context.WithTimeout(ctx, dataProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if applyHost != nil {
		applyHost(req)
	}
	resp, err := (&http.Client{Timeout: dataProbeTimeout}).Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, dataProbeMaxBytes))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		var reason struct {
			Detail string `json:"detail"`
		}
		if json.Unmarshal(body, &reason) == nil && strings.TrimSpace(reason.Detail) != "" {
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(reason.Detail))
		}
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return parseDataProbe(body, resp.Header.Get("Content-Type"))
}

// parseDataProbe accepts a body only if it has the probe's shape: a JSON object carrying an
// activity list. An SPA's index.html or another service's JSON is not a probe.
func parseDataProbe(body []byte, contentType string) (*DataProbe, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		ct := strings.TrimSpace(contentType)
		if ct == "" {
			ct = "unknown content type"
		}
		return nil, fmt.Errorf("not a data-probe response (%s)", ct)
	}
	if _, ok := raw["activity"]; !ok {
		return nil, fmt.Errorf("not a data-probe response (no activity)")
	}
	var probe DataProbe
	if err := json.Unmarshal(body, &probe); err != nil {
		return nil, fmt.Errorf("malformed data-probe response: %w", err)
	}
	return &probe, nil
}

// WatchlistSymbols is the environment's watchlist as its data probe publishes it: trimmed,
// upper-cased, distinct and sorted. Every way of not knowing is an error — the probe is
// unreachable, the application predates the watchlist key, or it sent symbols: null — so a
// caller never mistakes "could not read" for "watches nothing". Nothing falls back to SQL.
func (s *Service) WatchlistSymbols(ctx context.Context, env string) ([]string, error) {
	probe, err := s.fetchDataProbe(ctx, env)
	if err != nil {
		return nil, err
	}
	return probe.watchlistSymbols()
}

func (p *DataProbe) watchlistSymbols() ([]string, error) {
	w := p.Watchlist
	if w == nil {
		return nil, errors.New("data-probe has no watchlist (the application does not publish it yet)")
	}
	if w.Symbols == nil {
		detail := strings.TrimSpace(w.Detail)
		if detail == "" {
			detail = "symbols: null"
		}
		return nil, fmt.Errorf("data-probe watchlist unavailable (%s)", detail)
	}
	seen := make(map[string]struct{}, len(w.Symbols))
	out := make([]string, 0, len(w.Symbols))
	for _, raw := range w.Symbols {
		sym := strings.ToUpper(strings.TrimSpace(raw))
		if sym == "" {
			continue
		}
		if _, dup := seen[sym]; dup {
			continue
		}
		seen[sym] = struct{}{}
		out = append(out, sym)
	}
	sort.Strings(out)
	return out, nil
}

// latestActivity is the newest last_ts across the probe's sources, and the sources that
// reported one.
func (p *DataProbe) latestActivity() (*time.Time, []string, error) {
	var latest *time.Time
	var sources []string
	for _, a := range p.Activity {
		if a.LastTS == nil || strings.TrimSpace(*a.LastTS) == "" {
			continue
		}
		ts, err := parsePostgresTimestamp(*a.LastTS)
		if err != nil {
			return nil, sources, fmt.Errorf("data-probe %s last_ts %q: %w", a.Source, *a.LastTS, err)
		}
		sources = append(sources, a.Source)
		if latest == nil || ts.After(*latest) {
			t := ts
			latest = &t
		}
	}
	return latest, sources, nil
}

// usableCloneGroups drops groups without a name or without tables that pass the clone
// request's identifier check, so the Console only offers selections the API would accept.
func usableCloneGroups(groups []DataCloneGroup) []DataCloneGroup {
	out := make([]DataCloneGroup, 0, len(groups))
	for _, g := range groups {
		name := strings.TrimSpace(g.Name)
		if name == "" || len(g.Tables) == 0 {
			continue
		}
		ok := true
		for _, t := range g.Tables {
			if !safeIdentRe.MatchString(t) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, DataCloneGroup{Name: name, Tables: append([]string{}, g.Tables...), Note: strings.TrimSpace(g.Note)})
		}
	}
	return out
}
