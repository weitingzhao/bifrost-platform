// Package agentrtapi is the HTTP face of the agent runtime's kernel (design
// v5 §5.2 「路由草图」, /api/v1/agentrt/…): the routes agentd calls to claim
// work and hand results over, artifact upload and download, and reads. Every
// write route maps to one store function, which writes the state it changes
// and its events in one transaction (a heartbeat only renews the lease, a
// liveness column, without an event); there is no route that sets a state.
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
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// API serves the routes. A nil DB means the platform has no agentrt
// database configured: every route answers 503, so the code can be deployed
// before the database is wired.
type API struct {
	DB   store.DB
	Head store.BranchHead
	// ClaimWait is how long a claim waits for work (long poll; v5: at most
	// 25 seconds, as the approvals executor); ClaimPoll how often it looks.
	ClaimWait, ClaimPoll time.Duration
	Logf                 func(format string, args ...any)
	// Ask creates the Owner's decision card for a plan change outside the
	// standing authorization; nil answers such a proposal 503.
	Ask store.Ask
}

const maxBody = 1 << 20 // an artifact is at most 1 MiB; JSON bodies are smaller

// abilities are what each kind of caller may do (v5 §5.2 鉴权); Mount and
// whoami both read them. agentd, whose token is bound to one host, claims,
// keeps its leases and hands results over; threads and seats (reporter and
// operator tokens) and the Owner (admin) propose missions and tasks; they and
// agentd upload artifacts; anyone authenticated reads and may stop, pause or
// drain; operators and the Owner drain hosts; only the Owner lifts a control.
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
	read, agentd, propose, upload := gate("read"), gate("claim"), gate("propose"), gate("upload")
	// Authentication first, so an anonymous write is refused as such even
	// when the database is not configured.
	cfg := a.configured
	r.Route("/agentrt", func(r chi.Router) {
		r.With(read, cfg).Get("/whoami", a.whoami)
		r.With(agentd, cfg).Post("/claim", a.claim)
		r.With(agentd, cfg).Post("/attempts/{id}/{action}", a.attempt)
		r.With(upload, cfg).Put("/artifacts/{id}", a.putArtifact)
		r.With(read, cfg).Get("/artifacts/{id}", a.getArtifact)
		r.With(propose, cfg).Post("/missions", a.proposeMission)
		r.With(read, cfg).Get("/missions/{id}", a.getMission)
		r.With(propose, cfg).Post("/missions/{id}/tasks", a.proposeTasks)
		r.With(read, cfg).Get("/tasks", a.listTasks)
		r.With(read, cfg).Get("/tasks/{id}", a.getTask)
		r.With(read, cfg).Get("/attempts/{id}", a.getAttempt)
		r.With(read, cfg).Get("/events", a.getEvents)
		r.With(read, cfg).Get("/plan", a.getPlan)
		r.With(read, cfg).Get("/plan/{id}", a.getPlanNode)
		r.With(propose, cfg).Post("/plan/proposals", a.proposePlanChange)
		r.With(read, cfg).Get("/controls", a.listControls)
		r.With(gate("stop"), cfg).Post("/controls", a.setControl)
		r.With(gate("lift"), cfg).Delete("/controls/{id}", a.liftControl)
		r.With(read, cfg).Get("/hosts", a.listHosts)
		r.With(read, cfg).Get("/progress", a.progress)
		r.With(gate("drain"), cfg).Post("/hosts/{id}/drain", a.drainHost)
	})
}

