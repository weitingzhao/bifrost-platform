package marketdata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

type Service struct {
	cfg     Config
	cluster *cluster.Service
	client  *http.Client
	// freshnessProbe overrides CNPG query in unit tests.
	freshnessProbe func(ctx context.Context) ([]FreshnessInfo, probe.Reachability, string)
	// readinessProbe overrides Plugin readiness rollup in unit tests.
	readinessProbe func(ctx context.Context) *ReadinessRollup
	// deploymentsOverride skips live K8s reads in unit tests.
	deploymentsOverride []DeploymentInfo
}

func NewService(clusterSvc *cluster.Service) *Service {
	return &Service{
		cfg:     ConfigFromEnv(),
		cluster: clusterSvc,
		client:  &http.Client{Timeout: 5 * time.Second},
	}
}

func (s *Service) Status(ctx context.Context) StatusResponse {
	now := time.Now().UTC()
	resp := StatusResponse{
		Reachable:      false,
		Reachability:   probe.ReachUnknown,
		HealthReach:    probe.ReachUnknown,
		FreshnessReach: probe.ReachUnknown,
		Autonomy:       "L0",
		Deployments:    []DeploymentInfo{},
		Freshness:      []FreshnessInfo{},
		GeneratedAt:    now,
	}

	stocks := s.readDeployment(ctx, stocksDeployName)
	options := s.readDeployment(ctx, optionsDeployName)
	if s.deploymentsOverride != nil {
		resp.Deployments = append([]DeploymentInfo{}, s.deploymentsOverride...)
	} else {
		resp.Deployments = []DeploymentInfo{stocks, options}
	}

	workers := make([]WorkerInfo, 0, 2)
	healthErrors := make([]string, 0, 2)
	healthReach := probe.ReachOK

	for _, item := range []struct {
		url string
		svc string
	}{
		{s.cfg.StocksHealthURL, "market-data-health-stocks"},
		{s.cfg.OptionsHealthURL, "market-data-health-options"},
	} {
		worker, reach, errMsg := s.probePoolHealth(ctx, item.url, item.svc)
		if worker != nil {
			workers = append(workers, *worker)
		}
		if errMsg != "" {
			healthErrors = append(healthErrors, errMsg)
		}
		healthReach = worseReach(healthReach, reach)
	}
	nextByPool := s.probeCronJobNextFire(ctx, now)
	for i := range workers {
		if t, ok := nextByPool[workers[i].Pool]; ok {
			workers[i].NextRunAt = t.UTC().Format(time.RFC3339)
		}
	}

	resp.Workers = workers
	resp.HealthReach = healthReach
	if healthReach == probe.ReachFail && len(healthErrors) > 0 {
		resp.Error = strings.Join(healthErrors, "; ")
		resp.Hint = "Ensure plugin-market-data NS is applied and per-pool health Services are reachable"
	}

	freshRows, freshReach, freshErr := s.probeFreshness(ctx)
	resp.Freshness = freshRows
	resp.FreshnessReach = freshReach
	// Informational unknown (no rows yet) must not populate Error — Console treats
	// non-empty error as fail even when reachability is ok.
	if freshErr != "" && resp.Error == "" && freshReach != probe.ReachUnknown {
		resp.Error = freshErr
		resp.Hint = "Ensure ops_jobs.ingest_freshness is populated (worker jobs marking done)"
	}

	// Plugin-native readiness KPI — failure leaves field nil (does not affect reachability).
	resp.ReadinessRollup = s.probeReadinessRollup(ctx)

	readyCount := 0
	for _, d := range resp.Deployments {
		if d.Reach == probe.ReachOK {
			readyCount++
		}
	}
	total := len(resp.Deployments)

	switch {
	case readyCount == 0:
		resp.Reachability = probe.ReachFail
		resp.Reachable = false
	case readyCount < total || healthReach == probe.ReachFail || freshReach == probe.ReachFail:
		resp.Reachability = probe.ReachDegraded
		resp.Reachable = readyCount > 0
	case healthReach == probe.ReachDegraded || healthReach == probe.ReachUnknown ||
		freshReach == probe.ReachDegraded:
		// freshness unknown (no rows yet) must not degrade overall reachability
		resp.Reachability = probe.ReachDegraded
		resp.Reachable = true
	default:
		resp.Reachability = probe.ReachOK
		resp.Reachable = true
	}

	parts := []string{fmt.Sprintf("%d/%d deployments ready", readyCount, total)}
	for _, w := range resp.Workers {
		parts = append(parts, fmt.Sprintf("%s %d done %d failed", w.Pool, w.JobsDone, w.JobsFailed))
	}
	if len(resp.Freshness) > 0 {
		stale := 0
		for _, f := range resp.Freshness {
			if f.Verdict != "ok" {
				stale++
			}
		}
		parts = append(parts, fmt.Sprintf("freshness %d/%d ok", len(resp.Freshness)-stale, len(resp.Freshness)))
	} else if freshReach != probe.ReachOK {
		parts = append(parts, "freshness="+string(freshReach))
	}
	if healthReach != probe.ReachOK && len(healthErrors) > 0 {
		parts = append(parts, "health="+string(healthReach))
	}
	resp.Summary = strings.Join(parts, " · ")
	return resp
}

