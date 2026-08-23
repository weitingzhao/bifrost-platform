package cluster

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	freshnessCacheTTL  = 30 * time.Second
	freshnessFreshDays = 3.0
	freshnessStaleDays = 7.0
	dataCloneConfirmTok = "CLONE-FROM-PROD"
)

// Activity probe columns — table may be missing; probe degrades gracefully.
var freshnessProbeSpecs = []struct {
	table  string
	column string
}{
	{"strategy_instance", "updated_at"},
	{"strategy_opportunity", "updated_at"},
	{"watchlist", "created_at"},
}

type DataFreshnessDB struct {
	Name           string   `json:"name"`
	Environment    string   `json:"environment"`
	LastActivityTS *string  `json:"last_activity_ts,omitempty"`
	// AgeDays is wall-clock age (now − last_activity). Informational only — do not drive Sync decisions.
	AgeDays *float64 `json:"age_days,omitempty"`
	// LagVsProdDays is max(0, prod_activity − target_activity) in days. Nil for bifrost_prod (reference).
	LagVsProdDays *float64 `json:"lag_vs_prod_days,omitempty"`
	// StaleDays mirrors LagVsProdDays for non-prod (backward-compatible Sync signal). Nil for prod reference.
	StaleDays   *float64 `json:"stale_days,omitempty"`
	Verdict     string   `json:"verdict"` // fresh | aging | stale | reference | unknown
	Detail      string   `json:"detail,omitempty"`
	Sources     []string `json:"sources,omitempty"`
	LastCloneAt *string  `json:"last_clone_at,omitempty"`
}

type DataFreshnessResponse struct {
	ClusterID      string            `json:"cluster_id"`
	PrimaryPod     string            `json:"primary_pod,omitempty"`
	ReferenceDB    string            `json:"reference_db"`
	Databases      []DataFreshnessDB `json:"databases"`
	CacheHit       bool              `json:"cache_hit"`
	Detail         string            `json:"detail,omitempty"`
	GeneratedAt    time.Time         `json:"generated_at"`
	FreshThreshold float64           `json:"fresh_threshold_days"`
	StaleThreshold float64           `json:"stale_threshold_days"`
	// LastCloneAt is when the most recent successful clone finished (global).
	LastCloneAt *string `json:"last_clone_at,omitempty"`
}

type freshnessCache struct {
	mu      sync.Mutex
	at      time.Time
	payload DataFreshnessResponse
}

var sharedFreshnessCache freshnessCache

func (s *Service) DataFreshness(ctx context.Context) DataFreshnessResponse {
	sharedFreshnessCache.mu.Lock()
	if !sharedFreshnessCache.at.IsZero() && time.Since(sharedFreshnessCache.at) < freshnessCacheTTL {
		cached := sharedFreshnessCache.payload
		sharedFreshnessCache.mu.Unlock()
		cached.CacheHit = true
		return cached
	}
	sharedFreshnessCache.mu.Unlock()

	resp := s.probeDataFreshness(ctx)
	sharedFreshnessCache.mu.Lock()
	sharedFreshnessCache.at = time.Now().UTC()
	sharedFreshnessCache.payload = resp
	sharedFreshnessCache.mu.Unlock()
	return resp
}

func (s *Service) probeDataFreshness(ctx context.Context) DataFreshnessResponse {
	now := time.Now().UTC()
	base := s.baseMeta(now)
	resp := DataFreshnessResponse{
		ClusterID:      base.ClusterID,
		ReferenceDB:    "bifrost_prod",
		FreshThreshold: freshnessFreshDays,
		StaleThreshold: freshnessStaleDays,
		GeneratedAt:    now,
		Databases: []DataFreshnessDB{
			{Name: "bifrost_dev", Environment: "dev", Verdict: "unknown"},
			{Name: "bifrost_stg", Environment: "stg", Verdict: "unknown"},
			{Name: "bifrost_prod", Environment: "prod", Verdict: "unknown"},
		},
	}

	s.ensureCloneStores()
	if meta := s.cloneLast.Get(); meta.LastCloneAt != nil {
		iso := meta.LastCloneAt.UTC().Format(time.RFC3339)
		resp.LastCloneAt = &iso
	}

	primary, err := s.resolveCNPGPrimary(ctx)
	if err != nil {
		resp.Detail = err.Error()
		for i := range resp.Databases {
			resp.Databases[i].Detail = "primary unavailable"
		}
		return resp
	}
	resp.PrimaryPod = primary

	prodTS, prodSources, prodErr := s.queryDBActivity(ctx, primary, "bifrost_prod")
	for i := range resp.Databases {
		db := &resp.Databases[i]
		if resp.LastCloneAt != nil && db.Name != "bifrost_prod" {
			db.LastCloneAt = resp.LastCloneAt
		}
		ts, sources, qErr := s.queryDBActivity(ctx, primary, db.Name)
		if qErr != nil {
			db.Verdict = "unknown"
			db.Detail = qErr.Error()
			continue
		}
		db.Sources = sources
		if ts == nil {
			db.Verdict = "unknown"
			db.Detail = "no activity timestamps found"
			continue
		}
		iso := ts.UTC().Format(time.RFC3339)
		db.LastActivityTS = &iso
		age := now.Sub(ts.UTC()).Hours() / 24.0
		if age < 0 {
			age = 0
		}
		db.AgeDays = &age

		if db.Name == "bifrost_prod" {
			db.Verdict = "reference"
			if len(prodSources) > 0 {
				db.Detail = "reference · wall_age=" + fmt.Sprintf("%.1fd", age)
			} else {
				db.Detail = "reference"
			}
			continue
		}

		if prodTS == nil || prodErr != nil {
			db.Verdict = "unknown"
			if prodErr != nil {
				db.Detail = "prod activity unavailable: " + prodErr.Error()
			} else {
				db.Detail = "prod activity unavailable"
			}
			continue
		}

		lag := prodTS.Sub(ts.UTC()).Hours() / 24.0
		if lag < 0 {
			lag = 0
		}
		db.LagVsProdDays = &lag
		db.StaleDays = &lag // Sync signal = lag (not wall-clock age)
		db.Verdict = freshnessVerdictFromLag(lag)
		db.Detail = fmt.Sprintf("lag_vs_prod=%.1fd · wall_age=%.1fd", lag, age)
	}
	return resp
}

