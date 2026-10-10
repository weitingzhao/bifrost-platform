// Package approvalnotify pages the Owner's phone about approvals: a new
// request, a run that failed or lost its executor, an approval that was never
// executed, and reminders for requests still waiting. Notify sends other Owner
// notices (release policy reminders) the same way.
//
// The operator-plane alert relay (Mac mini .50) accepts
// POST /api/v1/alerts/notify and publishes to ntfy. This package is the
// platform-side client. Send returns one Delivery per target the relay
// reports (or one failed/skipped entry when the relay was not reached); the
// approvals service stores them on the record (deliveries[]).
//
// What a push carries (execution card 6, v4 §19): #n, tier, action, env, the
// one-line summary, key params, requester and expiry. Never the command text
// of owner_run_command, params beyond the key params, tokens or output.
//
// Environment (either unset → a "skipped" delivery, no HTTP):
//
//	APPROVAL_NOTIFY_URL    full URL, e.g. http://192.168.10.50:8783/api/v1/alerts/notify
//	APPROVAL_NOTIFY_TOKEN  bearer equal to the relay's ALERT_RELAY_TOKEN
//
// click_url is ConsoleClickPrefix + the approval id.
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
	"sort"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
)

// ConsoleClickPrefix is the Console deep link the notification opens.
const ConsoleClickPrefix = "http://ops.bifrost.lan/#approvals?id="

// ConsoleApprovals is the Console approvals page without a selected item.
const ConsoleApprovals = "http://ops.bifrost.lan/#approvals"

var httpClient = &http.Client{Timeout: 10 * time.Second}

// Kinds of approval notices. The kind is stored on each delivery so a
// reminder is sent once per record.
const (
	KindCreated = "created"
	// KindFailed: the run failed (including a late result that failed).
	KindFailed = "failed"
	// KindUnknown: the run ended unknown. The push shows Item.Error, the
	// redacted reason (create outcome unknown, the object already exists, or
	// the lease was lost).
	KindUnknown = "unknown"
	// KindNotExecuted: approved, but not executed before the execution deadline.
	KindNotExecuted = "not_executed"
	// KindWaiting: still pending RemindAfter after it was created.
	KindWaiting = "reminder_waiting"
	// KindExpiring: still pending RemindBeforeExpiry before it expires.
	KindExpiring = "reminder_expiring"
)

// Reminder thresholds (S0-0 plan §2.7). A reminder is retried after a failed
// send or an expired claim, so the same reminder may arrive twice.
const (
	RemindAfter        = 4 * time.Hour
	RemindBeforeExpiry = 2 * time.Hour
)

// Delivery results.
const (
	ResultAccepted = "accepted"
	ResultFailed   = "failed"
	ResultSkipped  = "skipped"
)

// Item is the part of an approval a notice may show. It has no params:
// KeyParams is already the short list the catalog chose to show.
type Item struct {
	ID        string
	Number    int
	Action    string
	Tier      string
	Env       string
	Summary   string
	KeyParams map[string]string
	Requester string
	Thread    string
	WorkID    string
	Runner    string
	CreatedAt time.Time
	ExpiresAt time.Time
	// Error is the one-line, redacted reason for failed / not executed.
	Error string
}

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

// Configured is true when the relay URL and token are both set.
func Configured() bool {
	return strings.TrimSpace(os.Getenv("APPROVAL_NOTIFY_URL")) != "" &&
		strings.TrimSpace(os.Getenv("APPROVAL_NOTIFY_TOKEN")) != ""
}

// Send composes the notice of this kind and posts it. It never returns an
// empty list: a relay that was not configured or not reached is a delivery too.
func Send(ctx context.Context, kind string, it Item, now time.Time) []Delivery {
	ds, _ := send(ctx, Compose(kind, it, now))
	for i := range ds {
		ds[i].Kind = kind
		ds[i].At = now
	}
	return ds
}

// Notify posts msg through the relay Send uses. Missing URL or token is a
// skip, not an error.
func Notify(ctx context.Context, msg Message) error {
	if msg.ClickURL == "" {
		msg.ClickURL = ConsoleApprovals
	}
	if msg.Priority == 0 {
		msg.Priority = 4
	}
	_, err := send(ctx, msg)
	return err
}