func worseReach(a, b probe.Reachability) probe.Reachability {
	order := map[probe.Reachability]int{
		probe.ReachOK:       0,
		probe.ReachUnknown:  1,
		probe.ReachDegraded: 2,
		probe.ReachFail:     3,
	}
	if order[b] > order[a] {
		return b
	}
	return a
}

func (s *Service) readDeployment(ctx context.Context, name string) DeploymentInfo {
	info := DeploymentInfo{
		Namespace: pluginNamespace,
		Name:      name,
		Ready:     "0/0",
		Reach:     probe.ReachUnknown,
	}
	if s.cluster == nil {
		info.Reach = probe.ReachFail
		info.Detail = "cluster service unavailable"
		return info
	}
	clientset, _, err := s.cluster.KubernetesClient()
	if err != nil {
		info.Reach = probe.ReachFail
		info.Detail = err.Error()
		return info
	}
	deploy, err := clientset.AppsV1().Deployments(pluginNamespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		info.Reach = probe.ReachFail
		info.Detail = err.Error()
		return info
	}
	replicas := int32(0)
	if deploy.Spec.Replicas != nil {
		replicas = *deploy.Spec.Replicas
	}
	info.Ready = fmt.Sprintf("%d/%d", deploy.Status.ReadyReplicas, replicas)
	info.Reach = probe.ReachOK
	info.Detail = "deployment ready"
	if deploy.Status.ReadyReplicas < replicas {
		info.Reach = probe.ReachDegraded
		info.Detail = "deployment progressing"
	}
	if replicas > 0 && deploy.Status.AvailableReplicas == 0 {
		info.Reach = probe.ReachFail
		info.Detail = "deployment unavailable"
	}
	return info
}

type healthPayload struct {
	Status      string  `json:"status"`
	Pool        string  `json:"pool"`
	LastClaimAt string  `json:"last_claim_at"`
	JobsDone    int     `json:"jobs_done"`
	JobsFailed  int     `json:"jobs_failed"`
	UptimeSec   float64 `json:"uptime_sec"`
}

// probePoolHealth prefers K8s API service proxy for in-cluster DNS URLs so local
// Mac platform-api (with kubeconfig) can reach plugin-market-data health without
// port-forward. Explicit MARKET_DATA_*_HEALTH_URL overrides use plain HTTP.
func (s *Service) probePoolHealth(ctx context.Context, url, serviceName string) (*WorkerInfo, probe.Reachability, string) {
	useKubeProxy := strings.Contains(url, ".svc.cluster.local") && s.cluster != nil
	if useKubeProxy {
		worker, reach, errMsg := s.probeHealthViaKube(ctx, serviceName)
		if reach != probe.ReachFail {
			return worker, reach, errMsg
		}
		// Fall through to direct HTTP (works when platform-api runs in-cluster).
	}
	return s.probeHealth(ctx, url)
}

func (s *Service) probeHealthViaKube(ctx context.Context, serviceName string) (*WorkerInfo, probe.Reachability, string) {
	if s.cluster == nil {
		return nil, probe.ReachFail, "cluster service unavailable"
	}
	clientset, _, err := s.cluster.KubernetesClient()
	if err != nil {
		return nil, probe.ReachFail, err.Error()
	}
	path := strings.TrimPrefix(healthPath, "/")
	raw, err := clientset.CoreV1().Services(pluginNamespace).ProxyGet(
		"http", serviceName, healthServicePort, path, nil,
	).DoRaw(ctx)
	if err != nil {
		return nil, probe.ReachFail, err.Error()
	}
	return parseHealthBody(raw)
}

