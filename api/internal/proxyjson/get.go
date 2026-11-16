// Package proxyjson supplies bounded JSON reads through an existing HTTP proxy.
package proxyjson

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
)

// Get retains the caller's status-error representation and does not own proxy policy.
func Get(ctx context.Context, proxy func(*http.Request, string) (*http.Response, error), path string, dest any, statusError func(string) error) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://platform.local"+path, nil)
	if err != nil {
		return err
	}
	resp, err := proxy(req, path)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return statusError(string(body))
	}
	return json.Unmarshal(body, dest)
}
