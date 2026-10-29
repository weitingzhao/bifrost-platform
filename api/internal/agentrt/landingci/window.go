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

// Keep the complete persisted delivery.windowRecord schema here, in the
// protected reader. Even an expired record must decode all known field types;
// dropping a field would turn its malformed values into ignored unknown keys.
type windowRecord struct {
	Who       string `json:"who"`
	What      string `json:"what"`
	Env       string `json:"env"`
	PID       int    `json:"pid"`
	Host      string `json:"host"`
	StartedAt string `json:"started_at"`
	ExpiresAt string `json:"expires_at,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

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
	var record windowRecord
	if json.Unmarshal([]byte(data[releaseWindowDataKey]), &record) != nil || strings.TrimSpace(record.Who) == "" {
		return true, fmt.Errorf("unreadable release window")
	}
	expires, err := time.Parse(time.RFC3339, record.ExpiresAt)
	if err != nil {
		return true, fmt.Errorf("unreadable release window expiry")
	}
	return now.Before(expires), nil
}
