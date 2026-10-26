package agentrtapi

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planimport"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/recorderimport"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The plan, read (v5 §5.2: GET /plan?root=R2, GET /plan/{plan_node_id}: the
// tree and each step's state with the event it came from).

// stateEvent is the event a node's state came from, as JSON: the last state
// change, or, for a node whose state never changed, its creation.
const stateEvent = `(SELECT jsonb_build_object('event_id', e.event_id, 'event_type', e.event_type, 'actor_id', e.actor_id,
	  'at', e.at, 'body', e.body_json) FROM agentrt.event e
	  WHERE e.event_id = n.state_event_id
	     OR (n.state_event_id IS NULL AND e.plan_node_id = n.plan_node_id AND e.event_type = 'plan_node_created')
	  ORDER BY e.event_id DESC LIMIT 1)`

// taskCounts is a node's tasks counted by state, as JSON.
const taskCounts = `COALESCE((SELECT jsonb_object_agg(state, k) FROM (SELECT state, count(*) AS k FROM agentrt.task
	  WHERE plan_node_id = n.plan_node_id GROUP BY state) s), '{}'::jsonb)`

// signoffs is a step's sign-offs (owner_signoff events: ref, by, at,
// event_id, oldest first) under "signoffs", merged into its node; a node with
// none gets no key.
const signoffs = `COALESCE((SELECT jsonb_build_object('signoffs', jsonb_agg(jsonb_build_object('ref', e.body_json->>'ref',
	  'by', e.actor_id, 'at', e.at, 'event_id', e.event_id) ORDER BY e.event_id)) FROM agentrt.event e
	  WHERE e.plan_node_id = n.plan_node_id AND e.event_type = 'owner_signoff' HAVING count(*) > 0), '{}'::jsonb)`

// getPlan is GET /plan: without root, the roots (rounds and the architecture
// root); with root, that node and everything under it, depth first in
// ordinal order, each with its depth, state event, task counts and sign-offs. A root
// there is not is 404.
func (a *API) getPlan(w http.ResponseWriter, r *http.Request) {
	root := r.URL.Query().Get("root")
	if root == "" {
		a.list(w, r, `SELECT jsonb_build_object('roots', COALESCE(jsonb_agg(to_jsonb(n) ORDER BY n.node_kind, n.ordinal, n.plan_node_id),
		  '[]'::jsonb)) FROM agentrt.plan_node n WHERE n.parent_plan_node_id IS NULL`)
		return
	}
	a.getRow(w, r, `WITH RECURSIVE tree AS (
	    SELECT p.plan_node_id, ARRAY[p.ordinal] AS path, 0 AS depth FROM agentrt.plan_node p WHERE p.plan_node_id = $1
	    UNION ALL
	    SELECT c.plan_node_id, t.path || c.ordinal, t.depth + 1
	      FROM agentrt.plan_node c JOIN tree t ON c.parent_plan_node_id = t.plan_node_id)
	  SELECT jsonb_build_object('root', $1::text, 'nodes', jsonb_agg(to_jsonb(n) || jsonb_build_object('depth', t.depth,
	      'state_event', `+stateEvent+`, 'task_counts', `+taskCounts+`) || `+signoffs+` ORDER BY t.path))
	  FROM tree t JOIN agentrt.plan_node n USING (plan_node_id)
	  HAVING count(*) > 0`, root)
}

// getPlanNode is GET /plan/{id}: the node with its state event, its
// children, its tasks, what it is bound to, its sign-offs and its last 50 events.
func (a *API) getPlanNode(w http.ResponseWriter, r *http.Request) {
	a.getRow(w, r, `SELECT to_jsonb(n) || jsonb_build_object(
	    'state_event', `+stateEvent+`,
	    'task_counts', `+taskCounts+`,
	    'children', COALESCE((SELECT jsonb_agg(jsonb_build_object('plan_node_id', c.plan_node_id, 'node_kind', c.node_kind,
	        'label', c.label, 'title', c.title, 'state', c.state) ORDER BY c.ordinal)
	      FROM agentrt.plan_node c WHERE c.parent_plan_node_id = n.plan_node_id), '[]'::jsonb),
	    'tasks', COALESCE((SELECT jsonb_agg(jsonb_build_object('task_id', t.task_id, 'title', t.title, 'role', t.role,
	        'state', t.state, 'mission_id', t.mission_id) ORDER BY t.created_at, t.task_id)
	      FROM agentrt.task t WHERE t.plan_node_id = n.plan_node_id), '[]'::jsonb),
	    'bindings', COALESCE((SELECT jsonb_agg(jsonb_build_object('kind', b.bound_kind, 'ref', b.bound_ref) ORDER BY b.plan_node_binding_id)
	      FROM agentrt.plan_node_binding b WHERE b.plan_node_id = n.plan_node_id), '[]'::jsonb),
	    'events', COALESCE((SELECT jsonb_agg(jsonb_build_object('event_id', e.event_id, 'event_type', e.event_type,
	        'actor_id', e.actor_id, 'at', e.at, 'body', e.body_json) ORDER BY e.event_id)
	      FROM (SELECT * FROM agentrt.event WHERE plan_node_id = n.plan_node_id ORDER BY event_id DESC LIMIT 50) e), '[]'::jsonb)) || `+signoffs+`
	  FROM agentrt.plan_node n WHERE n.plan_node_id = $1`, chi.URLParam(r, "id"))
}

type planProposalBody struct {
	IdempotencyKey string             `json:"idempotency_key"`
	Root           string             `json:"root"`
	Reason         string             `json:"reason"`
	Changes        []store.PlanChange `json:"changes"`
}

// proposePlanChange is POST /plan/proposals: plan_change_proposed; within
// the standing authorization decided and applied at once, otherwise tried,
// rolled back, and put to the Owner as a decision card.
func (a *API) proposePlanChange(w http.ResponseWriter, r *http.Request) {
	var b planProposalBody
	if !decodeView(w, r, &b) {
		return
	}
	who, err := writer(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	out, err := store.ProposePlanChange(r.Context(), a.DB, who, b.IdempotencyKey, b.Root, b.Reason, b.Changes, a.Ask)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

type planNoteBody struct {
	IdempotencyKey string `json:"idempotency_key"`
	Text           string `json:"text"`
}

// postPlanNote is POST /plan/{id}/notes: what an architecture row actually
// is now, in words (W-31 2.5 G2 A), appended as a note event on the row;
// GET /progress shows the latest beside the row's tick. Only an architecture
// row takes one: a node there is not is 404, another kind 422. Who may
// propose a plan change may write one, recorded as the proposer is; the key
// is the writer's, as for proposals.
func (a *API) postPlanNote(w http.ResponseWriter, r *http.Request) {
	var b planNoteBody
	if !decode(w, r, &b) {
		return
	}
	who, err := writer(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	out, err := store.PostPlanNote(r.Context(), a.DB, who, b.IdempotencyKey, chi.URLParam(r, "id"), b.Text)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

type signoffBody struct {
	Ref string `json:"ref"`
}

// signOffStep is POST /plan/{id}/signoff: the Owner signs a step off (the
// Owner 2026-10-21), the owner_signoff event its done rule's owner_signoff
// atom waits for; the controller's next rules pass moves the step. The admin
// role only (the "signoff" ability): the tokens agents hold (agentd, reporter,
// operator, viewer) are refused before this runs, and no MCP tool calls it.
// The body may name the ref; it must when the
// rule waits on more than one. A second press answers the first sign-off
// (already_signed) and writes nothing.
func (a *API) signOffStep(w http.ResponseWriter, r *http.Request) {
	var b signoffBody
	if !decode(w, r, &b) {
		return
	}
	who, err := writer(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	out, err := store.SignOffStep(r.Context(), a.DB, who.Actor, chi.URLParam(r, "id"), b.Ref)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

type planImportBody struct {
	IdempotencyKey string          `json:"idempotency_key"`
	Reason         string          `json:"reason"`
	Request        json.RawMessage `json:"request"`
}

// proposePlanImport is POST /plan/imports: a plan's first import
// (CUT-plan-import-2026-10-19.md §4 A), or a recorder log's
// (agentrt.recorder_import/v1, CUT-gate-b-2026-10-19.md §2 item 3 A), by the
// request's format; tried and rolled back, kept, and put to the Owner on one
// decision card; the controller lands it once he answers Import.
func (a *API) proposePlanImport(w http.ResponseWriter, r *http.Request) {
	var b planImportBody
	if !decode(w, r, &b) {
		return
	}
	// The request is read whole before the writer, as when it was one type
	// (review 402 C2).
	var head struct {
		Format string `json:"format"`
	}
	_ = json.Unmarshal(b.Request, &head)
	var recorder recorderimport.Request
	var plan planimport.Request
	if head.Format == recorderimport.Format {
		if !strictly(w, b.Request, &recorder) {
			return
		}
	} else if !strictly(w, b.Request, &plan) {
		return
	}
	who, err := writer(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	var out map[string]any
	if head.Format == recorderimport.Format {
		out, err = store.ProposeRecorderImport(r.Context(), a.DB, who, b.IdempotencyKey, b.Reason, recorder, a.Ask)
	} else {
		out, err = store.ProposePlanImport(r.Context(), a.DB, who, b.IdempotencyKey, b.Reason, plan, a.Ask)
	}
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// strictly reads a request that came as raw JSON into v as decode reads a
// body, with decode's answer: a field v does not have is refused (400).
func strictly(w http.ResponseWriter, raw json.RawMessage, v any) bool {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "body: "+err.Error())
		return false
	}
	return true
}

// progress is GET /progress: the projection of
// SPEC-agentrt-progress-2026-10-14.md, computed on every read. A read that
// fails is 503, never an old copy (review 181 B1).
func (a *API) progress(w http.ResponseWriter, r *http.Request) {
	out, err := store.ProgressSince(r.Context(), a.DB, a.back())
	if err != nil {
		// Unknown, never an old copy or half of one (SPEC §1).
		a.logf("agentrt progress: %v", err)
		writeError(w, http.StatusServiceUnavailable, "unavailable", "the runtime's state cannot be read now")
		return
	}
	writeJSON(w, http.StatusOK, out)
}
