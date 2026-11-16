package checklist

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

func (p *Prober) originalGetJSON(ctx context.Context, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.Base+path, nil)
	if err != nil {
		return err
	}
	resp, err := p.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func (p *Prober) originalHTTPOK(ctx context.Context, url string) (string, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return SignalUnknown, err.Error()
	}
	resp, err := p.client().Do(req)
	if err != nil {
		return SignalFail, url + ": " + err.Error()
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return SignalFail, fmt.Sprintf("%s: HTTP %d", url, resp.StatusCode)
	}
	return SignalOK, url + ": HTTP 200"
}
