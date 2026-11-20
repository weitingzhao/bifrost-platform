// Package agentrtapi mounts the agent runtime's routes (/api/v1/agentrt):
// each route's ability, the identity a request writes as, the strict body
// readers, the answers and the mapping of a store's error to a status. The
// handlers are in their domains' packages (2026-11-19): the plan's, its
// import included, in planapi; the coordination's in coordapi; registered
// work in lineageapi; the signed rules in rulesapi (verification); GET
// /delivery in deliveryapi; the artifacts and the event reads in recordsapi
// (foundation). The one handler still here is whoami (identity). What stays
// binds one domain's port to another's code: the release gate's signature
// check for the rules, the rule store for the delivery boundaries
// (boundarysource.go), the lineage's recorder log for the plan import and
// its record checks for the artifacts (host.go). Dispatch claim, lease and
// result-handoff routes have been retired.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/coordapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/deliveryapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/deliveryview"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/externalwork"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/lineageapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/ownercard"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/recordsapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rulesapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/workregistration"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
)

// agentrtAPI serves the routes. A nil DB means the platform has no agentrt
// database configured: every route answers 503, so the code can be deployed
// before the database is wired.
type agentrtAPI struct {
	ConfigurationBoundaries func(context.Context, string, string, []map[string]any) (map[string]any, error)
	DB                      eventlog.DB
	// ClaimPoll, when set, replaces how often a waiting inbox read re-reads
	// the record (tests).
	ClaimPoll time.Duration
	// MailWake registers a waiter on an address: its channel receives when the
	// bus says mail reached the address; stop removes it. Nil: waiters only
	// re-read the record.
	MailWake func(addr string) (woken <-chan struct{}, stop func())
	// DiskFloor retains the historical disk boundary configuration.
	DiskFloor int64
	Logf      func(format string, args ...any)
	// Ask creates the Owner's decision card for a plan change outside the
	// standing authorization (nil answers such a proposal 503) and for a
	// mission outside the standing envelope (nil leaves it proposed).
	Ask ownercard.Ask
	// Anchor is the fingerprint of the Owner's signing key a signed rule must
	// verify against (releasepolicy.OwnerKeyFingerprint); empty refuses every
	// signed rule.
	Anchor string
	// Sessions counts the retained agent sessions by binding for the plan
	// view's coverage.sessions (R5:5.5-12 §3.2); nil, or a failed read,
	// leaves it out and says why in the warnings.
	Sessions func(context.Context) (any, error)
	// ProductionDelivery reads the current PROD release for GET /delivery;
	// nil means not wired, and the PROD stages say so.
	ProductionDelivery func(context.Context) deliveryview.DeliveryProduction
	// Back records when runtime routes became ready. Progress retains this
	// input for compatibility but no longer computes dispatch host presence.
	Back func() time.Time
}

// back is Back, or the zero time.
func (a *agentrtAPI) back() time.Time {
	if a.Back == nil {
		return time.Time{}
	}
	return a.Back()
}

const maxBody = 1 << 20 // an artifact is at most 1 MiB; JSON bodies are smaller

// abilities describe the roles exposed by Mount and whoami. Historical
// ability names remain for compatibility; they do not restore retired routes.
// The assess ability requests advice, not an answer or approval.
var abilities = []struct {
	name  string
	roles []actuation.Role
}{
	{"read", []actuation.Role{actuation.RoleViewer, actuation.RoleReporter, actuation.RoleOperator, actuation.RoleAdmin, actuation.RoleAgentd}},
	{"claim", []actuation.Role{actuation.RoleAgentd}},
	{"propose", []actuation.Role{actuation.RoleReporter, actuation.RoleOperator, actuation.RoleAdmin}},
	{"upload", []actuation.Role{actuation.RoleAgentd, actuation.RoleReporter, actuation.RoleOperator, actuation.RoleAdmin}},
	{"stop", []actuation.Role{actuation.RoleViewer, actuation.RoleReporter, actuation.RoleOperator, actuation.RoleAdmin, actuation.RoleAgentd}},
	{"drain", []actuation.Role{actuation.RoleOperator, actuation.RoleAdmin}},
	{"lift", []actuation.Role{actuation.RoleAdmin}},
	{"measure", []actuation.Role{actuation.RoleAdmin}},
	{"signoff", []actuation.Role{actuation.RoleAdmin}},
	{"assess", []actuation.Role{actuation.RoleAdmin}},
}

func may(name string) []actuation.Role {
	for _, ab := range abilities {
		if ab.name == name {
			return ab.roles
		}
	}
	panic("agentrtapi: no ability " + name)
}

