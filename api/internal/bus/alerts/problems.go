package alerts

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// Problems the platform records about itself, said as alerts (the Owner,
// 2026-11-18: first make sure problems can be seen). Until then they stayed in
// the ledger under types the bus does not carry: 1,125 policy_alert records that
// nobody was told of. As alerts they reach the duty seat by the row every alert
// uses, and repeats share a letter.
const (
	NameApprovalClaim    = "ApprovalClaimInText"
	NameLoopFailing      = "ControllerLoopFailing"
	NameArtifactRefused  = "ArtifactRefused"
	NameHeartbeatMissing = "AlertmanagerHeartbeatMissing"
	NameRelayUnreachable = "AlertRelayUnreachable"

	// platformNamespace is the "namespace" of what the platform says of its
	// own runtime; monitoringNamespace of the alert path itself.
	platformNamespace   = "agentrt"
	monitoringNamespace = "monitoring"
)

func statusOf(firing bool) string {
	if firing {
		return "firing"
	}
	return "resolved"
}

// ApprovalClaim is a text that claims the Owner's approval.
func ApprovalClaim() Alert {
	return Alert{NameApprovalClaim, platformNamespace, "warning", "firing"}
}

// ArtifactRefused is a report the platform refused to store.
func ArtifactRefused() Alert {
	return Alert{NameArtifactRefused, platformNamespace, "warning", "firing"}
}

// LoopFailing is a controller loop failing the same way, or passing again.
func LoopFailing(firing bool) Alert {
	return Alert{NameLoopFailing, platformNamespace, "critical", statusOf(firing)}
}

// Fire appends one alert in the caller's transaction, so it stands or falls
// with the record it tells of. cause is that record's event, 0 for none. The
// caller has the controller actor.
func Fire(ctx context.Context, tx pgx.Tx, a Alert, cause int64) error {
	_, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: Stream(a.Name), Type: Type, Actor: actor, Cause: cause, Body: Body(a)})
	return err
}

// relayFailsToAlert is how many readings in a row must fail before the relay
// counts as unreachable: at one a minute, a restart of the Mac mini's plane
// says nothing.
const relayFailsToAlert = 3

// RelayReading is one look at the phone relay (GET /api/v1/alerts/relay).
type RelayReading struct {
	Reached, Enabled, HeartbeatOK bool
}

// RelayWatch mirrors the relay's own view onto the bus. The relay still pages
// the phone by itself when the Watchdog stops: it shares nothing with the
// cluster, and this watch is only the same fact where the rest can be seen.
type RelayWatch struct {
	Missing, Unreachable bool
	fails                int
}

// Step takes one reading and says what changed.
func (w *RelayWatch) Step(r RelayReading) []Alert {
	var out []Alert
	if !r.Reached {
		if w.fails++; w.fails >= relayFailsToAlert && !w.Unreachable {
			w.Unreachable = true
			out = append(out, relayAlert(NameRelayUnreachable, true))
		}
		return out
	}
	w.fails = 0
	if w.Unreachable {
		w.Unreachable = false
		out = append(out, relayAlert(NameRelayUnreachable, false))
	}
	if missing := r.Enabled && !r.HeartbeatOK; missing != w.Missing {
		w.Missing = missing
		out = append(out, relayAlert(NameHeartbeatMissing, missing))
	}
	return out
}

func relayAlert(name string, firing bool) Alert {
	return Alert{name, monitoringNamespace, "critical", statusOf(firing)}
}
