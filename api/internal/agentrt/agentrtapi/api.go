// Package agentrtapi is the HTTP face of the agent runtime's kernel (design
// v5 §5.2 「路由草图」, /api/v1/agentrt/…): the routes agentd calls to claim
// work and hand results over, artifact upload and download, and reads. Every
// write route maps to one store function, which writes the state it changes
// and its events in one transaction (a heartbeat only renews the lease, a
// liveness column, without an event); there is no route that sets a state.
package agentrtapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actions"
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
	// standing authorization (nil answers such a proposal 503) and for a
	// mission outside the standing envelope (nil leaves it proposed).
	Ask store.Ask
	// Anchor is the fingerprint of the Owner's signing key a signed rule must
	// verify against (releasepolicy.OwnerKeyFingerprint); empty refuses every
	// signed rule.
	Anchor string
	// MainHead reads a repository's main where agentd clones it, the base of
	// a goal's task; nil answers POST /goals 503.
	MainHead MainHead
	// Actions keeps attempts' actions in redis-agents (W-31 2.3); nil keeps
	// none: a heartbeat's actions are refused in its answer, the read 503.
	Actions *actions.Store
	// Exams is the chief exam's question set (W-31 2.4 ④, exams.go), loaded
	// once at start; nil answers the exam routes 503.
	Exams store.ExamSet
	// AgentHeads reads the attempt branches agentd may be given to delete
	// (TD-383, prune.go); nil gives none. MainResults reads what a
	// repository's main holds of the runtime's results; nil keeps the
	// branches of accepted results that landed. Now is the clock the
	// cleanup's rule reads,
	// time.Now when nil.
	AgentHeads  AgentHeads
	MainResults store.MainResults
	Now         func() time.Time
}

const maxBody = 1 << 20 // an artifact is at most 1 MiB; JSON bodies are smaller

// claimPollLimit bounds one poll of a claim, which neither the long poll's
// deadline nor the client cuts (TD-399). From the handler's start, a claim
// works for at most its wait (25 seconds) and one last poll with the
// read-back of a failed commit (store.ConfirmLimit): 36 seconds, which
// leaves agentd's 40-second client timeout 4 for connecting, sending the
// request and reading the answer. A body read is not bounded here.
const claimPollLimit = 8 * time.Second

