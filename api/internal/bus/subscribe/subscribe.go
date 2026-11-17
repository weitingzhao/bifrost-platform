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
	if row.Change != "" && ev.Change != row.Change {
		return false
	}
	return row.Match == "" || strings.Contains(ev.Subject, row.Match)
}

// CommitMessage reads a commit's message (delivery.PolicyFacts.CommitMessage).
type CommitMessage func(ctx context.Context, repo, sha string) (string, bool, error)

// Sender posts a message to an inbox once per key (store.PostConclusion).
type Sender func(ctx context.Context, key, to, body string) error

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
		u, err := url.Parse(ev.Ref)
		if m == nil || err != nil || u.Query().Get("repo") == "" {
			return "", fmt.Errorf("%s: %s names no commit and repo (subject %q, ref %q)", row.ID, ev.ID, ev.Subject, ev.Ref)
		}
		if read == nil {
			return "", fmt.Errorf("%s: no commit reader", row.ID)
		}
		msg, ok, err := read(ctx, u.Query().Get("repo"), m[1])
		if err != nil {
			return "", err
		}
		s := sessionRE.FindStringSubmatch(msg)
		if !ok || s == nil {
			return "", nil
		}
		return "thread:" + s[1], nil
	}
	if addrRE.MatchString(row.To) {
		return row.To, nil
	}
	return "", fmt.Errorf("%s: wake to %q is not resolved by the dispatcher", row.ID, row.To)
}

// Body is the wake message: what happened and where to read it.
func Body(row registry.Subscription, ev Event) string {
	return fmt.Sprintf("bus: %s %s (change %s). Read %s. [subscription %s, event %s]", ev.Type, ev.Subject, ev.Change, ev.Ref, row.ID, ev.ID)
}

// Key makes one message per row and event, however often it is delivered.
func Key(row registry.Subscription, ev Event) string { return "sub:" + row.ID + ":" + ev.ID }

// Deliver is one row's handling of one event: nil acknowledges it (also when
// it does not match or has nobody to wake); an error asks for it again.
func Deliver(ctx context.Context, row registry.Subscription, ev Event, read CommitMessage, send Sender) error {
	if row.Deliver != registry.DeliverWake || !Matches(row, ev) {
		return nil
	}
	to, err := Recipient(ctx, row, ev, read)
	if err != nil || to == "" {
		return err
	}
	return send(ctx, Key(row, ev), to, Body(row, ev))
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
