package cluster

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/resourceview"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

const (
	freshnessCacheTTL   = 30 * time.Second
	freshnessFreshDays  = 3.0
	freshnessStaleDays  = 7.0
	dataCloneConfirmTok = "CLONE-FROM-PROD"
)

type DataFreshnessDB = resourceview.DataFreshnessDB

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
	// CloneGroups are the selective-clone groups the clone source (ReferenceDB) publishes in
	// its data probe; empty when the probe is unavailable (CloneGroupsDetail says why).
	CloneGroups       []DataCloneGroup `json:"clone_groups"`
	CloneGroupsDetail string           `json:"clone_groups_detail,omitempty"`
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

	// Each environment's application says when its data last changed (data probe); the
	// platform reads no tables. A probe that cannot be read leaves that database "unknown".
	type envProbe struct {
		ts      *time.Time
		sources []string
		err     error
	}
	probes := make([]envProbe, len(resp.Databases))
	var cloneSource *DataProbe
	var wg sync.WaitGroup
	for i := range resp.Databases {
		wg.Add(1)
		go func(i int) {
			defer safego.Recover("cluster.dataFreshness.probe")
			defer wg.Done()
			p, err := s.fetchDataProbe(ctx, resp.Databases[i].Environment)
			if err != nil {
				probes[i].err = err
				return
			}
			if resp.Databases[i].Name == resp.ReferenceDB {
				cloneSource = p
			}
			probes[i].ts, probes[i].sources, probes[i].err = p.latestActivity()
		}(i)
	}
	wg.Wait()

	resp.CloneGroups = []DataCloneGroup{}
	switch {
	case cloneSource == nil:
		resp.CloneGroupsDetail = resp.ReferenceDB + " data-probe unavailable"
	default:
		resp.CloneGroups = usableCloneGroups(cloneSource.CloneGroups)
		if len(resp.CloneGroups) == 0 {
			resp.CloneGroupsDetail = resp.ReferenceDB + " data-probe publishes no clone groups"
		}
	}

	observed := make([]resourceview.ActivityObservation, len(probes))
	for i, p := range probes {
		observed[i] = resourceview.ActivityObservation{At: p.ts, Sources: p.sources, Err: p.err}
	}
	if appReadings.FreshnessDatabases != nil {
		resp.Databases = appReadings.FreshnessDatabases(resp.Databases, observed, resp.ReferenceDB, resp.LastCloneAt, now)
	}
	return resp
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
