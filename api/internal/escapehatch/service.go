package escapehatch

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

const runbookVersion = "2026-07-07.1"

type Service struct {
	cfg    *config.Config
	store  *Store
	client *http.Client
}

func NewService(cfg *config.Config, store *Store) *Service {
	return &Service{
		cfg:    cfg,
		store:  store,
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

func (s *Service) Snapshot(ctx context.Context) (Response, error) {
	rec, err := s.store.LoadDrill()
	if err != nil {
		return Response{}, err
	}

	localAPI := envOr("ESCAPE_HATCH_LOCAL_API_URL", "http://127.0.0.1:8780/health")
	localConsole := envOr("ESCAPE_HATCH_LOCAL_CONSOLE_URL", "http://127.0.0.1:5180/")

	var routes []EscapeRoute
	var mu sync.Mutex
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		probes := []RouteProbe{
			s.probeHTTP(ctx, "local-api", "Platform API", localAPI),
			s.probeHTTP(ctx, "local-console", "Platform Console", localConsole),
		}
		st, detail := aggregateProbes(probes)
		mu.Lock()
		routes = append(routes, EscapeRoute{
			ID:      "local-make-start",
			Label:   "Local dev bypass (make start)",
			Layer:   "L1",
			Summary: "Bypass cluster — run platform-api + console on Mac via make start (:8780 / :5180).",
			Command: "cd bifrost-platform && make start",
			Status:  st,
			Detail:  detail,
			Probes:  probes,
			RunbookRefs: []string{
				"cicdBootstrapCatalog.ts · L1 recoveryPath ①",
				"scripts/run_platform.py",
			},
		})
		mu.Unlock()
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		var probes []RouteProbe
		entry := s.cfg.DefaultCluster()
		if entry != nil {
			if u := strings.TrimSpace(entry.StgSmoke.PlatformAPIHealthURL); u != "" {
				probes = append(probes, s.probeHTTP(ctx, "nodeport-api-stg", "Platform API STG", u))
			}
			if u := strings.TrimSpace(entry.StgSmoke.PlatformConsoleURL); u != "" {
				probes = append(probes, s.probeHTTP(ctx, "nodeport-console-stg", "Platform Console STG", u))
			}
			if u := strings.TrimSpace(entry.ProdSmoke.PlatformAPIHealthURL); u != "" {
				probes = append(probes, s.probeHTTP(ctx, "nodeport-api-prod", "Platform API PROD", u))
			}
			if u := strings.TrimSpace(entry.ProdSmoke.PlatformConsoleURL); u != "" {
				probes = append(probes, s.probeHTTP(ctx, "nodeport-console-prod", "Platform Console PROD", u))
			}
		}
		if len(probes) == 0 {
			mu.Lock()
			routes = append(routes, EscapeRoute{
				ID:      "cluster-nodeport",
				Label:   "Cluster NodePort (STG/PROD)",
				Layer:   "L1",
				Summary: "Reach platform via K3s NodePort when in-cluster Deployments are healthy.",
				Status:  RouteUnknown,
				Detail:  "No NodePort URLs configured in environments.yaml",
				RunbookRefs: []string{
					"config/environments.yaml · stg_smoke / prod_smoke platform URLs",
				},
			})
			mu.Unlock()
			return
		}
		st, detail := aggregateProbes(probes)
		mu.Lock()
		routes = append(routes, EscapeRoute{
			ID:      "cluster-nodeport",
			Label:   "Cluster NodePort (STG/PROD)",
			Layer:   "L1",
			Summary: "In-cluster platform-api + console via NodePort (normal ops path).",
			Status:  st,
			Detail:  detail,
			Probes:  probes,
			RunbookRefs: []string{
				"SelfHealthPanel · GET /api/v1/self-health",
			},
		})
		mu.Unlock()
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		kc := strings.TrimSpace(os.Getenv("PLATFORM_KUBECONFIG"))
		if kc == "" {
			kc = strings.TrimSpace(os.Getenv("KUBECONFIG"))
		}
		st := RouteDocumented
		detail := "Runbook documented — verify kubeconfig on operator host before drill"
		if kc != "" {
			kc = os.ExpandEnv(kc)
			if strings.HasPrefix(kc, "~") {
				home, _ := os.UserHomeDir()
				kc = strings.Replace(kc, "~", home, 1)
			}
			if info, err := os.Stat(kc); err == nil && !info.IsDir() {
				st = RouteOK
				detail = fmt.Sprintf("kubeconfig present (%s)", kc)
			} else {
				st = RouteDegraded
				detail = fmt.Sprintf("PLATFORM_KUBECONFIG set but not readable: %s", kc)
			}
		}
		mu.Lock()
		routes = append(routes, EscapeRoute{
			ID:      "kubectl-overlay",
			Label:   "kubectl apply overlay (bypass pipeline)",
			Layer:   "L0/L1",
			Summary: "Owner applies Kustomize overlay directly when Tekton/Argo path is broken.",
			Command: "kubectl apply -k bifrost-trade-infra/k8s/overlays/platform-stg",
			Status:  st,
			Detail:  detail,
			RunbookRefs: []string{
				"cicdBootstrapCatalog.ts · L1 recoveryPath ②",
				"k3sBootstrapCatalog.ts",
			},
		})
		mu.Unlock()
	}()

	wg.Wait()

	sortRoutes(routes)
	overall := computeOverall(routes)
	guidance := buildGuidance(routes, quarterlyFromRecord(rec))

	return Response{
		GeneratedAt:    time.Now().UTC(),
		RunbookVersion: runbookVersion,
		Overall:        overall,
		Routes:         routes,
		Quarterly:      quarterlyFromRecord(rec),
		AgentGuidance:  guidance,
	}, nil
}

func (s *Service) RecordDrill(by, notes string, routeIDs []string) (*DrillRecord, error) {
	rec := DrillRecord{
		At:       time.Now().UTC(),
		By:       strings.TrimSpace(by),
		Notes:    strings.TrimSpace(notes),
		RouteIDs: routeIDs,
	}
	if rec.By == "" {
		rec.By = "owner"
	}
	if err := s.store.SaveDrill(rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

func (s *Service) probeHTTP(ctx context.Context, id, label, url string) RouteProbe {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return RouteProbe{ID: id, Label: label, URL: url, Status: RouteFail, Detail: err.Error()}
	}
	resp, err := s.client.Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return RouteProbe{ID: id, Label: label, URL: url, Status: RouteFail, Detail: err.Error(), LatencyMs: latency}
	}
	defer resp.Body.Close()
	st := RouteOK
	detail := fmt.Sprintf("HTTP %d", resp.StatusCode)
	if resp.StatusCode >= 500 {
		st = RouteFail
	} else if resp.StatusCode >= 400 {
		st = RouteDegraded
	}
	return RouteProbe{ID: id, Label: label, URL: url, Status: st, Detail: detail, LatencyMs: latency}
}

func aggregateProbes(probes []RouteProbe) (RouteStatus, string) {
	if len(probes) == 0 {
		return RouteUnknown, "no probes"
	}
	ok, fail := 0, 0
	parts := make([]string, 0, len(probes))
	for _, p := range probes {
		parts = append(parts, fmt.Sprintf("%s=%s", p.Label, p.Status))
		switch p.Status {
		case RouteOK:
			ok++
		case RouteFail:
			fail++
		}
	}
	if fail == len(probes) {
		return RouteFail, strings.Join(parts, " · ")
	}
	if ok == len(probes) {
		return RouteOK, strings.Join(parts, " · ")
	}
	return RouteDegraded, strings.Join(parts, " · ")
}

func computeOverall(routes []EscapeRoute) RouteStatus {
	hasDoc := false
	hasDegraded := false
	for _, r := range routes {
		switch r.Status {
		case RouteFail:
			return RouteFail
		case RouteDegraded, RouteUnknown:
			hasDegraded = true
		case RouteDocumented:
			hasDoc = true
		}
	}
	if hasDegraded {
		return RouteDegraded
	}
	if hasDoc {
		return RouteDegraded
	}
	return RouteOK
}

func buildGuidance(routes []EscapeRoute, q QuarterlyDrill) string {
	var parts []string
	parts = append(parts, "L1 escape hatch: two independent recovery paths when cluster pipeline/Console is down.")
	for _, r := range routes {
		if r.Status == RouteFail {
			parts = append(parts, fmt.Sprintf("Route %s FAILED — %s", r.ID, r.Detail))
		}
	}
	if q.Overdue {
		parts = append(parts, fmt.Sprintf("Quarterly drill OVERDUE (interval %dd) — record via Console or POST /api/v1/platform/escape-hatch/drill", q.IntervalDays))
	}
	return strings.Join(parts, " ")
}

func sortRoutes(routes []EscapeRoute) {
	order := map[string]int{"local-make-start": 0, "cluster-nodeport": 1, "kubectl-overlay": 2}
	for i := 0; i < len(routes); i++ {
		for j := i + 1; j < len(routes); j++ {
			if order[routes[i].ID] > order[routes[j].ID] {
				routes[i], routes[j] = routes[j], routes[i]
			}
		}
	}
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