// Compose is the push text for one notice. English (UI string rule), one
// phone screen.
func Compose(kind string, it Item, now time.Time) Message {
	it.Env = Redact(it.Env)
	it.Summary = Redact(it.Summary)
	it.Error = Redact(it.Error)
	it.Requester = Redact(it.Requester)
	it.Thread = Redact(it.Thread)
	it.WorkID = Redact(it.WorkID)
	it.KeyParams = redactKeyParams(actions.FilterNotifyParams(it.Action, it.KeyParams))
	ref := "#" + fmt.Sprint(it.Number)
	if it.Number == 0 {
		ref = it.ID
	}
	head := joinNonEmpty(" · ", ref, it.Tier, it.Action, it.Env)
	short := joinNonEmpty(" · ", it.Action, it.Env)
	summary := clip(oneLine(it.Summary), 140)
	params := keyParamsLine(it.KeyParams)
	from := fromLine(it)
	m := Message{ClickURL: ConsoleClickPrefix + url.QueryEscape(it.ID), Priority: 4}
	if it.Tier == "D" {
		m.Priority = 5
	}
	switch kind {
	case KindFailed:
		m.Title = ref + " failed · " + short
		m.Message = lines(summary, "error: "+orDefault(clip(oneLine(it.Error), 160), "unknown"), from)
	case KindUnknown:
		m.Title = ref + " unknown · " + short
		reason := clip(oneLine(it.Error), 160)
		if reason == "" {
			reason = "executor lost: lease lapsed with no result"
		}
		m.Message = lines(summary, reason, "Check before running it again.", from)
		m.Priority = 5
	case KindNotExecuted:
		m.Title = ref + " not executed · " + short
		m.Message = lines(summary, "Approved, but not executed before the deadline.", clip(oneLine(it.Error), 160), from)
	case KindWaiting:
		m.Title = "Waiting " + age(now.Sub(it.CreatedAt)) + " · " + head
		m.Message = lines(summary, params, from+" · "+expiry(it.ExpiresAt, now))
	case KindExpiring:
		m.Title = "Expires in " + age(it.ExpiresAt.Sub(now)) + " · " + head
		m.Message = lines(summary, params, from)
	default:
		m.Title = head
		m.Message = lines(summary, params, from+" · "+expiry(it.ExpiresAt, now))
	}
	return m
}

func fromLine(it Item) string {
	who := strings.TrimSpace(it.Requester)
	if who == "" {
		who = "unknown session"
	}
	origin := joinNonEmpty(" · ", strings.TrimSpace(it.WorkID), clip(oneLine(it.Thread), 60))
	from := "from " + who
	if origin != "" {
		from = "from " + origin + " (" + who + ")"
	}
	return from + " · runs on: " + runnerLabel(it.Runner)
}

// runnerLabel: "system" is the name the Owner uses for the host executor.
func runnerLabel(r string) string {
	switch r {
	case "host":
		return "system"
	case "owner":
		return "owner (by hand)"
	case "":
		return "platform"
	}
	return r
}

func redactKeyParams(kp map[string]string) map[string]string {
	if len(kp) == 0 {
		return nil
	}
	out := make(map[string]string, len(kp))
	for k, v := range kp {
		out[k] = Redact(v)
	}
	return out
}

// keyParamsLine never shows command: owner_run_command's text stays in the app.
func keyParamsLine(kp map[string]string) string {
	keys := make([]string, 0, len(kp))
	for k := range kp {
		if k == "command" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+clip(oneLine(kp[k]), 60))
	}
	return strings.Join(parts, " · ")
}

func expiry(at, now time.Time) string {
	if at.IsZero() {
		return "no expiry"
	}
	if !at.After(now) {
		return "expired"
	}
	return "expires in " + age(at.Sub(now))
}

// age is whole hours from one hour up, whole minutes below.
func age(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d >= time.Hour {
		return fmt.Sprintf("%dh", int(d/time.Hour))
	}
	return fmt.Sprintf("%dm", int(d/time.Minute))
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

func send(ctx context.Context, msg Message) ([]Delivery, error) {
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

func lines(parts ...string) string {
	return joinNonEmpty("\n", parts...)
}

func joinNonEmpty(sep string, parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}

func oneLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}

func clipErr(s string) string {
	return clip(oneLine(Redact(s)), 300)
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

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