func (s *Service) probeHealth(ctx context.Context, url string) (*WorkerInfo, probe.Reachability, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, probe.ReachFail, err.Error()
	}
	res, err := s.client.Do(req)
	if err != nil {
		return nil, probe.ReachFail, err.Error()
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 64*1024))
	if res.StatusCode != http.StatusOK {
		return nil, probe.ReachFail, fmt.Sprintf("HTTP %d", res.StatusCode)
	}
	return parseHealthBody(body)
}

func parseHealthBody(body []byte) (*WorkerInfo, probe.Reachability, string) {
	var payload healthPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, probe.ReachDegraded, "health JSON unparseable"
	}
	worker := &WorkerInfo{
		Pool:       payload.Pool,
		Status:     payload.Status,
		JobsDone:   payload.JobsDone,
		JobsFailed: payload.JobsFailed,
		UptimeSec:  payload.UptimeSec,
		LastClaim:  payload.LastClaimAt,
	}
	if strings.EqualFold(payload.Status, "ok") || payload.Status == "" {
		return worker, probe.ReachOK, ""
	}
	return worker, probe.ReachDegraded, "worker status=" + payload.Status
}

func (s *Service) probeFreshness(ctx context.Context) ([]FreshnessInfo, probe.Reachability, string) {
	if s.freshnessProbe != nil {
		return s.freshnessProbe(ctx)
	}
	if s.cluster == nil {
		return nil, probe.ReachUnknown, "cluster service unavailable"
	}
	db := s.cfg.FreshnessDB
	if db == "" {
		db = defaultFreshnessDB
	}
	sql := "SELECT dimension, COALESCE(last_run_at::text,''), COALESCE(rows_written,0), COALESCE(status,'unknown') FROM ops_jobs.ingest_freshness ORDER BY dimension"
	out, err := s.cluster.ExecSQLOnPrimary(ctx, db, sql)
	if err != nil {
		return nil, probe.ReachFail, err.Error()
	}
	rows := parseFreshnessOutput(out, time.Now().UTC())
	if len(rows) == 0 {
		return rows, probe.ReachUnknown, "no ingest_freshness rows"
	}
	reach := probe.ReachOK
	for _, r := range rows {
		switch r.Verdict {
		case "stale":
			reach = worseReach(reach, probe.ReachDegraded)
		case "unknown":
			reach = worseReach(reach, probe.ReachUnknown)
		}
	}
	return rows, reach, ""
}

// probeReadinessRollup calls Plugin quality endpoints (not Trade stock_readiness_daily).
// Golden Source owns market.stock_snapshot; Trade DB no longer has a usable readiness KPI table.
func (s *Service) probeReadinessRollup(ctx context.Context) *ReadinessRollup {
	if s.readinessProbe != nil {
		return s.readinessProbe(ctx)
	}
	covBody, err := s.fetchPluginJSON(ctx, "/market/readiness/snapshot-coverage")
	if err != nil {
		return nil
	}
	var cov struct {
		OK                bool   `json:"ok"`
		RowCount          int    `json:"row_count"`
		SessionDate       string `json:"session_date"`
		LastFetchedAt     string `json:"last_fetched_at"`
		ByInstrumentType  []struct {
			Code                string `json:"code"`
			SnapshotRowCount    int    `json:"snapshot_row_count"`
			UniverseTickerCount int    `json:"universe_ticker_count"`
		} `json:"by_instrument_type"`
	}
	if err := json.Unmarshal(covBody, &cov); err != nil {
		return nil
	}
	universe := 0
	covered := 0
	for _, row := range cov.ByInstrumentType {
		universe += row.UniverseTickerCount
		covered += row.SnapshotRowCount
	}
	asOf := strings.TrimSpace(cov.SessionDate)
	if asOf == "" {
		asOf = strings.TrimSpace(cov.LastFetchedAt)
	}
	if asOf == "" && cov.RowCount == 0 && universe == 0 {
		return nil
	}

	gapCount := 0
	// Prefer detail rows so zero-close SPACs are not counted as actionable gaps
	// (matches Ops Console Readiness; works before Plugin image excludes them).
	if gapBody, err := s.fetchPluginJSON(ctx, "/market/readiness/vendor-gap?detail=true&limit=500"); err == nil {
		var gap struct {
			GapCount int `json:"gap_count"`
			Gaps     []struct {
				SnapshotClose *float64 `json:"snapshot_close"`
			} `json:"gaps"`
		}
		if json.Unmarshal(gapBody, &gap) == nil {
			if len(gap.Gaps) > 0 && len(gap.Gaps) >= gap.GapCount {
				actionable := 0
				for _, row := range gap.Gaps {
					if row.SnapshotClose != nil && math.Abs(*row.SnapshotClose) >= 1e-9 {
						actionable++
					}
				}
				gapCount = actionable
			} else {
				gapCount = gap.GapCount
			}
		}
	} else if gapBody, err := s.fetchPluginJSON(ctx, "/market/readiness/vendor-gap"); err == nil {
		var gap struct {
			GapCount int `json:"gap_count"`
		}
		if json.Unmarshal(gapBody, &gap) == nil {
			gapCount = gap.GapCount
		}
	}

	return &ReadinessRollup{
		Universe:        universe,
		SnapshotRows:    cov.RowCount,
		SnapshotCovered: covered,
		VendorGapCount:  gapCount,
		AsOf:            asOf,
		Source:          "plugin",
	}
}

