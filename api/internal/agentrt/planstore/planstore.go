// Package planstore is the plan domain's data access in the agent runtime:
// the plan tree and its changes (proposals, the Owner's card, decisions,
// imports, notes), the adopted plan view with its bindings and observations,
// a step's facts and sign-off, and the Progress projection. It opens no
// HTTP. Every state change is an event written through eventlog.Append, under
// the vocabulary this package declares (vocab.go). doc.go states the contract.
//
// It was the plan files of package store and is the same code (2026-11-17,
// plan core wall step 5). It imports the base (eventlog, actor, canon,
// artifact, strictjson, ...) and the plan domain's own packages, and never
// store: what it needs of another domain it declares as a port
// (planports.go), which that domain's side binds at start. Store imports
// this package and binds the ports whose providers it still holds.
package planstore

import (
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// timeText is a time as the ledger writes one (eventlog.TimeLayout), to the
// microsecond in UTC: store's one-line timeText, which the plan files used.
func timeText(t time.Time) string {
	return t.UTC().Truncate(time.Microsecond).Format(eventlog.TimeLayout)
}
