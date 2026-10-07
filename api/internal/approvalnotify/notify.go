// Package approvalnotify pages the Owner's phone when an approval is created.
//
// The operator-plane alert relay (Mac mini .50) accepts
// POST /api/v1/alerts/notify. This package is the platform-side client.
// POST /api/v1/approvals calls NotifyCreated after the pending record is
// stored and before it writes 201. Approve, reject, and expiry do not.
//
// # B1 wiring (one line)
//
// In the POST /api/v1/approvals handler, after the pending record is stored
// and its id is known, before the 201 response is written:
//
//	_ = approvalnotify.NotifyCreated(r.Context(), approvalnotify.Created{
//		ID:        id,
//		Action:    body.Action,
//		Tier:      tier,
//		Requester: r.Header.Get("X-Bifrost-Session"),
//	})
//
// Import github.com/weitingzhao/bifrost-platform/api/internal/approvalnotify.
// Ignore the error: NotifyCreated logs it. A down relay must not fail create.
// Do not call it from approve, reject, or expiry.
//
// Environment (both unset → log and return nil, no HTTP):
//
//	APPROVAL_NOTIFY_URL    full URL, e.g. http://192.168.10.50:8783/api/v1/alerts/notify
//	APPROVAL_NOTIFY_TOKEN  bearer equal to the relay's ALERT_RELAY_TOKEN
//
// click_url is always http://ops.bifrost.lan/#approvals?id=<id>.
package approvalnotify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Created is the slice of a new pending approval the phone notification needs.
type Created struct {
	ID        string
	Action    string
	Tier      string
	Requester string
}

// ConsoleClickPrefix is the Console deep link the notification opens.
const ConsoleClickPrefix = "http://ops.bifrost.lan/#approvals?id="

var httpClient = &http.Client{Timeout: 10 * time.Second}

// NotifyCreated posts one ntfy notification via the alert relay.
// Missing URL or token is a skip, not an error.
func NotifyCreated(ctx context.Context, item Created) error {
	endpoint := strings.TrimSpace(os.Getenv("APPROVAL_NOTIFY_URL"))
	token := strings.TrimSpace(os.Getenv("APPROVAL_NOTIFY_TOKEN"))
	if endpoint == "" || token == "" {
		slog.Info("approval notify skipped", "id", item.ID, "reason", "APPROVAL_NOTIFY_URL or APPROVAL_NOTIFY_TOKEN unset")
		return nil
	}
	id := strings.TrimSpace(item.ID)
	if id == "" {
		return fmt.Errorf("approval notify: id is empty")
	}
	click := ConsoleClickPrefix + url.QueryEscape(id)
	raw, err := json.Marshal(map[string]any{
		"title":     "Approval needed",
		"message":   messageFor(item),
		"click_url": click,
		"priority":  4,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := httpClient.Do(req)
	if err != nil {
		slog.Warn("approval notify failed", "id", id, "err", err)
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode/100 != 2 {
		err = fmt.Errorf("approval notify: status %d", resp.StatusCode)
		slog.Warn("approval notify failed", "id", id, "err", err)
		return err
	}
	return nil
}

func messageFor(item Created) string {
	who := strings.TrimSpace(item.Requester)
	if who == "" {
		who = "unknown session"
	}
	action := strings.TrimSpace(item.Action)
	if action == "" {
		action = "an action"
	}
	tier := strings.TrimSpace(item.Tier)
	if tier == "" {
		tier = "?"
	}
	return fmt.Sprintf("%s requested %s (tier %s). Open to approve or reject.", who, action, tier)
}
