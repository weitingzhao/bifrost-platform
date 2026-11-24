// Package recordsapi is the foundation's HTTP for the agent runtime's
// records: the artifacts (PUT /agentrt/artifacts/{id}, GET /agentrt/artifacts,
// GET /agentrt/artifacts/{id}) and the event ledger's reads (GET
// /agentrt/events), with who may read what (an Agent host reads no exam
// artifact or record; no role reads an issue's or an intake's stream here; a
// collector reads only the artifacts it stored first).
//
// The handlers were agentrtapi's (putArtifact of api.go, reads.go,
// artifacts.go, events.go) and are the same code (OWNER_LAW #95, 2026-11-19:
// a domain's routes sit in its own package, so its contract can say what it
// offers). The artifact store and the ledger are foundation's (artifact,
// eventlog); agentrtapi still mounts the routes, behind the same abilities,
// and lends them what every /agentrt route shares (Host). The two checks that
// are the lineage's (a reporter's external work description, a record that is
// not the collector's own) come through Host, so this package imports no
// lineage package.
package recordsapi

import (
	"net/http"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// Host is what the agent runtime's HTTP layer lends the record routes. Each
// read is live: the platform connects the database after the routes are
// mounted.
type Host interface {
	// DB is the agent runtime's database.
	DB() eventlog.DB
	// ActorOf is who a request's write is recorded as (an agentd token as
	// its host).
	ActorOf(*http.Request) (actor.Writer, error)
	// LimitedReporter says whether p is a reporter that is no seat: it may
	// only upload an external work description.
	LimitedReporter(p actuation.Principal) bool
	// ExternalDescription checks that body is an external work description
	// (the lineage's check).
	ExternalDescription(body []byte) error
	// NotOwnRecord is the error a collector gets for a record that is not
	// its own (the lineage's).
	NotOwnRecord() error
	// JSON answers v with status.
	JSON(w http.ResponseWriter, status int, v any)
	// Error answers {"error": code, "message": msg} with status.
	Error(w http.ResponseWriter, status int, code, msg string)
	// Fail answers an error of the store with its status.
	Fail(w http.ResponseWriter, err error)
	// Logf logs.
	Logf(format string, args ...any)
}

// Routes are the foundation's handlers for the agent runtime's records.
type Routes struct{ Host Host }

const maxBody = 1 << 20 // an artifact is at most 1 MiB
