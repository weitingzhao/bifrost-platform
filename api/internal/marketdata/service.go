package marketdata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
		Reachable:    false,
		Reachability: probe.ReachUnknown,
		HealthReach:  probe.ReachUnknown,
		Autonomy:     "L0",
		Deployments:  []DeploymentInfo{},
		GeneratedAt:  now,
	}

	stocks := s.readDeployment(ctx, stocksDeployName)
	options := s.readDeployment(ctx, optionsDeployName)
	resp.Deployments = []DeploymentInfo{stocks, options}

	workers := make([]WorkerInfo, 0, 2)
	healthErrors := make([]string, 0, 2)
	healthReach := probe.ReachOK

	for _, url := range []string{s.cfg.StocksHealthURL, s.cfg.OptionsHealthURL} {
		worker, reach, errMsg := s.probeHealth(ctx, url)
		if worker != nil {
			workers = append(workers, *worker)
		}
		if errMsg != "" {
			healthErrors = append(healthErrors, errMsg)
		}
		healthReach = worseReach(healthReach, reach)
	}
	resp.Workers = workers
	resp.HealthReach = healthReach
	if healthReach == probe.ReachFail && len(healthErrors) > 0 {
		resp.Error = strings.Join(healthErrors, "; ")
		resp.Hint = "Ensure plugin-market-data NS is applied and per-pool health Services are reachable"
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
	case readyCount < total || healthReach == probe.ReachFail:
		resp.Reachability = probe.ReachDegraded
		resp.Reachable = readyCount > 0
	case healthReach == probe.ReachDegraded || healthReach == probe.ReachUnknown:
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