// abilities are what each kind of caller may do (v5 §5.2 鉴权); Mount and
// whoami both read them. agentd, whose token is bound to one host, claims,
// keeps its leases and hands results over; threads and seats (reporter and
// operator tokens) and the Owner (admin) propose missions, tasks and rules
// (and hand in a rule the Owner signed: the signature is the authority); they
// and agentd upload artifacts; anyone authenticated reads and may stop, pause,
// drain or tighten a rule; operators and the Owner drain hosts; only the Owner
// lifts a control and reads a vendor pool by hand.
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
		r.With(agentd, cfg).Post("/benchmark", a.recordBenchmark)
		r.With(agentd, cfg).Get("/prunes", a.prunes)
		r.With(agentd, cfg).Post("/prunes", a.reportPrunes)
		r.With(agentd, cfg).Get("/exams/{job}", a.issueExam)
		r.With(agentd, cfg).Post("/exams", a.recordExam)
		r.With(upload, cfg).Put("/artifacts/{id}", a.putArtifact)
		r.With(read, cfg).Get("/artifacts/{id}", a.getArtifact)
		r.With(propose, cfg).Post("/missions", a.proposeMission)
		r.With(read, cfg).Get("/missions/{id}", a.getMission)
		r.With(propose, cfg).Post("/missions/{id}/tasks", a.proposeTasks)
		r.With(propose, cfg).Post("/goals", a.proposeGoal)
		r.With(read, cfg).Get("/tasks", a.listTasks)
		r.With(read, cfg).Get("/tasks/{id}", a.getTask)
		r.With(read, cfg).Get("/attempts/{id}", a.getAttempt)
		r.With(read, cfg).Get("/attempts/{id}/actions", a.attemptActions)
		r.With(propose, cfg).Post("/messages", a.sendMessage)
		r.With(read, cfg).Get("/messages", a.readMessages)
		r.With(read, cfg).Get("/messages/thread", a.threadMessages)
		r.With(propose, cfg).Post("/messages/ack", a.ackMessages)
		r.With(read, cfg).Get("/events", a.getEvents)
		r.With(read, cfg).Get("/plan", a.getPlan)
		r.With(read, cfg).Get("/plan/{id}", a.getPlanNode)
		r.With(propose, cfg).Post("/plan/proposals", a.proposePlanChange)
		r.With(propose, cfg).Post("/plan/{id}/notes", a.postPlanNote)
		r.With(propose, cfg).Post("/plan/imports", a.proposePlanImport)
		r.With(read, cfg).Get("/controls", a.listControls)
		r.With(gate("stop"), cfg).Post("/controls", a.setControl)
		r.With(gate("lift"), cfg).Delete("/controls/{id}", a.liftControl)
		r.With(read, cfg).Get("/rules/{rule_id}", a.getRule)
		r.With(read, cfg).Post("/rules/{rule_id}/check", a.checkRule)
		r.With(propose, cfg).Post("/rules/{rule_id}/proposals", a.proposeRule)
		r.With(propose, cfg).Post("/rules/{rule_id}/sign", a.signRule)
		r.With(gate("stop"), cfg).Post("/rules/{rule_id}/tighten", a.tightenRule)
		r.With(read, cfg).Get("/hosts", a.listHosts)
		r.With(read, cfg).Get("/progress", a.progress)
		r.With(gate("drain"), cfg).Post("/hosts/{id}/drain", a.drainHost)
		r.With(read, cfg).Get("/pools", a.listPools)
		r.With(read, cfg).Get("/capabilities", a.capabilities)
		r.With(gate("measure"), cfg).Post("/pools/{id}/reading", a.recordReading)
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
	case errors.Is(err, store.ErrNotYours), errors.Is(err, store.ErrOtherHost):
		writeError(w, http.StatusForbidden, "not_yours", err.Error())
	case errors.Is(err, store.ErrInvalid), errors.Is(err, actions.ErrInvalid):
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
	entered := time.Now()
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
	// The wait counts from the request's start, the host's registration in
	// it, so the answer comes within the wait and one poll (TD-399).
	ctx, cancel := context.WithDeadline(r.Context(), entered.Add(wait))
	defer cancel()
	// The first valid claim of a token registers the host it is bound to.
	if err := store.RegisterAgentdHost(ctx, a.DB, req.HostID, actuation.PrincipalFromContext(r.Context()).Name); err != nil {
		a.fail(w, err)
		return
	}
	for {
		if r.Context().Err() != nil {
			return // the client went away: start no poll for it (one running may still lease it a task)
		}
		// Each poll runs to its end under its own bound, and the long poll
		// stops only between polls (TD-399): cut by the deadline while it
		// commits, pgx gives up on a commit whose outcome it does not know,
		// and the claim answered a lease the server rolled back, or 204 for
		// a poll the server committed. A commit that fails all the same (the
		// bound, the connection) is read back by the store.
		pctx, pcancel := context.WithTimeout(context.WithoutCancel(r.Context()), claimPollLimit)
		cl, err := store.ClaimNext(pctx, a.DB, cr)
		pcancel()
		if err != nil {
			a.fail(w, err)
			return
		}
		if cl != nil {
			body := map[string]any{
				"task_id": cl.TaskID, "attempt_id": cl.AttemptID, "attempt_no": cl.AttemptNo, "mission_id": cl.MissionID,
				"role": cl.Role, "repo": cl.Repo, "spec_artifact_id": cl.SpecArtifact, "base_commit": cl.BaseCommit, "work_id": cl.WorkID,
				"pushed_ref": cl.PushedRef, "effect_class": cl.Effect, "lease_epoch": cl.Epoch,
				"lease_expires_at": cl.LeaseExpiresAt, "hard_limit_s": int(cl.HardLimit / time.Second),
				"tool": tool{Executor: cl.Tool.Executor, Provider: cl.Tool.Provider, Model: cl.Tool.Model},
			}
			// What the task's earlier rounds failed (TD-405); an agentd from
			// before it ignores the field.
			if len(cl.Failed) > 0 {
				failed := make([]map[string]any, 0, len(cl.Failed))
				for _, f := range cl.Failed {
					failed = append(failed, map[string]any{"method": f.Method, "attempt_no": f.AttemptNo, "artifact_id": f.Evidence})
				}
				body["failed_checks"] = failed
			}
			// What the attempt a check checks handed in (TD-409); an agentd
			// from before it ignores the fields.
			if cl.SubjectReport != "" {
				body["subject_report_id"] = cl.SubjectReport
			}
			if cl.SubjectEffect != "" {
				body["subject_effect"] = cl.SubjectEffect
			}
			writeJSON(w, http.StatusOK, body)
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
// its own. A heartbeat may carry the executor's recent actions (W-31 2.3),
// kept in redis-agents for 24 hours; any other route that sends them is
// refused rather than answered as if they were kept (review 156 B1). The
// phase and last line of v5 §5.2 are not taken: the actions replace them.
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
	// Actions stays raw until the lease is renewed: actions that do not
	// parse never fail a heartbeat (review 264 A1).
	Actions json.RawMessage `json:"actions"`
	// An ending may carry what the attempt used and what its vendor said
	// about its pools (2.4 slice ①); only computed takes them. They stay raw
	// until then: a report that does not parse never fails the ending
	// (review 274 A2).
	Usage json.RawMessage `json:"usage"`
	Pools json.RawMessage `json:"pools"`
}

func present(raw json.RawMessage) bool { return len(raw) > 0 && string(raw) != "null" }

// usageReport parses an ending's usage and pool readings, strictly; what does
// not parse is dropped with the reason.
func usageReport(b attemptBody) (*store.Usage, []store.PoolReading, string) {
	var u *store.Usage
	var pools []store.PoolReading
	strict := func(raw json.RawMessage, v any) error {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		return dec.Decode(v)
	}
	if present(b.Usage) {
		u = &store.Usage{}
		if err := strict(b.Usage, u); err != nil {
			return nil, nil, "usage: " + err.Error()
		}
	}
	if present(b.Pools) {
		if err := strict(b.Pools, &pools); err != nil {
			return nil, nil, "pools: " + err.Error()
		}
	}
	return u, pools, ""
}

func (a *API) attempt(w http.ResponseWriter, r *http.Request) {
	id, action := chi.URLParam(r, "id"), chi.URLParam(r, "action")
	var b attemptBody
	if !decode(w, r, &b) || !boundHost(w, r, b.HostID) {
		return
	}
	if action != "heartbeat" && len(b.Actions) > 0 && string(b.Actions) != "null" {
		writeError(w, http.StatusBadRequest, "bad_request", "only a heartbeat carries actions")
		return
	}
	if action != "computed" && (present(b.Usage) || present(b.Pools)) {
		writeError(w, http.StatusBadRequest, "bad_request", "only computed carries usage and pool readings")
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
			a.keepActions(ctx, id, b.Actions, out)
		}
	case "start":
		if err = store.Start(ctx, a.DB, f, b.Model, b.Provider); err == nil {
			out = map[string]any{"started": true}
		}
	case "checkpoint":
		out, err = store.Checkpoint(ctx, a.DB, f, b.CheckpointSeq, b.Artifacts, b.TooLarge)
	case "computed":
		u, pools, refused := usageReport(b)
		if out, err = store.ComputedWith(ctx, a.DB, f, store.Ending{ExitCode: b.ExitCode, EndHow: b.EndHow, Conclusion: b.Conclusion,
			Usage: u, Pools: pools}); err == nil && refused != "" {
			out["usage_refused"] = refused
		}
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
	host, isAgentd := actuation.AgentdHost(p)
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
		a.refuse(w, r, host, kind, id, "local_path", "a local path in the artifact: "+p)
		return
	}
	if ref, found := store.WorkspaceRef(kind, body); found {
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

func (a *API) getArtifact(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var media, kind string
	var body []byte
	var exam bool
	host := actuation.PrincipalFromContext(r.Context()).Role == actuation.RoleAgentd
	err := store.InTx(r.Context(), a.DB, func(tx pgx.Tx) error {
		// Whether the exams' stream names it is read for an Agent host only
		// (review 412 C1: the stream is read by its id, then its bodies).
		return tx.QueryRow(r.Context(), `SELECT a.media_type, a.artifact_kind, b.body,
		    $3 AND EXISTS (SELECT 1 FROM agentrt.event e WHERE e.event_stream_id = $2 AND e.body_json->>'artifact_id' = a.artifact_id)
		  FROM agentrt.artifact a JOIN agentrt.artifact_blob b USING (artifact_id) WHERE a.artifact_id = $1`, id, store.ExamsStream, host).
			Scan(&media, &kind, &body, &exam)
	})
	if err != nil {
		a.fail(w, err)
		return
	}
	// An Agent host reads no exam artifact: an answer's id can be known to a
	// host that handed it in, and answers stay off the host (CUT-chief-exam
	// §5; review 406 A1). An exam artifact is one of kind exam or one the
	// exams' stream names, whatever kind its first upload gave it (the same
	// bytes put earlier as "other", review 409 A1). agentd keeps the paper
	// it was handed.
	if host && (kind == "exam" || exam) {
		writeError(w, http.StatusForbidden, "forbidden", "an agentd token does not read exam artifacts")
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

// hiddenFrom are the streams a principal of a role may not read: an Agent
// host's agentd does not read the exams' records (capability_exams: which
// rules an answer failed). The cut keeps per-rule results off the host
// (CUT-chief-exam §5), and an executor reads what its agentd's user can
// (TD-377), the token included.
func hiddenFrom(r *http.Request) []string {
	if actuation.PrincipalFromContext(r.Context()).Role == actuation.RoleAgentd {
		return []string{store.ExamsStream}
	}
	return []string{}
}

// getEvents lists a stream's events after a sequence number, at most 500.
func (a *API) getEvents(w http.ResponseWriter, r *http.Request) {
	stream := r.URL.Query().Get("stream")
	if stream == "" {
		a.getRecentEvents(w, r)
		return
	}
	if slices.Contains(hiddenFrom(r), stream) {
		writeError(w, http.StatusForbidden, "forbidden", "this token may not read stream "+stream)
		return
	}
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
