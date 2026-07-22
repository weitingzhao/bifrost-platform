package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultTimeout = 10 * time.Second

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
	}
}

type promAPIResponse struct {
	Status    string          `json:"status"`
	Error     string          `json:"error"`
	ErrorType string          `json:"errorType"`
	Data      promAPIData     `json:"data"`
	Raw       json.RawMessage `json:"-"`
}

type promAPIData struct {
	ResultType string            `json:"resultType"`
	Result     []promVectorEntry `json:"result"`
}

type promVectorEntry struct {
	Metric map[string]string `json:"metric"`
	Value  []any             `json:"value"`
}

func (c *Client) QueryInstant(ctx context.Context, promQL string) ([]SamplePoint, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("prometheus url not configured")
	}
	endpoint := c.baseURL + "/api/v1/query?" + url.Values{"query": {promQL}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("prometheus HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	var parsed promAPIResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	if parsed.Status != "success" {
		if parsed.Error != "" {
			return nil, fmt.Errorf("%s: %s", parsed.ErrorType, parsed.Error)
		}
		return nil, fmt.Errorf("prometheus query failed")
	}
	return vectorToPoints(parsed.Data.Result), nil
}

type promAlertsResponse struct {
	Status string `json:"status"`
	Data   struct {
		Alerts []struct {
			Labels      map[string]string `json:"labels"`
			Annotations map[string]string `json:"annotations"`
			State       string            `json:"state"`
			ActiveAt    string            `json:"activeAt"`
			Value       string            `json:"value"`
		} `json:"alerts"`
	} `json:"data"`
	Error     string `json:"error"`
	ErrorType string `json:"errorType"`
}

type promTargetsResponse struct {
	Status string `json:"status"`
	Data   struct {
		ActiveTargets  []promTargetEntry `json:"activeTargets"`
		DroppedTargets []promTargetEntry `json:"droppedTargets"`
	} `json:"data"`
	Error     string `json:"error"`
	ErrorType string `json:"errorType"`
}

type promTargetEntry struct {
	Labels             map[string]string `json:"labels"`
	DiscoveredLabels   map[string]string `json:"discoveredLabels"`
	ScrapePool         string            `json:"scrapePool"`
	ScrapeURL          string            `json:"scrapeUrl"`
	Health             string            `json:"health"`
	LastError          string            `json:"lastError"`
	LastScrape         string            `json:"lastScrape"`
	LastScrapeDuration float64           `json:"lastScrapeDuration"`
}

func (c *Client) getJSON(ctx context.Context, path string, dest any) error {
	if c.baseURL == "" {
		return fmt.Errorf("prometheus url not configured")
	}
	endpoint := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("prometheus HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	if err := json.Unmarshal(body, dest); err != nil {
		return err
	}
	return nil
}

func (c *Client) QueryAlerts(ctx context.Context) ([]AlertEntry, error) {
	var parsed promAlertsResponse
	if err := c.getJSON(ctx, "/api/v1/alerts", &parsed); err != nil {
		return nil, err
	}
	if parsed.Status != "success" {
		if parsed.Error != "" {
			return nil, fmt.Errorf("%s: %s", parsed.ErrorType, parsed.Error)
		}
		return nil, fmt.Errorf("prometheus alerts query failed")
	}
	out := make([]AlertEntry, 0, len(parsed.Data.Alerts))
	for _, a := range parsed.Data.Alerts {
		out = append(out, AlertEntry{
			Labels:      a.Labels,
			Annotations: a.Annotations,
			State:       a.State,
			ActiveAt:    a.ActiveAt,
			Value:       a.Value,
		})
	}
	return out, nil
}

func (c *Client) QueryTargets(ctx context.Context, state string) (TargetsResponse, error) {
	path := "/api/v1/targets"
	if state != "" && state != "any" {
		path += "?" + url.Values{"state": {state}}.Encode()
	}
	var parsed promTargetsResponse
	if err := c.getJSON(ctx, path, &parsed); err != nil {
		return TargetsResponse{}, err
	}
	if parsed.Status != "success" {
		if parsed.Error != "" {
			return TargetsResponse{}, fmt.Errorf("%s: %s", parsed.ErrorType, parsed.Error)
		}
		return TargetsResponse{}, fmt.Errorf("prometheus targets query failed")
	}
	return TargetsResponse{
		ActiveTargets:  mapTargetEntries(parsed.Data.ActiveTargets),
		DroppedTargets: mapTargetEntries(parsed.Data.DroppedTargets),
	}, nil
}

func mapTargetEntries(in []promTargetEntry) []TargetEntry {
	out := make([]TargetEntry, 0, len(in))
	for _, t := range in {
		labels := t.Labels
		if len(labels) == 0 {
			labels = t.DiscoveredLabels
		}
		out = append(out, TargetEntry{
			Labels:             labels,
			ScrapePool:         t.ScrapePool,
			ScrapeURL:          t.ScrapeURL,
			Health:             t.Health,
			LastError:          t.LastError,
			LastScrape:         t.LastScrape,
			LastScrapeDuration: t.LastScrapeDuration,
		})
	}
	return out
}

func vectorToPoints(entries []promVectorEntry) []SamplePoint {
	out := make([]SamplePoint, 0, len(entries))
	for _, entry := range entries {
		if len(entry.Value) < 2 {
			continue
		}
		ts, ok := parseFloatAny(entry.Value[0])
		if !ok {
			continue
		}
		val, ok := parseFloatAny(entry.Value[1])
		if !ok {
			continue
		}
		labels := entry.Metric
		if labels == nil {
			labels = map[string]string{}
		}
		out = append(out, SamplePoint{
			Labels:    labels,
			Value:     val,
			Timestamp: ts,
		})
	}
	return out
}

func parseFloatAny(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case string:
		f, err := strconv.ParseFloat(t, 64)
		return f, err == nil
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