// freshnessVerdictFromLag maps lag_vs_prod_days → verdict for non-prod DBs.
// Aligns with Console badge: <3 fresh/green · 3–7 aging/yellow · ≥7 stale/red.
func freshnessVerdictFromLag(lagDays float64) string {
	switch {
	case lagDays < freshnessFreshDays:
		return "fresh"
	case lagDays < freshnessStaleDays:
		return "aging"
	default:
		return "stale"
	}
}

// FreshnessBadgeLevel maps lag (preferred) to Console badge colors.
// days should be lag_vs_prod_days for non-prod; reference/unknown return dedicated levels.
func FreshnessBadgeLevel(days *float64, verdict string) string {
	switch verdict {
	case "unknown":
		return "unknown"
	case "reference":
		return "reference"
	}
	if days == nil {
		return "unknown"
	}
	d := *days
	switch {
	case d < freshnessFreshDays:
		return "green"
	case d < freshnessStaleDays:
		return "yellow"
	default:
		return "red"
	}
}

func (s *Service) resolveCNPGPrimary(ctx context.Context) (string, error) {
	if s != nil && s.primaryOverride != "" {
		return s.primaryOverride, nil
	}
	dyn, err := s.buildDynamicClient()
	if err != nil {
		return "", fmt.Errorf("dynamic client: %w", err)
	}
	obj, err := dyn.Resource(cnpgClusterGVR).Namespace(cnpgNamespace).Get(ctx, cnpgClusterName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get CNPG cluster: %w", err)
	}
	primary := strings.TrimSpace(stringFromUnstructured(obj, "status", "currentPrimary"))
	if primary == "" {
		return "", fmt.Errorf("CNPG cluster %s has no currentPrimary", cnpgClusterName)
	}
	return primary, nil
}

func (s *Service) queryDBActivity(ctx context.Context, primary, database string) (*time.Time, []string, error) {
	// Discover which probe tables exist.
	var existsParts []string
	for _, spec := range freshnessProbeSpecs {
		existsParts = append(existsParts, fmt.Sprintf(
			"SELECT '%s' AS t, '%s' AS c WHERE EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='%s' AND column_name='%s')",
			spec.table, spec.column, spec.table, spec.column,
		))
	}
	listSQL := strings.Join(existsParts, " UNION ALL ")
	out, err := s.execOnPrimary(ctx, primary, "psql", "-U", "postgres", "-d", database, "-tAc", listSQL)
	if err != nil {
		return nil, nil, err
	}
	lines := splitNonEmpty(out)
	if len(lines) == 0 {
		return nil, nil, nil
	}

	var greatestParts []string
	var sources []string
	for _, line := range lines {
		fields := strings.Split(strings.TrimSpace(line), "|")
		if len(fields) != 2 {
			continue
		}
		table, col := fields[0], fields[1]
		sources = append(sources, table+"."+col)
		greatestParts = append(greatestParts, fmt.Sprintf("(SELECT MAX(%s) FROM %s)", col, table))
	}
	if len(greatestParts) == 0 {
		return nil, nil, nil
	}
	maxSQL := fmt.Sprintf("SELECT GREATEST(%s)", strings.Join(greatestParts, ", "))
	raw, err := s.execOnPrimary(ctx, primary, "psql", "-U", "postgres", "-d", database, "-tAc", maxSQL)
	if err != nil {
		return nil, sources, err
	}
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "null") {
		return nil, sources, nil
	}
	ts, err := parsePostgresTimestamp(raw)
	if err != nil {
		return nil, sources, fmt.Errorf("parse timestamp %q: %w", raw, err)
	}
	return &ts, sources, nil
}

func parsePostgresTimestamp(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999-07",
		"2006-01-02 15:04:05.999999+00",
		"2006-01-02 15:04:05-07",
		"2006-01-02 15:04:05+00",
		"2006-01-02 15:04:05.999999",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC(), nil
		}
	}
	// Numeric epoch fallback
	if f, err := strconv.ParseFloat(raw, 64); err == nil && f > 1e9 {
		return time.Unix(int64(f), 0).UTC(), nil
	}
	return time.Time{}, fmt.Errorf("unrecognized timestamp")
}

func splitNonEmpty(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}
