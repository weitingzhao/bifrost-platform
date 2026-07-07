package telemetry

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

type Service struct {
	cfg *config.Config
}

func NewService(cfg *config.Config) *Service {
	return &Service{cfg: cfg}
}

func (s *Service) prometheusURL() string {
	if s.cfg == nil {
		return ""
	}
	entry := s.cfg.DefaultCluster()
	if entry == nil {
		return ""
	}
	return entry.PrometheusURL()
}

func (s *Service) Overview(ctx context.Context, namespace string) (OverviewResponse, error) {
	ns := ResolveNamespace(namespace)
	promURL := s.prometheusURL()
	if promURL == "" {
		return OverviewResponse{}, ErrPrometheusNotConfigured
	}

	client := NewClient(promURL)
	queries := PresetQueries()
	metrics := make([]MetricResult, len(queries))
	var wg sync.WaitGroup
	for i, spec := range queries {
		wg.Add(1)
		go func(idx int, q QuerySpec) {
			defer wg.Done()
			metrics[idx] = s.runQuery(ctx, client, q, ns)
		}(i, spec)
	}
	wg.Wait()

	resp := OverviewResponse{
		Namespace:     ns,
		PrometheusURL: promURL,
		Metrics:       metrics,
		GeneratedAt:   time.Now().UTC(),
	}
	return resp, nil
}

func (s *Service) Query(ctx context.Context, queryID, namespace string) (QueryResponse, error) {
	spec, ok := FindQuery(queryID)
	if !ok {
		return QueryResponse{}, ErrUnknownQuery
	}
	ns := ResolveNamespace(namespace)
	promURL := s.prometheusURL()
	if promURL == "" {
		return QueryResponse{}, ErrPrometheusNotConfigured
	}
	client := NewClient(promURL)
	metric := s.runQuery(ctx, client, spec, ns)
	return QueryResponse{
		Namespace: ns,
		QueryID:   spec.ID,
		Metric:    metric,
	}, nil
}

func (s *Service) PromQL(ctx context.Context, promQL string) (PromQLResponse, error) {
	q := strings.TrimSpace(promQL)
	if q == "" {
		return PromQLResponse{}, ErrMissingPromQL
	}
	promURL := s.prometheusURL()
	if promURL == "" {
		return PromQLResponse{}, ErrPrometheusNotConfigured
	}
	client := NewClient(promURL)
	points, err := client.QueryInstant(ctx, q)
	if err != nil {
		return PromQLResponse{}, err
	}
	resultType := "vector"
	if len(points) == 0 {
		resultType = "vector"
	}
	return PromQLResponse{
		PrometheusURL: promURL,
		Query:         q,
		ResultType:    resultType,
		Points:        points,
		GeneratedAt:   time.Now().UTC(),
	}, nil
}

func (s *Service) Alerts(ctx context.Context) (AlertsResponse, error) {
	promURL := s.prometheusURL()
	if promURL == "" {
		return AlertsResponse{}, ErrPrometheusNotConfigured
	}
	client := NewClient(promURL)
	alerts, err := client.QueryAlerts(ctx)
	if err != nil {
		return AlertsResponse{}, err
	}
	return AlertsResponse{
		PrometheusURL: promURL,
		Alerts:        alerts,
		GeneratedAt:   time.Now().UTC(),
	}, nil
}

func (s *Service) Targets(ctx context.Context, state string) (TargetsResponse, error) {
	promURL := s.prometheusURL()
	if promURL == "" {
		return TargetsResponse{}, ErrPrometheusNotConfigured
	}
	client := NewClient(promURL)
	resp, err := client.QueryTargets(ctx, strings.TrimSpace(state))
	if err != nil {
		return TargetsResponse{}, err
	}
	resp.PrometheusURL = promURL
	resp.GeneratedAt = time.Now().UTC()
	return resp, nil
}

func (s *Service) runQuery(ctx context.Context, client *Client, spec QuerySpec, ns string) MetricResult {
	points, err := client.QueryInstant(ctx, spec.Build(ns))
	if err != nil {
		return MetricResult{
			ID:     spec.ID,
			Title:  spec.Title,
			Unit:   spec.Unit,
			Status: "error",
			Detail: err.Error(),
			Points: []SamplePoint{},
		}
	}
	status := "ok"
	detail := ""
	if len(points) == 0 {
		status = "empty"
		detail = "no series returned (targets may still be scraping)"
	}
	return MetricResult{
		ID:     spec.ID,
		Title:  spec.Title,
		Unit:   spec.Unit,
		Status: status,
		Detail: detail,
		Points: points,
	}
}
