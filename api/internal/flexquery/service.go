package flexquery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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

	apiDeploy := s.readDeployment(ctx, apiDeployName)
	workerDeploy := s.readDeployment(ctx, workerDeployName)
	resp.Deployments = []DeploymentInfo{apiDeploy, workerDeploy}

	worker, healthReach, healthErr := s.probePoolHealth(ctx, s.cfg.HealthURL, healthServiceName)
	resp.HealthReach = healthReach
	if worker != nil {
		resp.Workers = []WorkerInfo{*worker}
	}
	if healthErr != "" {
		resp.Error = healthErr
		resp.Hint = "Ensure plugin-flex-query NS is applied and flex-query-health is reachable"
	}

	freshRows, freshReach, freshErr := s.probeFreshness(ctx)
	resp.Freshness = freshRows
	resp.FreshnessReach = freshReach
	if freshErr != "" && resp.Error == "" && freshReach != probe.ReachUnknown {
		resp.Error = freshErr
		resp.Hint = "Ensure ops_jobs.flex_ingest_freshness is populated (worker jobs marking done)"
	}

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
	Status      string `json:"status"`
	Pool        string `json:"pool"`
	LastClaimAt string `json:"last_claim_at"`
	JobsDone    int    `json:"jobs_done"`
	JobsFailed  int    `json:"jobs_failed"`
}

func (s *Service) probePoolHealth(ctx context.Context, url, serviceName string) (*WorkerInfo, probe.Reachability, string) {
	useKubeProxy := strings.Contains(url, ".svc.cluster.local") && s.cluster != nil
	if useKubeProxy {
		worker, reach, errMsg := s.probeHealthViaKube(ctx, serviceName)
		if reach != probe.ReachFail {
			return worker, reach, errMsg
		}
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
		LastClaim:  payload.LastClaimAt,
	}
	if strings.EqualFold(payload.Status, "ok") || payload.Status == "" {
		return worker, probe.ReachOK, ""
	}
	return worker, probe.ReachDegraded, "worker status=" + payload.Status
}

func (s *Service) probeFreshness(ctx context.Context) ([]FreshnessInfo, probe.Reachability, string) {
	if s.cluster == nil {
		return nil, probe.ReachUnknown, "cluster service unavailable"
	}
	db := s.cfg.FreshnessDB
	if db == "" {
		db = defaultFreshnessDB
	}
	sql := "SELECT dimension, COALESCE(latest_ts::text,''), COALESCE(row_count,0), 'ok' FROM ops_jobs.flex_ingest_freshness ORDER BY dimension"
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