func (a *API) configured(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.DB == nil {
			writeError(w, http.StatusServiceUnavailable, "not_configured", "the agent runtime database is not configured")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// boundHost is the host of the calling agentd token; the body must name it.
func boundHost(w http.ResponseWriter, r *http.Request, named string) bool {
	host, ok := actuation.AgentdHost(actuation.PrincipalFromContext(r.Context()))
	if !ok {
		writeError(w, http.StatusForbidden, "forbidden", "only an agentd token bound to a host may call this")
		return false
	}
	if named != host {
		writeError(w, http.StatusForbidden, "wrong_host", fmt.Sprintf("the token is bound to host %s, the body names %s", host, named))
		return false
	}
	return true
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
	case errors.Is(err, store.ErrInvalid):
		writeError(w, http.StatusUnprocessableEntity, "invalid", err.Error())
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

// hostBody is the part every agentd body carries.
type hostBody struct {
	HostID string `json:"host_id"`
}

type tool struct {
	Executor string `json:"executor"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

type claimRequest struct {
	hostBody
	FreeSlots int    `json:"free_slots"`
	Tools     []tool `json:"tools"`
}

// claim is the long poll for work: it tries until a task is leased or the
// wait is over (204).
func (a *API) claim(w http.ResponseWriter, r *http.Request) {
	var req claimRequest
	if !decode(w, r, &req) || !boundHost(w, r, req.HostID) {
		return
	}
	cr := store.ClaimRequest{HostID: req.HostID, FreeSlots: req.FreeSlots}
	for _, t := range req.Tools {
		if t.Executor == "" || t.Provider == "" {
			writeError(w, http.StatusBadRequest, "bad_request", "every tool names its executor and its model provider")
			return
		}
		cr.Tools = append(cr.Tools, store.Tool{Executor: t.Executor, Provider: t.Provider, Model: t.Model})
	}
	wait, poll := a.ClaimWait, a.ClaimPoll
	if wait <= 0 || wait > 25*time.Second {
		wait = 25 * time.Second
	}
	if poll <= 0 {
		poll = time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), wait)
	defer cancel()
	for {
		cl, err := store.ClaimNext(ctx, a.DB, cr)
		if err != nil && ctx.Err() == nil {
			a.fail(w, err)
			return
		}
		if cl != nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"task_id": cl.TaskID, "attempt_id": cl.AttemptID, "attempt_no": cl.AttemptNo, "mission_id": cl.MissionID,
				"role": cl.Role, "repo": cl.Repo, "spec_artifact_id": cl.SpecArtifact, "base_commit": cl.BaseCommit, "work_id": cl.WorkID,
				"pushed_ref": cl.PushedRef, "effect_class": cl.Effect, "lease_epoch": cl.Epoch,
				"lease_expires_at": cl.LeaseExpiresAt, "hard_limit_s": int(cl.HardLimit / time.Second),
				"tool": tool{Executor: cl.Tool.Executor, Provider: cl.Tool.Provider, Model: cl.Tool.Model},
			})
			return
		}
		select {
		case <-ctx.Done():
			w.WriteHeader(http.StatusNoContent)
			return
		case <-time.After(poll):
		}
	}
}

// attemptBody holds every field an attempt route takes; each route reads
// its own. A heartbeat's phase and last line (v5 §5.2) are not taken yet:
// nothing keeps them until the progress bus (2.3) gives them a place, and a
// body that sends them is refused rather than answered as if they were kept
// (review 156 B1).
type attemptBody struct {
	hostBody
	Epoch         int64    `json:"lease_epoch"`
	Model         string   `json:"model"`
	Provider      string   `json:"model_provider"`
	CheckpointSeq int      `json:"checkpoint_seq"`
	Artifacts     []string `json:"artifacts"`
	TooLarge      bool     `json:"too_large"`
	ExitCode      *int     `json:"exit_code"`
	EndHow        string   `json:"end_how"`
	Conclusion    string   `json:"conclusion"`
	Report        string   `json:"report"`
	PushedRef     string   `json:"pushed_ref"`
	PushedCommit  string   `json:"pushed_commit"`
	GroupCleared  bool     `json:"group_cleared"`
}

func (a *API) attempt(w http.ResponseWriter, r *http.Request) {
	id, action := chi.URLParam(r, "id"), chi.URLParam(r, "action")
	var b attemptBody
	if !decode(w, r, &b) || !boundHost(w, r, b.HostID) {
		return
	}
	f := store.Fence{AttemptID: id, HostID: b.HostID, Epoch: b.Epoch}
	res := store.Result{Report: b.Report, Artifacts: b.Artifacts, Ref: b.PushedRef, Commit: b.PushedCommit}
	ctx := r.Context()
	var out map[string]any
	var err error
	switch action {
	case "heartbeat":
		var beat store.Beat
		if beat, err = store.Heartbeat(ctx, a.DB, f); err == nil {
			out = map[string]any{"lease_expires_at": beat.LeaseExpiresAt, "stop": beat.Stop}
		}
	case "start":
		if err = store.Start(ctx, a.DB, f, b.Model, b.Provider); err == nil {
			out = map[string]any{"started": true}
		}
	case "checkpoint":
		out, err = store.Checkpoint(ctx, a.DB, f, b.CheckpointSeq, b.Artifacts, b.TooLarge)
	case "computed":
		out, err = store.Computed(ctx, a.DB, f, b.ExitCode, b.EndHow, b.Conclusion)
	case "persist":
		out, err = store.Persist(ctx, a.DB, f, res, a.Head)
	case "submit":
		out, err = store.Submit(ctx, a.DB, f)
	case "salvage":
		out, err = store.Salvage(ctx, a.DB, id, b.HostID, res, a.Head)
	case "ended":
		out, err = store.Ended(ctx, a.DB, id, b.HostID, b.EndHow, b.GroupCleared)
	default:
		writeError(w, http.StatusNotFound, "not_found", "no such attempt route")
		return
	}
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// The artifact kinds agentd may upload: results, not specs or plans.
var agentdKinds = map[string]bool{"report": true, "verify": true, "review": true, "log_tail": true,
	"checkpoint_patch": true, "handover": true, "other": true}

// putArtifact stores a body under its sha256 (PUT /artifacts/{sha256}): the
// id in the path must be the hash of the body; the kind comes in the
// X-Artifact-Kind header and must be one the schema has; an artifact with a
// local path in it is refused unless its kind is one H8 lets through
// (store.LocalPath). The first upload of a body records artifact_put, as
// the writer (agentd as its host, others as for proposals); uploading it
// again is harmless.
func (a *API) putArtifact(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p := actuation.PrincipalFromContext(r.Context())
	_, isAgentd := actuation.AgentdHost(p)
	kind := r.Header.Get("X-Artifact-Kind")
	if p.Role == actuation.RoleAgentd && !isAgentd {
		writeError(w, http.StatusForbidden, "forbidden", "an agentd token must be bound to a host")
		return
	}
	if !store.ArtifactKinds[kind] {
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
	sum := sha256.Sum256(body)
	if id != "sha256:"+hex.EncodeToString(sum[:]) {
		writeError(w, http.StatusBadRequest, "hash", "the path is not the sha256 of the body")
		return
	}
	if p, found := store.LocalPath(kind, body); found {
		writeError(w, http.StatusUnprocessableEntity, "local_path", "a local path in the artifact: "+p)
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

func (a *API) getArtifact(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var media string
	var body []byte
	err := store.InTx(r.Context(), a.DB, func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `SELECT a.media_type, b.body FROM agentrt.artifact a JOIN agentrt.artifact_blob b USING (artifact_id)
		  WHERE a.artifact_id = $1`, id).Scan(&media, &body)
	})
	if err != nil {
		a.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", media)
	_, _ = w.Write(body)
}

func (a *API) getRow(w http.ResponseWriter, r *http.Request, q, id string) {
	var doc []byte
	err := store.InTx(r.Context(), a.DB, func(tx pgx.Tx) error { return tx.QueryRow(r.Context(), q, id).Scan(&doc) })
	if err != nil {
		a.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(doc)
}

func (a *API) getTask(w http.ResponseWriter, r *http.Request) {
	a.getRow(w, r, `SELECT to_jsonb(t) FROM agentrt.task t WHERE task_id = $1`, chi.URLParam(r, "id"))
}

func (a *API) getAttempt(w http.ResponseWriter, r *http.Request) {
	a.getRow(w, r, `SELECT to_jsonb(a) FROM agentrt.attempt a WHERE attempt_id = $1`, chi.URLParam(r, "id"))
}

// getEvents lists a stream's events after a sequence number, at most 500.
func (a *API) getEvents(w http.ResponseWriter, r *http.Request) {
	stream := r.URL.Query().Get("stream")
	var after int64
	if s := r.URL.Query().Get("after_seq"); s != "" {
		if _, err := fmt.Sscan(s, &after); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "after_seq is a number")
			return
		}
	}
	var doc []byte
	err := store.InTx(r.Context(), a.DB, func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `SELECT COALESCE(jsonb_agg(to_jsonb(e) ORDER BY e.seq), '[]') FROM (
		  SELECT * FROM agentrt.event WHERE event_stream_id = $1 AND seq > $2 ORDER BY seq LIMIT 500) e`, stream, after).Scan(&doc)
	})
	if err != nil {
		a.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(doc)
}
