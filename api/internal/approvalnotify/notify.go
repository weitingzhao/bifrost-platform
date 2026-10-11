// Package approvalnotify pages the Owner's phone about approvals: a new
// request, a run that failed or lost its executor, an approval that was never
// executed, and reminders for requests still waiting. Notify sends other Owner
// notices (release policy reminders) the same way.
//
// The operator-plane alert relay (Mac mini .50) accepts
// POST /api/v1/alerts/notify and publishes to ntfy. The post itself lives in
// package notice. This package composes the approval text and calls it.
// Send returns one Delivery per target the relay reports (or one
// failed/skipped entry when the relay was not reached); the approvals
// service stores them on the record (deliveries[]).
//
// What a push carries (execution card 6, v4 §19): a verb that says what the
// Owner has to do, then the reference, then a short lead, plus the one-line
// summary, key params, requester and expiry. Never the command text of
// owner_run_command, params beyond the key params, tokens or output.
// A kind with no verb in kindVerb is not a message and is not sent.
//
// Environment (either unset → a "skipped" delivery, no HTTP):
//
//	APPROVAL_NOTIFY_URL    full URL, e.g. http://192.168.10.50:8783/api/v1/alerts/notify
//	APPROVAL_NOTIFY_TOKEN  bearer equal to the relay's ALERT_RELAY_TOKEN
//
// click_url is ConsoleClickPrefix + the approval id.
package approvalnotify

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/notice"
)

// ConsoleClickPrefix is the Console deep link the notification opens.
const ConsoleClickPrefix = "http://ops.bifrost.lan/#approvals?id="

// ConsoleApprovals is the Console approvals page without a selected item.
const ConsoleApprovals = "http://ops.bifrost.lan/#approvals"

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

// Delivery results. The values are the relay's.
const (
	ResultAccepted = notice.ResultAccepted
	ResultFailed   = notice.ResultFailed
	ResultSkipped  = notice.ResultSkipped
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
	// Reason is the approval reason. The lead is its first paragraph.
	Reason string
	// Error is the one-line, redacted reason for failed / not executed.
	Error string
}

// Delivery is one notice to one target. The relay record lives in notice.
type Delivery = notice.Delivery

// Message is one push through the relay. The relay record lives in notice.
type Message = notice.Message

// Configured is true when the relay URL and token are both set.
func Configured() bool { return notice.Configured() }

// Send composes the notice of this kind and posts it. A kind with no verb is
// not a message: Send returns nothing and does not call the relay, and the
// caller skips it. Otherwise it never returns an empty list: a relay that was
// not configured or not reached is a delivery too.
func Send(ctx context.Context, kind string, it Item, now time.Time) []Delivery {
	msg := Compose(kind, it, now)
	if msg.Title == "" {
		return nil
	}
	ds, _ := notice.Send(ctx, msg)
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
	_, err := notice.Send(ctx, msg)
	return err
}

// kindVerb is what the push asks of the Owner. A kind that is not in this
// table asks nothing, Compose returns no message, and Send does not post it.
var kindVerb = map[string]string{
	KindCreated:     "Decide",
	KindWaiting:     "Decide",
	KindExpiring:    "Decide",
	KindUnknown:     "Check",
	KindFailed:      "Check",
	KindNotExecuted: "Check",
}

// Compose is the push text for one notice. English (UI string rule), one
// phone screen. The title starts with the verb, then the reference, then
// plain words. A kind with no verb returns a zero Message.
func Compose(kind string, it Item, now time.Time) Message {
	verb, ok := kindVerb[kind]
	if !ok {
		return Message{}
	}
	it.Env = Redact(it.Env)
	it.Summary = Redact(it.Summary)
	it.Reason = Redact(it.Reason)
	it.Error = Redact(it.Error)
	it.Requester = Redact(it.Requester)
	it.Thread = Redact(it.Thread)
	it.WorkID = Redact(it.WorkID)
	it.KeyParams = redactKeyParams(actions.FilterNotifyParams(it.Action, it.KeyParams))
	ref := "#" + fmt.Sprint(it.Number)
	if it.Number == 0 {
		ref = it.ID
	}
	lead := noticeLead(it)
	summary := clip(oneLine(it.Summary), 140)
	params := keyParamsLine(it.KeyParams)
	from := fromLine(it)
	ask := tierAsk(it.Tier)
	m := Message{ClickURL: ConsoleClickPrefix + url.QueryEscape(it.ID), Priority: 4}
	if it.Tier == "D" {
		m.Priority = 5
	}
	switch kind {
	case KindFailed:
		m.Title = joinNonEmpty(" · ", verb, ref, "failed", lead)
		m.Message = lines("error: "+orDefault(clip(oneLine(it.Error), 160), "unknown"), "Nothing else was run.", summary, from)
	case KindUnknown:
		m.Title = joinNonEmpty(" · ", verb, ref, "result unknown", lead)
		reason := clip(oneLine(it.Error), 160)
		if reason == "" {
			reason = "executor lost: lease lapsed with no result"
		}
		m.Message = lines("The platform lost track of it: "+reason+".", "Open it and say what happened.", from)
		m.Priority = 5
	case KindNotExecuted:
		m.Title = joinNonEmpty(" · ", verb, ref, "approved but never ran", lead)
		m.Message = lines("It was approved and not run before its deadline.", clip(oneLine(it.Error), 160), from)
	case KindWaiting:
		m.Title = joinNonEmpty(" · ", verb, ref, "waiting "+age(now.Sub(it.CreatedAt)), lead)
		m.Message = lines(ask, summary, params, from+" · "+expiry(it.ExpiresAt, now))
	case KindExpiring:
		m.Title = joinNonEmpty(" · ", verb, ref, "expires in "+age(it.ExpiresAt.Sub(now)), lead)
		m.Message = lines(ask, summary, params, from)
	default:
		m.Title = joinNonEmpty(" · ", verb, ref, lead)
		m.Message = lines(ask, summary, params, from+" · "+expiry(it.ExpiresAt, now))
	}
	m.Title = notice.Redact(notice.StripControls(m.Title, false))
	m.Message = notice.Redact(notice.StripControls(m.Message, true))
	return m
}

// noticeLead is the first paragraph of the reason, else the summary, else
// action · env. It is already redacted. Clipped to 100 characters.
func noticeLead(it Item) string {
	lead := clipChars(oneLine(it.Reason), 100)
	if lead == "" {
		lead = clipChars(oneLine(it.Summary), 100)
	}
	if lead == "" {
		lead = joinNonEmpty(" · ", it.Action, it.Env)
	}
	return lead
}

// tierAsk is the first body line of a decision the Owner has not made yet.
func tierAsk(tier string) string {
	switch tier {
	case "C":
		return "Runs as soon as you approve."
	case "D":
		return "After approving, run one command in a terminal."
	default:
		return ""
	}
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

// clipChars keeps at most n characters (runes), with an ellipsis when it cuts.
func clipChars(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n < 1 {
		return ""
	}
	return string(r[:n-1]) + "…"
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
