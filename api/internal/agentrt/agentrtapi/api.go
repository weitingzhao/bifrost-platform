// Package agentrtapi serves plans, rules, evidence, artifacts and coordination.
// Dispatch claim, lease and result-handoff routes have been retired.
package agentrtapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/artifact"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/deliveryview"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/externalwork"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// API serves the routes. A nil DB means the platform has no agentrt
// database configured: every route answers 503, so the code can be deployed
// before the database is wired.
type API struct {
	ConfigurationBoundaries func(context.Context, string, string, []map[string]any) (map[string]any, error)
	DB                      store.DB
	// ClaimWait and ClaimPoll retain legacy configuration fields. No dispatch
	// claim route runs a long poll now.
	ClaimWait, ClaimPoll time.Duration
	// DiskFloor retains the historical disk boundary configuration.
	DiskFloor int64
	Logf      func(format string, args ...any)
	// Ask creates the Owner's decision card for a plan change outside the
	// standing authorization (nil answers such a proposal 503) and for a
	// mission outside the standing envelope (nil leaves it proposed).
	Ask store.Ask
	// Anchor is the fingerprint of the Owner's signing key a signed rule must
	// verify against (releasepolicy.OwnerKeyFingerprint); empty refuses every
	// signed rule.
	Anchor string
	// Now is the clock; time.Now when nil.
	Now func() time.Time
	// Sessions counts the retained agent sessions by binding for the plan
	// view's coverage.sessions (R5:5.5-12 §3.2); nil, or a failed read,
	// leaves it out and says why in the warnings.
	Sessions func(context.Context) (any, error)
	// ProductionDelivery reads the current PROD release for GET /delivery;
	// nil means not wired, and the PROD stages say so.
	ProductionDelivery func(context.Context) deliveryview.DeliveryProduction
	// PinReason is what an implement task pinned to the coordinator without
	// a pin_reason gets (AGENTRT_PIN_REASON, store/pinreason.go); "" is warn.
	PinReason store.PinReasonMode
	// Back records when runtime routes became ready. Progress retains this
	// input for compatibility but no longer computes dispatch host presence.
	Back func() time.Time
}

