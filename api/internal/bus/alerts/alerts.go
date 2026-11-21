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

	actors "github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"

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
	// hostRE is a host name or an IPv4 address.
	hostRE = regexp.MustCompile(`^[A-Za-z0-9]([-A-Za-z0-9.]{0,251}[A-Za-z0-9])?$`)
)

// Alert is what the bus keeps of one alert: no annotations, no values. Host
// is the machine the alert is about, "" for one about no machine.
//
// 2026-11-20 (comm line): a hardware alert has no namespace, so it was written
// as <alertname>@cluster and its machine was lost. In the week to 11-21 00:18Z
// firing alerts named one (BifrostClusterNodeDown node=ubt-k3s-05,
// BifrostElasticStandbyMarker node=gpu-server; Prometheus ALERTS, ad-hoc
// reading). The duty
// seat's letter said only "@cluster", and the same alert on a second machine
// within 6 hours shared the first one's letter and was not told.
type Alert struct {
	Name, Namespace, Severity, Status, Host string
}

// HostOf is the machine an alert's labels name: host (the external hosts'
// scrape label), else node (Kubernetes rules). "" when neither is a host name
// or address.
//
// The instance label is not read. 2026-11-21 (comm line, the first PROD
// events with a host): KubeJobFailed@cicd/10.42.6.28 named the
// kube-state-metrics pod that exports the series, not a machine the alert
// is about. instance is the scrape target, which is a machine only for
// node-level exporters.
func HostOf(host, node string) string {
	for _, h := range []string{host, node} {
		h = strings.TrimSpace(h)
		if h != "" && hostRE.MatchString(h) {
			return h
		}
	}
	return ""
}

// Stream is the ledger stream of one alert rule.
func Stream(name string) string { return "alert:" + name }

// Subject is "<alertname>@<namespace>", and "/<host>" after it for an alert
// about one machine: a seat follows its namespaces with match=@<namespace>
// and a machine with match=/<host>.
func Subject(name, namespace string) string { return name + "@" + namespace }

// SubjectOf is the alert's subject, its host included.
func SubjectOf(a Alert) string {
	if a.Host == "" {
		return Subject(a.Name, a.Namespace)
	}
	return Subject(a.Name, a.Namespace) + "/" + a.Host
}

// Body is the event's body: the bus's fixed fields and their facts. key is
// the namespace, change is firing or resolved.
func Body(a Alert) map[string]any {
	b := map[string]any{
		"alertname": a.Name,
		"namespace": a.Namespace,
		"severity":  a.Severity,
		"status":    a.Status,
		"subject":   SubjectOf(a),
		"ref":       Ref,
		"key":       a.Namespace,
		"change":    a.Status,
	}
	if a.Host != "" {
		b["host"] = a.Host
	}
	return b
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

// Fired is one alert the ledger now holds, with its event id.
type Fired struct {
	Alert
	EventID int64
}

// Announce appends one event per alert in one transaction and says each
// event's id (the webhook's own deliveries name the event they are for). A
// ledger without the controller (a database the platform has not seeded)
// announces nothing.
func Announce(ctx context.Context, db eventlog.DB, list []Alert) ([]Fired, error) {
	if db == nil || len(list) == 0 {
		return nil, nil
	}
	var out []Fired
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		out = out[:0]
		if seeded, err := actors.Exists(ctx, tx, actor); err != nil || !seeded {
			return err
		}
		for _, a := range list {
			ap, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: Stream(a.Name), Type: Type, Actor: actor, Body: Body(a)})
			if err != nil {
				return err
			}
			out = append(out, Fired{Alert: a, EventID: ap.EventID})
		}
		return nil
	})
	return out, err
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
				{Name: "host", Kind: eventlog.Str, Optional: true},
			},
		},
		Values: map[string]map[string]eventlog.Value{
			Type: {
				"alertname": eventlog.Shaped("an alert name", nameRE),
				"namespace": eventlog.Shaped("a namespace name or cluster", nsRE),
				"status":    eventlog.OneOf("firing", "resolved"),
				"change":    eventlog.OneOf("firing", "resolved"),
				"host":      eventlog.Shaped("a host name or address", hostRE),
			},
		},
		After: map[string]func(body map[string]any) error{
			Type: func(body map[string]any) error {
				s := func(k string) string { v, _ := body[k].(string); return v }
				a := Alert{Name: s("alertname"), Namespace: s("namespace"), Host: s("host")}
				if body["subject"] != SubjectOf(a) || body["ref"] != Ref || body["key"] != s("namespace") || body["change"] != s("status") {
					return eventlog.Invalid("%s: subject, ref, key and change are made of alertname, namespace, host and status", Type)
				}
				return nil
			},
		},
	})
}
