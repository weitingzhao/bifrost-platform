package notice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

// Delivery results.
const (
	ResultAccepted = "accepted"
	ResultFailed   = "failed"
	ResultSkipped  = "skipped"
)

// Delivery is one notice to one target.
type Delivery struct {
	Kind    string
	Channel string
	Target  string
	At      time.Time
	Result  string
	Error   string
}

// Message is one push through the relay.
type Message struct {
	Title    string
	Message  string
	ClickURL string
	Priority int
}

var httpClient = &http.Client{Timeout: 10 * time.Second}

// Configured is true when the relay URL and token are both set.
func Configured() bool {
	return strings.TrimSpace(os.Getenv("APPROVAL_NOTIFY_URL")) != "" &&
		strings.TrimSpace(os.Getenv("APPROVAL_NOTIFY_TOKEN")) != ""
}

// relayAnswer is the relay's reply to POST /alerts/notify.
type relayAnswer struct {
	Error      string `json:"error"`
	Deliveries []struct {
		Channel string `json:"channel"`
		Target  string `json:"target"`
		Result  string `json:"result"`
		Error   string `json:"error"`
	} `json:"deliveries"`
}

// Send posts msg to the alert relay and returns one Delivery per target the
// relay reports. A relay that was not configured or not reached is one
// delivery too. The log lines still say "approval notify": that is the text
// already shipped.
//
// Send checks the title and body and does not rewrite them. A removed-class
// character (LF allowed in the body only) is a failed delivery, error
// "unclean text", and no HTTP. The caller that forgot PushField fails here.
func Send(ctx context.Context, msg Message) ([]Delivery, error) {
	if pushUnclean(msg.Title, false) || pushUnclean(msg.Message, true) {
		slog.Warn("approval notify failed", "reason", "unclean text")
		return []Delivery{{Channel: "ntfy", Target: "relay", Result: ResultFailed, Error: "unclean text"}}, fmt.Errorf("unclean text")
	}
	endpoint := strings.TrimSpace(os.Getenv("APPROVAL_NOTIFY_URL"))
	token := strings.TrimSpace(os.Getenv("APPROVAL_NOTIFY_TOKEN"))
	if endpoint == "" || token == "" {
		slog.Info("approval notify skipped", "title", msg.Title, "reason", "APPROVAL_NOTIFY_URL or APPROVAL_NOTIFY_TOKEN unset")
		return []Delivery{{Channel: "ntfy", Target: "relay", Result: ResultSkipped,
			Error: "APPROVAL_NOTIFY_URL or APPROVAL_NOTIFY_TOKEN unset"}}, nil
	}
	failed := func(err error) ([]Delivery, error) {
		redacted := Redact(err.Error())
		slog.Warn("approval notify failed", "title", msg.Title, "err", redacted)
		return []Delivery{{Channel: "ntfy", Target: "relay", Result: ResultFailed, Error: clipErr(redacted)}}, err
	}
	raw, err := json.Marshal(map[string]any{
		"title":     msg.Title,
		"message":   msg.Message,
		"click_url": msg.ClickURL,
		"priority":  msg.Priority,
	})
	if err != nil {
		return failed(err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return failed(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return failed(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	var ans relayAnswer
	_ = json.Unmarshal(body, &ans)
	var out []Delivery
	for _, d := range ans.Deliveries {
		out = append(out, Delivery{Channel: d.Channel, Target: d.Target, Result: d.Result, Error: clipErr(d.Error)})
	}
	if resp.StatusCode/100 != 2 {
		err = fmt.Errorf("approval notify: relay status %d", resp.StatusCode)
		if ans.Error != "" {
			err = fmt.Errorf("%w: %s", err, Redact(ans.Error))
		}
		if len(out) == 0 {
			return failed(err)
		}
		slog.Warn("approval notify failed", "title", msg.Title, "err", Redact(err.Error()))
		return out, err
	}
	if len(out) == 0 {
		// A relay older than per-target answers only says it sent.
		out = []Delivery{{Channel: "ntfy", Target: "relay", Result: ResultAccepted}}
	}
	return out, nil
}

func clipErr(s string) string {
	return clip(oneLine(Redact(s)), 300)
}

func oneLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n - 1
	for cut > 0 && (s[cut]&0xC0) == 0x80 {
		cut--
	}
	return s[:cut] + "…"
}
