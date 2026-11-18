// Package subscribe is the bus's one dispatcher: it reads the tenants'
// subscription tables (bus/registry/subscriptions) and, for each row the
// dispatcher delivers, runs a durable consumer and hands each matching event
// to its delivery. New reactions are new rows, not new code (2026-11-16, the
// coordinator for the Owner). A row decides who is told, never what may be
// done.
//
// This version delivers wake only: a message to the seat's inbox, which
// announces seat.mail.arrived; the seat's doorbell waits on that.
package subscribe

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/relay"
)

// Event is what a row sees of an envelope.
type Event struct {
	ID, Type, Subject, Ref, Change string
}

// FromEnvelope keeps the fields a row reads.
func FromEnvelope(e relay.Envelope) Event {
	return Event{ID: e.ID, Type: e.Type, Subject: e.Subject, Ref: e.Ref, Change: e.Change}
}

// Matches says whether ev is one of row's.
func Matches(row registry.Subscription, ev Event) bool {
	on := false
	for _, t := range row.On {
		on = on || t == ev.Type
	}
	if !on {
		return false
	}
	if row.Change != "" && !changeMatches(row.Change, ev.Change) {
		return false
	}
	return row.Match == "" || strings.Contains(ev.Subject, row.Match)
}

// changeMatches reads a row's change: one value, or "!value" for any other.
func changeMatches(want, got string) bool {
	if strings.HasPrefix(want, "!") {
		return got != "" && got != want[1:]
	}
	return got == want
}

// CommitMessage reads a commit's message (delivery.PolicyFacts.CommitMessage).
type CommitMessage func(ctx context.Context, repo, sha string) (string, bool, error)

// Sender posts a message to an inbox once per key (coord.PostConclusion).
// cause is the ledger number of the event the wake answers (its ev-<n>), so
// the letter names what woke it (2026-11-17: the bus's loops).
type Sender func(ctx context.Context, key, to, body string, cause int64) error

var (
	sessionRE = regexp.MustCompile(`(?m)^Claude-Session:\s*(local_[A-Za-z0-9-]{1,100})\s*$`)
	shaRE     = regexp.MustCompile(`@([0-9a-f]{40})`)
	addrRE    = regexp.MustCompile(`^(seat|thread|agent):[A-Za-z0-9._:-]{1,128}$`)
)

// Recipient resolves a wake row's to for ev: a literal address, the event's
// subject, or commit.session (the Claude-Session trailer of the commit named
// in the subject, in the repo named in the ref). "" with a nil error: nobody
// to wake (a commit with no session trailer), which is not a failure.
func Recipient(ctx context.Context, row registry.Subscription, ev Event, read CommitMessage) (string, error) {
	switch row.To {
	case "subject":
		if addrRE.MatchString(ev.Subject) {
			return ev.Subject, nil
		}
		return "", nil
	case "commit.session":
		m := shaRE.FindStringSubmatch(ev.Subject)
		repos := row.Repos
		if u, err := url.Parse(ev.Ref); err == nil && u.Query().Get("repo") != "" {
			repos = []string{u.Query().Get("repo")}
		}
		if m == nil || len(repos) == 0 {
			return "", fmt.Errorf("%s: %s names no commit and repo (subject %q, ref %q)", row.ID, ev.ID, ev.Subject, ev.Ref)
		}
		if read == nil {
			return "", fmt.Errorf("%s: no commit reader", row.ID)
		}
		// The commit is looked for in each candidate repository; a commit id
		// is in one of them.
		for _, repo := range repos {
			msg, ok, err := read(ctx, repo, m[1])
			if err != nil {
				return "", err
			}
			if !ok {
				continue
			}
			if s := sessionRE.FindStringSubmatch(msg); s != nil {
				return "thread:" + s[1], nil
			}
			return "", nil
		}
		return "", nil
	}
	if addrRE.MatchString(row.To) {
		return row.To, nil
	}
	return "", fmt.Errorf("%s: wake to %q is not resolved by the dispatcher", row.ID, row.To)
}

