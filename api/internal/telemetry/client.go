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
	defer resp.Body.Close()
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
