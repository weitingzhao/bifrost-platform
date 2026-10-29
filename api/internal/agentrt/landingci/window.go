package landingci

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const releaseWindowConfigMap = "bifrost-release-window"
const releaseWindowDataKey = "window.json"

// LandingWindowBlocked only reads; even expired windows are never deleted.
func (c *Client) LandingWindowBlocked(ctx context.Context) (bool, error) {
	return c.windowBlocked(ctx, time.Now())
}
func (c *Client) windowBlocked(ctx context.Context, now time.Time) (bool, error) {
	if c.ConfigMap == nil {
		return true, fmt.Errorf("release window transport unavailable")
	}
	data, found, err := c.ConfigMap(ctx, releaseWindowConfigMap)
	if err != nil {
		return true, err
	}
	if !found {
		return false, nil
	}
	var record struct {
		Who       string `json:"who"`
		ExpiresAt string `json:"expires_at"`
	}
	if json.Unmarshal([]byte(data[releaseWindowDataKey]), &record) != nil || strings.TrimSpace(record.Who) == "" {
		return true, fmt.Errorf("unreadable release window")
	}
	expires, err := time.Parse(time.RFC3339, record.ExpiresAt)
	if err != nil {
		return true, fmt.Errorf("unreadable release window expiry")
	}
	return now.Before(expires), nil
}
