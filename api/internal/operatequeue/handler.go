package operatequeue

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/remediation"
)

type Handler struct {
	store       *Store
	briefs      *BriefStore
	drain       *DrainWorker
	audit       *actuation.AuditLog
	jobs        *remediation.JobStore
	observer    LifecycleObserver
	evidence    EvidenceSource
	remediation RemediationStarter

	mu               sync.Mutex
	lastSweepSummary string
}

type LifecycleObserver interface {
	OnOperateQueueExecution(item Item)
	OnOperateQueueClosed(item Item)
}

func (h *Handler) BindRemediationJobs(jobs *remediation.JobStore) {
	h.jobs = jobs
}

func (h *Handler) BindRemediationStarter(starter RemediationStarter) {
	h.remediation = starter
}

func (h *Handler) BindLifecycleObserver(observer LifecycleObserver) {
	h.observer = observer
}

func (h *Handler) BindEvidenceSource(src EvidenceSource) {
	h.evidence = src
}

func NewHandler(configDir string, audit *actuation.AuditLog) *Handler {
	h := &Handler{
		store:  NewStore(configDir),
		briefs: NewBriefStore(configDir),
		audit:  audit,
	}
	h.drain = NewDrainWorker(h)
	return h
}

func (h *Handler) Store() *Store {
	return h.store
}

func (h *Handler) Briefs() *BriefStore {
	return h.briefs
}

func (h *Handler) HandleGetQueue(w http.ResponseWriter, _ *http.Request) {
	list, err := h.store.List()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (h *Handler) HandleEnqueue(w http.ResponseWriter, r *http.Request) {
	var req EnqueueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	item, err := NewItemFromManual(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	saved, err := h.store.Add(item)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	h.audit.Record(r, "operate.queue.enqueue", saved.ID, StatusOpen,
		fmt.Sprintf("program=%s source=%s", saved.ProgramID, saved.Source))
	writeJSON(w, http.StatusCreated, saved)
}

// FindOpenChecklistHandoff returns the open checklist_dispatch handoff for a
// checklist item, when one exists.
func (h *Handler) FindOpenChecklistHandoff(checklistItemID string) (Item, bool) {
	want := strings.TrimSpace(checklistItemID)
	if want == "" {
		return Item{}, false
	}
	list, err := h.store.List()
	if err != nil {
		return Item{}, false
	}
	for _, item := range list.Open {
		if item.Source != SourceChecklistDispatch {
			continue
		}
		if ExtractChecklistItemID(item) == want {
			return item, true
		}
	}
	return Item{}, false
}

// EnqueueChecklistDispatch adds a semi_auto checklist handoff (source=checklist_dispatch).
// The bool reports whether a new item was created.
//
// One condition, one handoff. The dispatcher's own 24h window lives in a
// snapshot it overwrites on every run, so a failure that stayed open used to
// enqueue a fresh handoff every other run. The open queue is the durable state,
// so dedupe against it: an item already carrying this checklist item is
// returned as-is.
func (h *Handler) EnqueueChecklistDispatch(req EnqueueRequest) (Item, bool, error) {
	item, err := NewItemFromManual(req)
	if err != nil {
		return Item{}, false, err
	}
	item.Source = SourceChecklistDispatch
	if strings.TrimSpace(item.Reason) == "" {
		item.Reason = "checklist_dispatch"
	}
	if checklistID := ExtractChecklistItemID(item); checklistID != "" {
		if existing, ok := h.FindOpenChecklistHandoff(checklistID); ok {
			return existing, false, nil
		}
	}
	saved, err := h.store.Add(item)
	if err != nil {
		return Item{}, false, err
	}
	return saved, true, nil
}

// RetireRecoveredChecklistHandoffs dismisses open checklist_dispatch handoffs
// whose checklist item is ok again, and returns the ones it closed.
//
// An item whose remediation job is still running is left alone — the sweep
// closes those once the job reports, with its post-fix evidence.
func (h *Handler) RetireRecoveredChecklistHandoffs(checklistItemID, detail string) []Item {
	want := strings.TrimSpace(checklistItemID)
	if want == "" {
		return nil
	}
	list, err := h.store.List()
	if err != nil {
		return nil
	}
	var out []Item
	for _, item := range list.Open {
		if item.Source != SourceChecklistDispatch || ExtractChecklistItemID(item) != want {
			continue
		}
		if jobID := strings.TrimSpace(item.ExecutionJobID); jobID != "" && h.jobs != nil {
			if job, ok := h.jobs.Get(jobID); ok && job.Status == remediation.JobRunning {
				continue
			}
		}
		evidence := []string{
			"dismiss:resolved",
			"checklist: " + want + " signal=ok — condition cleared, handoff retired automatically",
		}
		if strings.TrimSpace(detail) != "" {
			evidence = append(evidence, "signal: "+detail)
		}
		closed, err := h.store.Dismiss(item.ID, DismissRequest{
			CompletionEvidence: evidence,
			Reason:             "resolved",
		})
		if err != nil {
			continue
		}
		out = append(out, closed)
	}
	return out
}

func (h *Handler) HandleClose(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"})
		return
	}
	var req CloseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	jobDone := false
	if item, ok := h.store.FindByID(id); ok && item.ExecutionJobID != "" && h.jobs != nil {
		if job, found := h.jobs.Get(item.ExecutionJobID); found {
			jobDone = job.Status == remediation.JobDone
		}
	}
	closed, err := h.store.Close(id, req, jobDone)
	if err != nil {
		msg := err.Error()
		switch msg {
		case "item not found":
			writeJSON(w, http.StatusNotFound, map[string]string{"error": msg})
		default:
			writeJSON(w, http.StatusConflict, map[string]string{"error": msg})
		}
		return
	}
	h.audit.Record(r, "operate.queue.close", closed.ID, StatusClosed,
		fmt.Sprintf("program=%s", closed.ProgramID))
	if h.observer != nil {
		h.observer.OnOperateQueueClosed(closed)
	}
	writeJSON(w, http.StatusOK, closed)
}

func (h *Handler) HandleDismiss(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"})
		return
	}
	var req DismissRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	closed, err := h.store.Dismiss(id, req)
	if err != nil {
		msg := err.Error()
		switch msg {
		case "item not found":
			writeJSON(w, http.StatusNotFound, map[string]string{"error": msg})
		default:
			writeJSON(w, http.StatusConflict, map[string]string{"error": msg})
		}
		return
	}
	h.audit.Record(r, "operate.queue.dismiss", closed.ID, StatusClosed,
		fmt.Sprintf("program=%s", closed.ProgramID))
	if h.observer != nil {
		h.observer.OnOperateQueueClosed(closed)
	}
	writeJSON(w, http.StatusOK, closed)
}

