package server

import (
	"context"
	"net/http"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/deliveryview"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/externalwork"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/ownercard"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	lineageassembly "github.com/weitingzhao/bifrost-platform/api/internal/lineage/assembly"
)

// host lends the domains' routes what every /agentrt route shares, read from
// the API at each request: its database and wiring, who a request is
// recorded as (writer, actorOf), the strict body readers, the answers and
// the mapping of an error to a status (fail). It implements planapi.Host,
// coordapi.Host, lineageapi.Host, rulesapi.Host, deliveryapi.Host and
// recordsapi.Host. Two of recordsapi's are the lineage's checks bound here
// (ExternalDescription, NotOwnRecord); nothing else here is a domain's.
type host struct{ a *agentrtAPI }

func (h host) DB() eventlog.DB            { return h.a.DB }
func (h host) Ask() ownercard.Ask         { return h.a.Ask }
func (h host) Back() time.Time            { return h.a.back() }
func (h host) ClaimPoll() time.Duration   { return h.a.ClaimPoll }
func (h host) Logf(f string, args ...any) { h.a.logf(f, args...) }

func (h host) Sessions() func(context.Context) (any, error) { return h.a.Sessions }

func (h host) MailWake() func(addr string) (<-chan struct{}, func()) { return h.a.MailWake }

func (h host) Writer(r *http.Request) (actor.Writer, error)  { return writer(r) }
func (h host) ActorOf(r *http.Request) (actor.Writer, error) { return actorOf(r) }

func (h host) Decode(w http.ResponseWriter, r *http.Request, v any) bool { return decode(w, r, v) }

func (h host) DecodeCanonical(w http.ResponseWriter, r *http.Request, v any) bool {
	return decodeView(w, r, v)
}

func (h host) Document(w http.ResponseWriter, r *http.Request, q string, args ...any) {
	h.a.document(w, r, q, args...)
}

// DecodeCanonicalRaw is DecodeCanonical giving the bytes it read too.
func (h host) DecodeCanonicalRaw(w http.ResponseWriter, r *http.Request, v any) ([]byte, bool) {
	return decodeViewRaw(w, r, v)
}

func (h host) JSON(w http.ResponseWriter, status int, v any) { writeJSON(w, status, v) }

func (h host) Error(w http.ResponseWriter, status int, code, msg string) {
	writeError(w, status, code, msg)
}

func (h host) Fail(w http.ResponseWriter, err error) { h.a.fail(w, err) }

func (h host) Anchor() string   { return h.a.Anchor }
func (h host) DiskFloor() int64 { return h.a.DiskFloor }

func (h host) ConfigurationBoundaries() func(context.Context, string, string, []map[string]any) (map[string]any, error) {
	return h.a.ConfigurationBoundaries
}

func (h host) ProductionDelivery() func(context.Context) deliveryview.DeliveryProduction {
	return h.a.ProductionDelivery
}

func (h host) LimitedReporter(p actuation.Principal) bool { return limitedReporter(p) }

// ExternalDescription is the lineage's check of an external work
// description, for recordsapi.
func (h host) ExternalDescription(body []byte) error {
	return externalwork.ValidateExternalDescription(body)
}

// NotOwnRecord is the lineage's error for a record that is not the
// collector's own, for recordsapi.
func (h host) NotOwnRecord() error { return externalwork.ErrNotOwnRecord }

// recorderLogs binds the plan import's recorder logs (planapi.RecorderLogs)
// to the lineage, which reads, checks and keeps a log (recorderimport,
// externalwork); the plan proposes its import.
type recorderLogs struct{}

func (recorderLogs) Format() string { return lineageassembly.RecorderFormat }

func (recorderLogs) Request() any { return &lineageassembly.RecorderRequest{} }

func (recorderLogs) Prepare(request any) (planstore.RecorderLog, error) {
	log, err := externalwork.PrepareRecorderImport(*request.(*lineageassembly.RecorderRequest))
	return planstore.RecorderLog(log), err
}