// mountAgentrt adds the routes under /agentrt of r (r is the /api/v1 router), each
// behind the roles of its ability.
func mountAgentrt(r chi.Router, auth *actuation.AuthService, a *agentrtAPI) {
	gate := func(name string) func(http.Handler) http.Handler { return auth.RequireAny(may(name)...) }
	read, propose, upload := gate("read"), gate("propose"), gate("upload")
	external := propose
	propose = func(next http.Handler) http.Handler { return external(proposersOnly(next)) }
	// Authentication first, so an anonymous write is refused as such even
	// when the database is not configured. Every route's parameters are read
	// as the client meant them (pathParams).
	cfg := func(next http.Handler) http.Handler { return a.configured(pathParams(next)) }
	// The handlers are in their domains' packages (2026-11-19, OWNER_LAW
	// #28); their routes and abilities are here with every other, and so are
	// the bindings of one domain's port to another's code.
	plan := planapi.Routes{Host: host{a}, WorkSpans: workregistration.WorkSpans, RecorderLogs: recorderLogs{}}
	coordination := coordapi.Routes{Host: host{a}}
	lineage := lineageapi.Routes{Host: host{a}}
	rules := rulesapi.Routes{Host: host{a}, Verify: releasepolicy.VerifyAnchored}
	delivery := deliveryapi.Routes{Host: host{a}, Boundaries: boundarySource}
	records := recordsapi.Routes{Host: host{a}}
	r.Route("/agentrt", func(r chi.Router) {
		r.With(read, cfg).Get("/whoami", a.whoami)
		r.With(upload, cfg).Put("/artifacts/{id}", records.HandlePutArtifact)
		r.With(read, cfg).Get("/artifacts", records.HandleSearchArtifacts)
		r.With(read, cfg).Get("/artifacts/{id}", records.HandleGetArtifact)
		r.With(propose, cfg).Post("/messages", coordination.HandleSendMessage)
		r.With(read, cfg).Get("/messages", coordination.HandleReadMessages)
		r.With(read, cfg).Get("/messages/thread", coordination.HandleThreadMessages)
		r.With(propose, cfg).Post("/messages/ack", coordination.HandleAckMessages)
		r.With(read, cfg).Get("/events", records.HandleGetEvents)
		r.With(read, cfg).Get("/plan", plan.HandleGetPlan)
		r.With(read, cfg).Get("/plan-view", plan.HandleGetPlanView)
		r.With(read, cfg).Get("/delivery", delivery.HandleGetDelivery)
		r.With(read, cfg).Get("/plan-pace", plan.HandleGetPlanPace)
		r.With(read, cfg).Get("/external-work/{id}", lineage.HandleGetExternalWork)
		r.With(external, cfg).Post("/external-work", lineage.HandleRegisterExternalWork)
		r.With(external, cfg).Post("/external-work/{id}/events", lineage.HandlePostExternalEvent)
		r.With(read, cfg).Get("/plan/{id}", plan.HandleGetPlanNode)
		r.With(propose, cfg).Post("/plan/proposals", plan.HandleProposePlanChange)
		r.With(propose, cfg).Post("/plan/{id}/notes", plan.HandlePostPlanNote)
		r.With(gate("signoff"), cfg).Post("/plan/{id}/signoff", plan.HandleSignOffStep)
		r.With(propose, cfg).Post("/plan/imports", plan.HandleProposePlanImport)
		r.With(read, cfg).Get("/controls", coordination.HandleListControls)
		r.With(gate("stop"), cfg).Post("/controls", coordination.HandleSetControl)
		r.With(gate("lift"), cfg).Delete("/controls/{id}", coordination.HandleLiftControl)
		r.With(read, cfg).Get("/rules/{rule_id}", rules.HandleGetRule)
		r.With(read, cfg).Post("/rules/{rule_id}/check", rules.HandleCheckRule)
		r.With(propose, cfg).Post("/rules/{rule_id}/proposals", rules.HandleProposeRule)
		r.With(propose, cfg).Post("/rules/{rule_id}/sign", rules.HandleSignRule)
		r.With(gate("stop"), cfg).Post("/rules/{rule_id}/tighten", rules.HandleTightenRule)
		r.With(read, cfg).Get("/progress", plan.HandleProgress)
		r.With(read, cfg).Get("/seats", coordination.HandleSeatStatuses)
	})
}