func (h *Handler) HandleRecordExecution(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	var req ExecutionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if h.jobs == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "remediation job store unavailable"})
		return
	}
	if _, ok := h.jobs.Get(strings.TrimSpace(req.ExecutionJobID)); !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "execution job not found"})
		return
	}
	item, err := h.store.RecordExecution(id, req.ExecutionJobID)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	h.audit.Record(r, "operate.queue.execution", item.ID, item.Status, "job="+item.ExecutionJobID)
	if h.observer != nil {
		h.observer.OnOperateQueueExecution(item)
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handler) HandleSweep(w http.ResponseWriter, r *http.Request) {
	var req SweepRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
	}
	// Query override: ?auto_drain=true
	if strings.EqualFold(r.URL.Query().Get("auto_drain"), "true") {
		req.AutoDrain = true
	}
	resp, err := h.Sweep(req)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	h.audit.Record(r, "operate.queue.sweep", "queue", "ok",
		fmt.Sprintf("auto_drain=%v dismissed=%d queued=%d decisions=%d",
			req.AutoDrain, len(resp.Dismissed), len(resp.Queued), len(resp.Decisions)))
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleListBriefs(w http.ResponseWriter, _ *http.Request) {
	if h.briefs == nil {
		writeJSON(w, http.StatusOK, map[string]any{"briefs": []DecisionBrief{}})
		return
	}
	briefs, err := h.briefs.ListPending(time.Now().UTC())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"briefs": briefs})
}

func (h *Handler) HandleDecideBrief(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"})
		return
	}
	var req DecideRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if h.briefs == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "brief store unavailable"})
		return
	}
	brief, err := h.briefs.ApplyDecision(id, req.Decision)
	if err != nil {
		msg := err.Error()
		switch {
		case msg == "brief not found":
			writeJSON(w, http.StatusNotFound, map[string]string{"error": msg})
		case strings.Contains(msg, "decision must"):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
		default:
			writeJSON(w, http.StatusConflict, map[string]string{"error": msg})
		}
		return
	}

	switch brief.Decision {
	case DecisionDismissed:
		closed, derr := h.store.Dismiss(brief.ItemID, DismissRequest{
			CompletionEvidence: []string{
				"operator: decision brief dismissed",
				"brief:" + brief.ID,
			},
			Reason: "resolved",
		})
		if derr != nil {
			writeJSON(w, http.StatusConflict, map[string]string{
				"error": "brief decided but dismiss failed: " + derr.Error(),
				"brief": brief.ID,
			})
			return
		}
		if h.observer != nil {
			h.observer.OnOperateQueueClosed(closed)
		}
		h.audit.Record(r, "operate.brief.decide", brief.ID, DecisionDismissed, "item="+brief.ItemID)
		writeJSON(w, http.StatusOK, map[string]any{"brief": brief, "item": closed})
		return

	case DecisionApprovedRun:
		item, ok := h.store.FindByID(brief.ItemID)
		if !ok || item.Status != StatusOpen {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "queue item not open for approved_run"})
			return
		}
		h.EnqueueForDrain(*item)
		h.audit.Record(r, "operate.brief.decide", brief.ID, DecisionApprovedRun, "item="+brief.ItemID)
		writeJSON(w, http.StatusOK, map[string]any{"brief": brief, "drain": h.drain.Status()})
		return

	case DecisionHold:
		h.audit.Record(r, "operate.brief.decide", brief.ID, DecisionHold, "until="+brief.HoldUntil)
		writeJSON(w, http.StatusOK, map[string]any{"brief": brief})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"brief": brief})
}

func (h *Handler) HandleDrainStatus(w http.ResponseWriter, _ *http.Request) {
	if h.drain == nil {
		writeJSON(w, http.StatusOK, DrainStatus{})
		return
	}
	writeJSON(w, http.StatusOK, h.drain.Status())
}

func (h *Handler) InjectFromApproval(r *http.Request, params ApprovalInjectParams) (Item, error) {
	item := NewItemFromApproval(params)
	saved, err := h.store.Add(item)
	if err != nil {
		return Item{}, err
	}
	detail := fmt.Sprintf("program=%s pending=%s", saved.ProgramID, saved.PendingID)
	if strings.TrimSpace(saved.Lane) != "" {
		detail += " lane=" + saved.Lane
	}
	h.audit.Record(r, "operate.queue.enqueue", saved.ID, StatusOpen, detail)
	return saved, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
