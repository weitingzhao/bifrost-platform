package selfhealth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/gitops"
)

type ProbeStatus string

const (
	StatusOK      ProbeStatus = "ok"
	StatusDegraded ProbeStatus = "degraded"
	StatusFail    ProbeStatus = "fail"
	StatusUnknown ProbeStatus = "unknown"
)

type SelfHealthProbe struct {
	ID        string      `json:"id"`
	Category  string      `json:"category"`
	Env       string      `json:"env"`
	URL       string      `json:"url,omitempty"`
	Status    ProbeStatus `json:"status"`
	Detail    string      `json:"detail"`
	LatencyMs int64       `json:"latency_ms"`
}

type SelfHealthResponse struct {
	GeneratedAt time.Time         `json:"generated_at"`
	Probes      []SelfHealthProbe `json:"probes"`
	Overall     ProbeStatus       `json:"overall"`
}

type Service struct {
	cfg       *config.Config
	gitopsSvc *gitops.Service
	client    *http.Client
}

func NewService(cfg *config.Config, gitopsSvc *gitops.Service) *Service {
	return &Service{
		cfg:       cfg,
		gitopsSvc: gitopsSvc,
		client:    &http.Client{Timeout: 6 * time.Second},
	}
}

func (s *Service) Probe(ctx context.Context) SelfHealthResponse {
	entry := s.cfg.DefaultCluster()
	var probes []SelfHealthProbe
	var mu sync.Mutex
	var wg sync.WaitGroup

	type target struct {
		id, category, env, url string
	}

	var targets []target

	if entry != nil {
		if u := strings.TrimSpace(entry.StgSmoke.PlatformAPIHealthURL); u != "" {
			targets = append(targets, target{"platform-api-stg", "api", "stg", u})
		}
		if u := strings.TrimSpace(entry.StgSmoke.PlatformConsoleURL); u != "" {
			targets = append(targets, target{"platform-console-stg", "console", "stg", u})
		}
		if u := strings.TrimSpace(entry.ProdSmoke.PlatformAPIHealthURL); u != "" {
			targets = append(targets, target{"platform-api-prod", "api", "prod", u})
		}
		if u := strings.TrimSpace(entry.ProdSmoke.PlatformConsoleURL); u != "" {
			targets = append(targets, target{"platform-console-prod", "console", "prod", u})
		}
	}

	for _, t := range targets {
		wg.Add(1)
		go func(tgt target) {
			defer wg.Done()
			p := s.probeHTTP(ctx, tgt.id, tgt.category, tgt.env, tgt.url)
			mu.Lock()
			probes = append(probes, p)
			mu.Unlock()
		}(t)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		argoProbes := s.probeArgo(ctx)
		mu.Lock()
		probes = append(probes, argoProbes...)
		mu.Unlock()
	}()

	wg.Wait()

	sortProbes(probes)
	overall := computeOverall(probes)

	return SelfHealthResponse{
		GeneratedAt: time.Now().UTC(),
		Probes:      probes,
		Overall:     overall,
	}
}

func (s *Service) probeHTTP(ctx context.Context, id, category, env, url string) SelfHealthProbe {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return SelfHealthProbe{
			ID: id, Category: category, Env: env, URL: url,
			Status: StatusFail, Detail: "request error: " + err.Error(),
		}
	}
	resp, err := s.client.Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return SelfHealthProbe{
			ID: id, Category: category, Env: env, URL: url,
			Status: StatusFail, Detail: err.Error(), LatencyMs: latency,
		}
	}
	defer resp.Body.Close()

	status := StatusOK
	detail := fmt.Sprintf("HTTP %d", resp.StatusCode)
	if resp.StatusCode == 503 {
		status = StatusDegraded
		detail = fmt.Sprintf("HTTP %d (starting or degraded)", resp.StatusCode)
	} else if resp.StatusCode >= 400 {
		status = StatusFail
	}
	return SelfHealthProbe{
		ID: id, Category: category, Env: env, URL: url,
		Status: status, Detail: detail, LatencyMs: latency,
	}
}

func (s *Service) probeArgo(ctx context.Context) []SelfHealthProbe {
	if s.gitopsSvc == nil {
		return []SelfHealthProbe{
			{ID: "argo-platform-stg", Category: "gitops", Env: "stg", Status: StatusUnknown, Detail: "gitops service not available"},
			{ID: "argo-platform-prod", Category: "gitops", Env: "prod", Status: StatusUnknown, Detail: "gitops service not available"},
		}
	}
	appsResp := s.gitopsSvc.Apps(ctx)

	platformApps := map[string]string{
		"bifrost-platform-stg":  "stg",
		"bifrost-platform-prod": "prod",
	}
	found := map[string]bool{}
	var probes []SelfHealthProbe

	for _, app := range appsResp.Apps {
		env, ok := platformApps[app.Name]
		if !ok {
			continue
		}
		found[app.Name] = true
		status := StatusOK
		detail := fmt.Sprintf("%s, %s", app.SyncStatus, app.HealthStatus)
		if app.SyncStatus != "Synced" {
			status = StatusDegraded
		}
		if app.HealthStatus != "Healthy" {
			if app.HealthStatus == "Degraded" || app.HealthStatus == "Missing" {
				status = StatusFail
			} else {
				status = StatusDegraded
			}
		}
		probes = append(probes, SelfHealthProbe{
			ID: "argo-" + app.Name, Category: "gitops", Env: env,
			Status: status, Detail: detail,
		})
	}

	for name, env := range platformApps {
		if !found[name] {
			probes = append(probes, SelfHealthProbe{
				ID: "argo-" + name, Category: "gitops", Env: env,
				Status: StatusUnknown, Detail: "Application not found",
			})
		}
	}
	return probes
}

func computeOverall(probes []SelfHealthProbe) ProbeStatus {
	if len(probes) == 0 {
		return StatusUnknown
	}
	hasDegraded := false
	for _, p := range probes {
		if p.Status == StatusFail {
			return StatusFail
		}
		if p.Status == StatusDegraded || p.Status == StatusUnknown {
			hasDegraded = true
		}
	}
	if hasDegraded {
		return StatusDegraded
	}
	return StatusOK
}

func sortProbes(probes []SelfHealthProbe) {
	order := map[string]int{"api": 0, "console": 1, "gitops": 2}
	envOrder := map[string]int{"stg": 0, "prod": 1}
	for i := 0; i < len(probes); i++ {
		for j := i + 1; j < len(probes); j++ {
			ci, cj := order[probes[i].Category], order[probes[j].Category]
			if ci > cj || (ci == cj && envOrder[probes[i].Env] > envOrder[probes[j].Env]) {
				probes[i], probes[j] = probes[j], probes[i]
			}
		}
	}
}

// Handler — HTTP handler for /api/v1/self-health.
type Handler struct {
	svc *Service
}

func NewHandler(cfg *config.Config, gitopsSvc *gitops.Service) *Handler {
	return &Handler{svc: NewService(cfg, gitopsSvc)}
}

func (h *Handler) HandleSelfHealth(w http.ResponseWriter, r *http.Request) {
	resp := h.svc.Probe(r.Context())
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}
