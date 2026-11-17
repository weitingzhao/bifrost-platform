// Package alerts is the bus's entry for Alertmanager: every alert the
// platform's webhook receives is written to the ledger as alert.rule.fired,
// so the seat that owns its namespace can subscribe to it.
//
// 2026-11-16: the TTL cleanup job's two warnings fired for 43 hours and
// reached nobody. Only critical alerts and a few backup names go to the
// Owner's phone; every other alert ends at this webhook, which only wrote
// the audit log. The CI line found it and asked for a topic it can follow.
package alerts

import (
	"context"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// Type is the event: an alert started or stopped firing.
const Type = "alert.rule.fired"

// Ref is where a reader reads the alerts now firing.
const Ref = "/api/v1/telemetry/alerts"

// NoNamespace stands for an alert without a namespace label (a node, the
// cluster itself).
const NoNamespace = "cluster"

// actor writes the events; the platform seeds it (the evidence ledger's
// verdicts are written by it too).
const actor = "controller"

var (
	nameRE = regexp.MustCompile(`^[A-Za-z0-9_:.-]{1,200}$`)
	nsRE   = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)
)

// Alert is what the bus keeps of one alert: no annotations, no values.
type Alert struct {
	Name, Namespace, Severity, Status string
}

// Stream is the ledger stream of one alert rule.
func Stream(name string) string { return "alert:" + name }

// Subject is "<alertname>@<namespace>": a seat follows its namespaces with
// match=@<namespace>.
func Subject(name, namespace string) string { return name + "@" + namespace }

// Body is the event's body: the bus's fixed fields and their facts. key is
// the namespace, change is firing or resolved.
func Body(a Alert) map[string]any {
	return map[string]any{
		"alertname": a.Name,
		"namespace": a.Namespace,
		"severity":  a.Severity,
		"status":    a.Status,
		"subject":   Subject(a.Name, a.Namespace),
		"ref":       Ref,
		"key":       a.Namespace,
		"change":    a.Status,
	}
}

// Normalize fills the namespace and lowercases the status and severity; ok
// is false for an alert the bus cannot name.
func Normalize(name, namespace, severity, status string) (Alert, bool) {
	a := Alert{Name: strings.TrimSpace(name), Namespace: strings.TrimSpace(namespace),
		Severity: strings.ToLower(strings.TrimSpace(severity)), Status: strings.ToLower(strings.TrimSpace(status))}
	if a.Namespace == "" {
		a.Namespace = NoNamespace
	}
	if a.Severity == "" {
		a.Severity = "none"
	}
	if !nameRE.MatchString(a.Name) || !nsRE.MatchString(a.Namespace) || (a.Status != "firing" && a.Status != "resolved") || !nameRE.MatchString(a.Severity) {
		return Alert{}, false
	}
	return a, true
}

// Announce appends one event per alert in one transaction. A ledger without
// the controller (a database the platform has not seeded) announces nothing.
func Announce(ctx context.Context, db eventlog.DB, list []Alert) error {
	if db == nil || len(list) == 0 {
		return nil
	}
	return eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		var seeded bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.actor WHERE actor_id = $1)`, actor).Scan(&seeded); err != nil || !seeded {
			return err
		}
		for _, a := range list {
			if _, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: Stream(a.Name), Type: Type, Actor: actor, Body: Body(a)}); err != nil {
				return err
			}
		}
		return nil
	})
}

func init() {
	eventlog.Declare(eventlog.Vocabulary{
		Bodies: map[string][]eventlog.Field{
			Type: {
				{Name: "alertname", Kind: eventlog.Str},
				{Name: "namespace", Kind: eventlog.Str},
				{Name: "severity", Kind: eventlog.Str},
				{Name: "status", Kind: eventlog.Str},
				{Name: "subject", Kind: eventlog.Str},
				{Name: "ref", Kind: eventlog.Str},
				{Name: "key", Kind: eventlog.Str},
				{Name: "change", Kind: eventlog.Str},
			},
		},
		Values: map[string]map[string]eventlog.Value{
			Type: {
				"alertname": eventlog.Shaped("an alert name", nameRE),
				"namespace": eventlog.Shaped("a namespace name or cluster", nsRE),
				"status":    eventlog.OneOf("firing", "resolved"),
				"change":    eventlog.OneOf("firing", "resolved"),
			},
		},
		After: map[string]func(body map[string]any) error{
			Type: func(body map[string]any) error {
				s := func(k string) string { v, _ := body[k].(string); return v }
				if body["subject"] != Subject(s("alertname"), s("namespace")) || body["ref"] != Ref || body["key"] != s("namespace") || body["change"] != s("status") {
					return eventlog.Invalid("%s: subject, ref, key and change are made of alertname, namespace and status", Type)
				}
				return nil
			},
		},
	})
}
