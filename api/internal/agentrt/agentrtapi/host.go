package agentrtapi

import (
	"context"
	"net/http"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/ownercard"
)

// host lends the domains' routes (planapi, coordapi) what every /agentrt route
// shares, read from the API at each request: its database and wiring, who a
// request is recorded as (writer, actorOf), the strict body readers, the
// answers and the mapping of an error to a status (fail). It implements
// planapi.Host, coordapi.Host and lineageapi.Host; nothing here is a domain's.
type host struct{ a *API }

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
