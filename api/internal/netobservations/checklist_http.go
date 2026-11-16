package netobservations

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// GetJSON retains the unbounded streaming JSON read used by observation callers.
// It does not normalize status, follow a new policy, retry, or read credentials.
func GetJSON(ctx context.Context, client *http.Client, url string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// DoAndClose observes response status without draining its body. The caller
// retains request construction, status classification and retry policy.
func DoAndClose(client *http.Client, req *http.Request) (*http.Response, error) {
	resp, err := client.Do(req)
	if err != nil {
		return resp, err
	}
	resp.Body.Close()
	return resp, nil
}