// pathParams unescapes a route's parameters when the client escaped the
// path. The router then matches the escaped path (chi reads RawPath), so a
// browser's encodeURIComponent sends sha256%3A…, R2%3A2.1-3 or routing%3Adev
// and the parameter arrives escaped, naming no row: on 2026-10-21 the plan's
// import request read 404 on PROD and Work showed no plan. Only an escaped
// path is unescaped, once, so a parameter read from the decoded path is not
// decoded twice (reviews 281 A1, 285). One not escaped right is a 400. Every
// route of Mount takes it through cfg.
func pathParams(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rctx := chi.RouteContext(r.Context()); r.URL.RawPath != "" && rctx != nil {
			for i, v := range rctx.URLParams.Values {
				u, err := url.PathUnescape(v)
				if err != nil {
					writeError(w, http.StatusBadRequest, "bad_request", fmt.Sprintf("the %s in the path is not escaped right", rctx.URLParams.Keys[i]))
					return
				}
				rctx.URLParams.Values[i] = u
			}
		}
		next.ServeHTTP(w, r)
	})
}

// agentrtReady is a database that may not be connected yet: the platform keeps
// connecting in the background, and until it has, the routes answer 503.
type agentrtReady interface{ Ready() bool }

func (a *agentrtAPI) configured(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.DB == nil {
			writeError(w, http.StatusServiceUnavailable, "not_configured", "the agent runtime database is not configured")
			return
		}
		if late, ok := a.DB.(agentrtReady); ok && !late.Ready() {
			writeError(w, http.StatusServiceUnavailable, "not_connected", "the agent runtime database is not connected yet; the platform keeps trying")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *agentrtAPI) logf(format string, args ...any) {
	if a.Logf != nil {
		a.Logf(format, args...)
	}
}

// fail maps a store error to a status: a lease not held, a reused key, a
// salvage still in grace and a state that does not fit are 409 with a code
// agentd acts on; a proposal that breaks its own rules or a guard of the
// database is 422 with the rule it broke (T32 through the interface); a
// missing row is 404; anything else is 500 and logged.
func (a *agentrtAPI) fail(w http.ResponseWriter, err error) {
	var pg *pgconn.PgError
	switch {
	case errors.Is(err, actor.ErrNotYours), errors.Is(err, externalwork.ErrNotOwnRecord):
		writeError(w, http.StatusForbidden, "not_yours", err.Error())
	case errors.Is(err, planstore.ErrNotOwner):
		writeError(w, http.StatusForbidden, "not_owner", err.Error())
	case errors.Is(err, externalwork.ErrIdentityRequired):
		writeError(w, http.StatusBadRequest, "identity_required", err.Error())
	case errors.Is(err, externalwork.ErrIdentityMismatch):
		writeError(w, http.StatusConflict, "identity_mismatch", err.Error())
	case errors.Is(err, eventlog.ErrInvalid):
		writeError(w, http.StatusUnprocessableEntity, "invalid", err.Error())
	case errors.Is(err, store.ErrUnsigned):
		writeError(w, http.StatusForbidden, "unsigned", err.Error())
	case errors.Is(err, planstore.ErrAsk):
		writeError(w, http.StatusServiceUnavailable, "owner_unreachable", err.Error())
	case errors.Is(err, planstore.ErrBusy):
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusServiceUnavailable, "busy", err.Error())
	case errors.As(err, &pg) && constraintCodes[pg.Code]:
		writeError(w, http.StatusUnprocessableEntity, "constraint", pg.Message)
	case errors.Is(err, actor.ErrConflict):
		writeError(w, http.StatusConflict, "key_conflict", err.Error())
	case errors.Is(err, eventlog.ErrState):
		writeError(w, http.StatusConflict, "state", err.Error())
	case errors.Is(err, pgx.ErrNoRows):
		writeError(w, http.StatusNotFound, "not_found", "no such record")
	default:
		a.logf("agentrt api: %v", err)
		writeError(w, http.StatusInternalServerError, "internal", "internal error")
	}
}

// constraintCodes are the database errors that say a write broke a rule:
// not null, foreign key, unique and check violations, and what the guard
// triggers raise.
var constraintCodes = map[string]bool{"23502": true, "23503": true, "23505": true, "23514": true, "P0001": true}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": code, "message": msg})
}

// decode reads a JSON body of at most maxBody bytes into v, refusing unknown
// fields.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "body: "+err.Error())
		return false
	}
	return true
}

// mainHead reads the commit the main branch of a repository points at. The
// head release and the evidence reads use the server's reader of this type.
type mainHead func(ctx context.Context, repo string) (string, error)