// back is Back, or the zero time.
func (a *API) back() time.Time {
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

// Mount adds the routes under /agentrt of r (r is the /api/v1 router), each
// behind the roles of its ability.
func Mount(r chi.Router, auth *actuation.AuthService, a *API) {
	gate := func(name string) func(http.Handler) http.Handler { return auth.RequireAny(may(name)...) }
	read, propose, upload := gate("read"), gate("propose"), gate("upload")
	external := propose
	propose = func(next http.Handler) http.Handler { return external(proposersOnly(next)) }
	// Authentication first, so an anonymous write is refused as such even
	// when the database is not configured. Every route's parameters are read
	// as the client meant them (pathParams).
	cfg := func(next http.Handler) http.Handler { return a.configured(pathParams(next)) }
	r.Route("/agentrt", func(r chi.Router) {
		r.With(read, cfg).Get("/whoami", a.whoami)
		r.With(upload, cfg).Put("/artifacts/{id}", a.putArtifact)
		r.With(read, cfg).Get("/artifacts", a.searchArtifacts)
		r.With(read, cfg).Get("/artifacts/{id}", a.getArtifact)
		r.With(propose, cfg).Post("/messages", a.sendMessage)
		r.With(read, cfg).Get("/messages", a.readMessages)
		r.With(read, cfg).Get("/messages/thread", a.threadMessages)
		r.With(propose, cfg).Post("/messages/ack", a.ackMessages)
		r.With(read, cfg).Get("/events", a.getEvents)
		r.With(read, cfg).Get("/plan", a.getPlan)
		r.With(read, cfg).Get("/plan-view", a.getPlanView)
		r.With(read, cfg).Get("/delivery", a.getDelivery)
		r.With(read, cfg).Get("/plan-pace", a.getPlanPace)
		r.With(read, cfg).Get("/external-work/{id}", a.getExternalWork)
		r.With(external, cfg).Post("/external-work", a.registerExternalWork)
		r.With(external, cfg).Post("/external-work/{id}/events", a.postExternalEvent)
		r.With(read, cfg).Get("/plan/{id}", a.getPlanNode)
		r.With(propose, cfg).Post("/plan/proposals", a.proposePlanChange)
		r.With(propose, cfg).Post("/plan/{id}/notes", a.postPlanNote)
		r.With(gate("signoff"), cfg).Post("/plan/{id}/signoff", a.signOffStep)
		r.With(propose, cfg).Post("/plan/imports", a.proposePlanImport)
		r.With(read, cfg).Get("/controls", a.listControls)
		r.With(gate("stop"), cfg).Post("/controls", a.setControl)
		r.With(gate("lift"), cfg).Delete("/controls/{id}", a.liftControl)
		r.With(read, cfg).Get("/rules/{rule_id}", a.getRule)
		r.With(read, cfg).Post("/rules/{rule_id}/check", a.checkRule)
		r.With(propose, cfg).Post("/rules/{rule_id}/proposals", a.proposeRule)
		r.With(propose, cfg).Post("/rules/{rule_id}/sign", a.signRule)
		r.With(gate("stop"), cfg).Post("/rules/{rule_id}/tighten", a.tightenRule)
		r.With(read, cfg).Get("/progress", a.progress)
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

// Ready is a database that may not be connected yet: the platform keeps
// connecting in the background, and until it has, the routes answer 503.
type Ready interface{ Ready() bool }

func (a *API) configured(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.DB == nil {
			writeError(w, http.StatusServiceUnavailable, "not_configured", "the agent runtime database is not configured")
			return
		}
		if late, ok := a.DB.(Ready); ok && !late.Ready() {
			writeError(w, http.StatusServiceUnavailable, "not_connected", "the agent runtime database is not connected yet; the platform keeps trying")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *API) logf(format string, args ...any) {
	if a.Logf != nil {
		a.Logf(format, args...)
	}
}

// fail maps a store error to a status: a lease not held, a reused key, a
// salvage still in grace and a state that does not fit are 409 with a code
// agentd acts on; a proposal that breaks its own rules or a guard of the
// database is 422 with the rule it broke (T32 through the interface); a
// missing row is 404; anything else is 500 and logged.
func (a *API) fail(w http.ResponseWriter, err error) {
	var pg *pgconn.PgError
	switch {
	case errors.Is(err, store.ErrNotYours), errors.Is(err, externalwork.ErrNotOwnRecord):
		writeError(w, http.StatusForbidden, "not_yours", err.Error())
	case errors.Is(err, store.ErrNotOwner):
		writeError(w, http.StatusForbidden, "not_owner", err.Error())
	case errors.Is(err, store.ErrExternalHost):
		writeError(w, http.StatusForbidden, "external_host", err.Error())
	case errors.Is(err, store.ErrNotExternal):
		writeError(w, http.StatusForbidden, "forbidden", err.Error())
	case errors.Is(err, externalwork.ErrIdentityRequired):
		writeError(w, http.StatusBadRequest, "identity_required", err.Error())
	case errors.Is(err, externalwork.ErrIdentityMismatch):
		writeError(w, http.StatusConflict, "identity_mismatch", err.Error())
	case errors.Is(err, store.ErrNotPinned):
		writeError(w, http.StatusForbidden, "not_pinned", err.Error())
	case errors.Is(err, store.ErrNoLease):
		writeError(w, http.StatusConflict, "no_lease", err.Error())
	case errors.Is(err, store.ErrCoreMismatch):
		writeError(w, http.StatusUnprocessableEntity, "core_mismatch", err.Error())
	case errors.Is(err, store.ErrMixedSource):
		writeError(w, http.StatusUnprocessableEntity, "mixed_source_not_supported", err.Error())
	case errors.Is(err, store.ErrPinNotPermitted):
		writeError(w, http.StatusUnprocessableEntity, "pin_not_permitted", err.Error())
	case errors.Is(err, store.ErrInvalid):
		writeError(w, http.StatusUnprocessableEntity, "invalid", err.Error())
	case errors.Is(err, store.ErrUnsigned):
		writeError(w, http.StatusForbidden, "unsigned", err.Error())
	case errors.Is(err, store.ErrAsk):
		writeError(w, http.StatusServiceUnavailable, "owner_unreachable", err.Error())
	case errors.Is(err, store.ErrBusy):
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusServiceUnavailable, "busy", err.Error())
	case errors.As(err, &pg) && constraintCodes[pg.Code]:
		writeError(w, http.StatusUnprocessableEntity, "constraint", pg.Message)
	case errors.Is(err, store.ErrFenced):
		writeError(w, http.StatusConflict, "fenced", err.Error())
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "key_conflict", err.Error())
	case errors.Is(err, store.ErrInGrace):
		writeError(w, http.StatusConflict, "in_grace", err.Error())
	case errors.Is(err, store.ErrState):
		writeError(w, http.StatusConflict, "state", err.Error())
	case errors.Is(err, store.ErrNoHead):
		writeError(w, http.StatusServiceUnavailable, "no_head_reader", err.Error())
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

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

// GrantedSlotsHeader carries a claim's granted_slots on every answer, the
// 204 of a long poll that leased nothing included (store.GrantedSlots), so
// agentd learns its count without a lease.
const GrantedSlotsHeader = "X-Agentrt-Granted-Slots"

// The artifact kinds agentd may upload: results, not specs or plans.
var agentdKinds = map[string]bool{"report": true, "verify": true, "review": true, "log_tail": true,
	"checkpoint_patch": true, "handover": true, "other": true}

// putArtifact stores a body under its sha256 (PUT /artifacts/{sha256}): the
// id in the path must be the hash of the body; the kind comes in the
// X-Artifact-Kind header and must be one the schema has; an artifact with a
// local path in it is refused unless its kind is one H8 lets through
// (artifact.LocalPath). The first upload of a body records artifact_put, as
// the writer (agentd as its host, others as for proposals); uploading it
// again is harmless.
func (a *API) putArtifact(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p := actuation.PrincipalFromContext(r.Context())
	host, isAgentd := actuation.AgentdHost(p)
	kind := r.Header.Get("X-Artifact-Kind")
	if limitedReporter(p) && kind != "other" {
		writeError(w, http.StatusForbidden, "forbidden", "a reporter may only upload an external work description")
		return
	}
	if p.Role == actuation.RoleAgentd && !isAgentd {
		writeError(w, http.StatusForbidden, "forbidden", "an agentd token must be bound to a host")
		return
	}
	if !artifact.ArtifactKinds[kind] {
		writeError(w, http.StatusBadRequest, "kind", fmt.Sprintf("X-Artifact-Kind %q is not an artifact kind", kind))
		return
	}
	if isAgentd && !agentdKinds[kind] {
		writeError(w, http.StatusForbidden, "kind", "agentd may not upload artifacts of kind "+kind)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "an artifact is at most 1 MiB")
		return
	}
	if limitedReporter(p) {
		if err := externalwork.ValidateExternalDescription(body); err != nil {
			writeError(w, http.StatusForbidden, "forbidden", "a reporter may only upload an external work description")
			return
		}
	}
	sum := sha256.Sum256(body)
	if id != "sha256:"+hex.EncodeToString(sum[:]) {
		writeError(w, http.StatusBadRequest, "hash", "the path is not the sha256 of the body")
		return
	}
	if p, found := artifact.LocalPath(kind, body); found {
		a.refuse(w, r, host, kind, id, "local_path", "a local path in the artifact: "+p)
		return
	}
	if ref, found := artifact.WorkspaceRef(kind, body); found {
		a.refuse(w, r, host, kind, id, "workspace_ref", "the artifact refers to a task's workspace: "+ref)
		return
	}
	media := r.Header.Get("Content-Type")
	if media == "" {
		media = "application/octet-stream"
	}
	who, err := actorOf(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	_, created, err := store.UploadArtifact(r.Context(), a.DB, who, kind, media, body)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"artifact_id": id, "created": created})
}

// refuse answers an artifact refused by rule (422). A report agentd uploads
// is recorded as refused on its host's stream (store.RefusedReport, TD-382),
// and an attempt of the host that then ends without its result says so; the
// answer is the refusal whatever the record does.
func (a *API) refuse(w http.ResponseWriter, r *http.Request, host, kind, id, rule, message string) {
	if host != "" {
		if _, err := store.RefusedReport(r.Context(), a.DB, host, kind, id, rule); err != nil {
			a.logf("agentrt: the refusal of %s from host %s was not recorded: %v", id, host, err)
		}
	}
	writeError(w, http.StatusUnprocessableEntity, rule, message)
}

// MainHead reads the commit the main branch of a repository points at. The
// head release and the evidence reads use the server's reader of this type.
type MainHead func(ctx context.Context, repo string) (string, error)