// fetchPluginJSON GETs a Plugin API path via MARKET_DATA_API_URL or K8s service proxy.
func (s *Service) fetchPluginJSON(ctx context.Context, pluginPath string) ([]byte, error) {
	pluginPath = "/" + strings.TrimPrefix(pluginPath, "/")
	if base := strings.TrimRight(strings.TrimSpace(s.cfg.APIBaseURL), "/"); base != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+pluginPath, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		client := &http.Client{Timeout: proxyTimeout}
		res, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		if res.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("plugin HTTP %d", res.StatusCode)
		}
		return body, nil
	}
	if s.cluster == nil {
		return nil, fmt.Errorf("cluster service unavailable")
	}
	clientset, _, err := s.cluster.KubernetesClient()
	if err != nil {
		return nil, err
	}
	rawPath := strings.TrimPrefix(pluginPath, "/")
	return clientset.CoreV1().Services(pluginNamespace).ProxyGet(
		"http", apiServiceName, apiServicePort, rawPath, nil,
	).DoRaw(ctx)
}

func parseReadinessRollupOutput(out string) *ReadinessRollup {
	// Legacy Trade SQL pipe parser retained for older unit fixtures; prefer Plugin JSON path.
	line := strings.TrimSpace(out)
	if line == "" {
		return nil
	}
	parts := strings.Split(line, "|")
	if len(parts) < 4 {
		return nil
	}
	universe, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	covered, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	gapCount, err3 := strconv.Atoi(strings.TrimSpace(parts[2]))
	asOf := strings.TrimSpace(parts[3])
	if err1 != nil || err2 != nil || err3 != nil {
		return nil
	}
	if asOf == "" || strings.EqualFold(asOf, "null") {
		return nil
	}
	return &ReadinessRollup{
		Universe:        universe,
		SnapshotRows:    covered,
		SnapshotCovered: covered,
		VendorGapCount:  gapCount,
		AsOf:            asOf,
		Source:          "legacy",
	}
}

func parseFreshnessOutput(out string, now time.Time) []FreshnessInfo {
	var rows []FreshnessInfo
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 4 {
			continue
		}
		dim := strings.TrimSpace(parts[0])
		lastRaw := strings.TrimSpace(parts[1])
		rowsWritten, _ := strconv.Atoi(strings.TrimSpace(parts[2]))
		status := strings.TrimSpace(parts[3])
		info := FreshnessInfo{
			Dimension:   dim,
			RowsWritten: rowsWritten,
			Status:      status,
			Verdict:     "unknown",
		}
		if lastRaw != "" && !strings.EqualFold(lastRaw, "null") {
			if ts, err := parseFreshnessTimestamp(lastRaw); err == nil {
				info.LastRunAt = ts.UTC().Format(time.RFC3339)
				age := now.Sub(ts.UTC()).Hours()
				if age < 0 {
					age = 0
				}
				info.AgeHours = age
				if strings.EqualFold(status, "ok") && age < freshnessMaxAgeH {
					info.Verdict = "ok"
				} else {
					info.Verdict = "stale"
				}
			}
		}
		rows = append(rows, info)
	}
	return rows
}

func parseFreshnessTimestamp(raw string) (time.Time, error) {
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
	return time.Time{}, fmt.Errorf("unrecognized timestamp")
}