// Detail answers a row's params.detail for one event: what the event alone
// does not say, read where it is kept (a run's task results and params),
// by name; a name with nothing is left out or "".
type Detail func(ctx context.Context, row registry.Subscription, ev Event) (map[string]string, error)

// RunOf is the run an event of ci.run.finished names ("<run>@<commit>").
func RunOf(ev Event) string {
	run, _, _ := strings.Cut(ev.Subject, "@")
	return run
}

// readDetail reads the row's detail: values by name, or why it could not.
func readDetail(ctx context.Context, row registry.Subscription, ev Event, detail Detail) (map[string]string, error) {
	if detail == nil || row.Params[registry.ParamDetail] == "" {
		return nil, nil
	}
	return detail(ctx, row, ev)
}

// withDetail is body with the row's detail added: one name as "Why: <it>",
// several as "name=value; …" in the detail's order. A detail that cannot be
// read says so and the wake still goes (it is a hint, not the message).
func withDetail(row registry.Subscription, values map[string]string, err error, body string) string {
	spec := row.Params[registry.ParamDetail]
	if spec == "" {
		return body
	}
	if err != nil {
		return body + " (" + spec + " could not be read: " + err.Error() + ")"
	}
	_, names, _ := registry.TaskResults(spec)
	if len(names) == 1 {
		if v := values[names[0]]; v != "" {
			return body + " Why: " + v
		}
		return body
	}
	var parts []string
	for _, n := range names {
		if v := values[n]; v != "" {
			parts = append(parts, n+"="+v)
		}
	}
	if len(parts) == 0 {
		return body
	}
	return body + " Detail: " + strings.Join(parts, "; ")
}

// Body is the wake message: what happened and where to read it.
func Body(row registry.Subscription, ev Event) string {
	return fmt.Sprintf("bus: %s %s (change %s). Read %s. [subscription %s, event %s]", ev.Type, ev.Subject, ev.Change, ev.Ref, row.ID, ev.ID)
}

// Key makes one message per row and event, however often it is delivered.
func Key(row registry.Subscription, ev Event) string { return "sub:" + row.ID + ":" + ev.ID }

// Known says whether an address can receive a wake here (a registered
// thread in this environment's ledger).
type Known func(ctx context.Context, address string) (bool, error)

// Outcome says what Deliver did, for the log and the receipts.
type Outcome string

const (
	Skipped       Outcome = "skipped"       // not this row's event, or nobody to wake
	Delivered     Outcome = "delivered"     // the recipient got its wake
	Rerouted      Outcome = "rerouted"      // the recipient is unknown here; undeliverable_to was told
	Undeliverable Outcome = "undeliverable" // unknown recipient and nowhere to tell
)

// Deliver is one row's handling of one event: a nil error acknowledges it;
// an error asks for it again. A wake whose recipient is not known here is
// not dropped silently: it goes to the tenant's undeliverable_to, naming the
// row, the event and the address (2026-11-16, the tenant drill).
func Deliver(ctx context.Context, row registry.Subscription, ev Event, read CommitMessage, detail Detail, send Sender, known Known, undeliverableTo string) (Outcome, error) {
	if row.Deliver != registry.DeliverWake || !Matches(row, ev) {
		return Skipped, nil
	}
	values, derr := readDetail(ctx, row, ev, detail)
	// params.require: only when the named value is there; a detail that
	// could not be read wakes anyway (it may be the one that matters).
	if req := row.Params[registry.ParamRequire]; req != "" && derr == nil && values[req] == "" {
		return Skipped, nil
	}
	to, err := Recipient(ctx, row, ev, read)
	if err != nil || to == "" {
		return Skipped, err
	}
	if known != nil {
		ok, err := known(ctx, to)
		if err != nil {
			return Skipped, err
		}
		if !ok {
			if undeliverableTo == "" {
				return Undeliverable, nil
			}
			body := fmt.Sprintf("bus: a wake could not be delivered: %s is not a registered thread here. %s", to, Body(row, ev))
			return Rerouted, send(ctx, Key(row, ev)+":undeliverable", undeliverableTo, body, causeOf(ev))
		}
	}
	return Delivered, send(ctx, Key(row, ev), to, withDetail(row, values, derr, Body(row, ev)), causeOf(ev))
}

// Notifier pushes one notice to the Owner's phone (the platform's notice
// relay); an error asks for the event again.
type Notifier func(ctx context.Context, key, title, body, click string) error

// Notify is a notify row's handling of one event: one push, its title and
// link from the row's params, its body the event's line. A nil error
// acknowledges the event.
func Notify(ctx context.Context, row registry.Subscription, ev Event, notify Notifier) (Outcome, error) {
	if row.Deliver != registry.DeliverNotify || row.To != registry.NotifyTo || !Matches(row, ev) || notify == nil {
		return Skipped, nil
	}
	fill := func(t string) string {
		return strings.NewReplacer("{type}", ev.Type, "{subject}", ev.Subject, "{change}", ev.Change, "{ref_id}", refID(ev.Ref)).Replace(t)
	}
	return Delivered, notify(ctx, Key(row, ev), fill(row.Params[registry.ParamTitle]), Body(row, ev), fill(row.Params[registry.ParamClick]))
}

// refID is the last segment of a ref ("/api/v1/approvals/appr_1" → "appr_1"), path-escaped.
func refID(ref string) string {
	ref, _, _ = strings.Cut(ref, "?")
	ref = strings.TrimRight(ref, "/")
	return url.PathEscape(ref[strings.LastIndex(ref, "/")+1:])
}

// causeOf is the ledger number of the event a wake answers (0 when its id is not ev-<n>).
func causeOf(ev Event) int64 {
	n, _ := relay.ParseEventID(ev.ID)
	return n
}

// UndeliverableTo is the tenant's undeliverable_to.
func UndeliverableTo(subs map[string]registry.Subscriptions, tenant string) string {
	return subs[tenant].UndeliverableTo
}

// Rows is the rows of tenant the dispatcher delivers.
func Rows(subs map[string]registry.Subscriptions, tenant string) []registry.Subscription {
	var out []registry.Subscription
	for _, r := range subs[tenant].Rows {
		if r.By == registry.ByDispatcher {
			out = append(out, r)
		}
	}
	return out
}

// Consumer is one durable consumer the dispatcher runs: a row of a tenant's
// table, in this environment.
type Consumer struct {
	Tenant string
	Row    registry.Subscription
}

// ConsumerPrefix starts every durable consumer the dispatcher owns.
const ConsumerPrefix = "sub-"

// Name is the consumer's durable name: sub-<row> for the stream's own
// tenant (the names already in use), sub-<tenant>-<row> for another.
func (c Consumer) Name(streamTenant string) string {
	if c.Tenant == streamTenant {
		return ConsumerPrefix + c.Row.ID
	}
	return ConsumerPrefix + c.Tenant + "-" + c.Row.ID
}

// Consumers is every row of every tenant the dispatcher delivers in env, in
// tenant then table order. A tenant comes and goes with its files: nothing
// here names one (2026-11-16, the Owner: users come and go by configuration).
func Consumers(subs map[string]registry.Subscriptions, env string) []Consumer {
	names := make([]string, 0, len(subs))
	for n := range subs {
		names = append(names, n)
	}
	sort.Strings(names)
	var out []Consumer
	for _, n := range names {
		for _, r := range Rows(subs, n) {
			if r.InEnv(env) {
				out = append(out, Consumer{Tenant: n, Row: r})
			}
		}
	}
	return out
}

// Stale is the dispatcher's durable consumers (names with ConsumerPrefix)
// that no row asks for any more: a row or a tenant was removed, so its
// consumer goes too and nothing keeps its messages for it.
func Stale(existing []string, want []Consumer, streamTenant string) []string {
	keep := map[string]bool{}
	for _, c := range want {
		keep[c.Name(streamTenant)] = true
	}
	var out []string
	for _, n := range existing {
		if strings.HasPrefix(n, ConsumerPrefix) && !keep[n] {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}
