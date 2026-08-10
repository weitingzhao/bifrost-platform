package patrol

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const defaultTick = 30 * time.Second

// Handler serves /api/v1/patrol/* and runs the embedded cron scanner.
type Handler struct {
	mu         sync.Mutex
	skills     []PatrolSkill
	byID       map[string]PatrolSkill
	store      *Store
	dispatch   dispatcher
	writeTools map[string]struct{}
	now        func() time.Time
	tick       time.Duration
	nextDue    map[string]time.Time
	inflight   map[string]struct{}
	syncScan   bool // tests: scanDue runs execute inline
	stop       context.CancelFunc
}

type HandlerOptions struct {
	SkillsDir  string
	StateDir   string
	Dispatcher dispatcher
	Now        func() time.Time
	Tick       time.Duration
	SyncScan   bool
}

func NewHandler(configDir string) (*Handler, error) {
	return NewHandlerWithOptions(HandlerOptions{
		SkillsDir: DefaultSkillsDir(configDir),
	})
}

func NewHandlerWithOptions(opts HandlerOptions) (*Handler, error) {
	skills, err := LoadDir(opts.SkillsDir)
	if err != nil {
		return nil, err
	}
	store, err := NewStore(opts.StateDir)
	if err != nil {
		return nil, err
	}
	nowFn := opts.Now
	if nowFn == nil {
		nowFn = func() time.Time { return time.Now().UTC() }
	}
	tick := opts.Tick
	if tick <= 0 {
		tick = defaultTick
	}
	disp := opts.Dispatcher
	if disp == nil {
		disp = resolveDispatcher()
	}
	h := &Handler{
		skills:     skills,
		byID:       map[string]PatrolSkill{},
		store:      store,
		dispatch:   disp,
		writeTools: writeToolSet(),
		now:        nowFn,
		tick:       tick,
		nextDue:    map[string]time.Time{},
		inflight:   map[string]struct{}{},
		syncScan:   opts.SyncScan,
	}
	for _, s := range skills {
		h.byID[s.ID] = s
		if next, err := NextAfter(s.Schedule, nowFn()); err == nil {
			h.nextDue[s.ID] = next
		}
	}
	return h, nil
}

// Start launches the 30s cron scanner. Cancel ctx (or Stop) to halt.
func (h *Handler) Start(ctx context.Context) {
	if h == nil {
		return
	}
	h.mu.Lock()
	if h.stop != nil {
		h.mu.Unlock()
		return
	}
	loopCtx, cancel := context.WithCancel(ctx)
	h.stop = cancel
	h.mu.Unlock()
	go h.loop(loopCtx)
}

func (h *Handler) Stop() {
	if h == nil {
		return
	}
	h.mu.Lock()
	cancel := h.stop
	h.stop = nil
	h.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (h *Handler) loop(ctx context.Context) {
	ticker := time.NewTicker(h.tick)
	defer ticker.Stop()
	h.scanDue()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.scanDue()
		}
	}
}

func (h *Handler) scanDue() {
	now := h.now().UTC()
	h.mu.Lock()
	ids := make([]string, 0, len(h.skills))
	for _, s := range h.skills {
		if !h.store.Enabled(s.ID, s.Enabled) {
			continue
		}
		due, ok := h.nextDue[s.ID]
		if !ok || due.IsZero() {
			if next, err := NextAfter(s.Schedule, now); err == nil {
				h.nextDue[s.ID] = next
			}
			continue
		}
		if !now.Before(due) {
			ids = append(ids, s.ID)
			if next, err := NextAfter(s.Schedule, now); err == nil {
				h.nextDue[s.ID] = next
			}
		}
	}
	syncScan := h.syncScan
	h.mu.Unlock()

	for _, id := range ids {
		if syncScan {
			_, _ = h.execute(context.Background(), id, TriggerCron)
			continue
		}
		go func(skillID string) {
			_, _ = h.execute(context.Background(), skillID, TriggerCron)
		}(id)
	}
}

func (h *Handler) HandleListSkills(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, SkillsResponse{Skills: h.listSkills()})
}

func (h *Handler) HandleGetSkill(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	skill, ok := h.publicSkill(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown patrol skill"})
		return
	}
	writeJSON(w, http.StatusOK, skill)
}

func (h *Handler) HandleListRuns(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err == nil && n > 0 {
			limit = n
		}
	}
	runs, total := h.store.ListRuns(limit)
	if runs == nil {
		runs = []PatrolRun{}
	}
	writeJSON(w, http.StatusOK, RunsResponse{Runs: runs, Total: total})
}

func (h *Handler) HandleEnable(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, ok := h.byID[id]; !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown patrol skill"})
		return
	}
	var body EnableRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body: expected {\"enabled\": bool}"})
		return
	}
	if err := h.store.SetEnabled(id, body.Enabled); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	skill, _ := h.publicSkill(id)
	writeJSON(w, http.StatusOK, skill)
}

func (h *Handler) HandleTrigger(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if h.syncScan {
		resp, code := h.execute(r.Context(), id, TriggerManual)
		writeJSON(w, code, resp)
		return
	}
	resp, code := h.enqueue(id, TriggerManual)
	writeJSON(w, code, resp)
}

// HandleWebhook handles POST /patrol/webhook/{event}. External systems (CI/CD,
// Argo CD hooks, delivery pipelines) call this to trigger patrol skills. The
// event path param selects predefined skill mappings; a custom skill_id in the
// body overrides the default.
func (h *Handler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	event := chi.URLParam(r, "event")
	var body WebhookTriggerRequest
	if r.Body != nil && r.ContentLength != 0 {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	skillID := body.SkillID
	if skillID == "" {
		skillID = webhookEventSkill(event)
	}
	if skillID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "unknown webhook event: " + event,
		})
		return
	}
	slog.Info("patrol webhook", "event", event, "skill_id", skillID, "reason", body.Reason)
	if h.syncScan {
		resp, code := h.execute(r.Context(), skillID, TriggerWebhook)
		writeJSON(w, code, resp)
		return
	}
	resp, code := h.enqueue(skillID, TriggerWebhook)
	writeJSON(w, code, resp)
}

// TriggerInternal programmatically triggers a patrol skill from within the API
// process (e.g. after a delivery pipeline completes). Runs asynchronously.
func (h *Handler) TriggerInternal(skillID string) {
	if h == nil {
		return
	}
	slog.Info("patrol internal trigger", "skill_id", skillID)
	go func() {
		_, _ = h.execute(context.Background(), skillID, TriggerWebhook)
	}()
}

// webhookEventSkill maps well-known webhook event names to patrol skill IDs.
func webhookEventSkill(event string) string {
	switch strings.ToLower(strings.TrimSpace(event)) {
	case "delivery-complete", "pipeline-complete":
		return "ops-autopilot"
	default:
		return ""
	}
}

func (h *Handler) lookupReady(id string, trigger Trigger) (PatrolSkill, TriggerResponse, int, bool) {
	h.mu.Lock()
	skill, ok := h.byID[id]
	h.mu.Unlock()
	if !ok {
		return PatrolSkill{}, TriggerResponse{Error: "unknown patrol skill"}, http.StatusNotFound, false
	}
	if !h.store.Enabled(id, skill.Enabled) {
		run := h.record(skill, trigger, ResultSkipped, "", "skill disabled")
		return skill, TriggerResponse{RunID: run.ID, Status: StatusBlocked, Result: ResultSkipped, Error: "skill disabled"}, http.StatusOK, false
	}
	gate := EvaluateTrust(skill, trigger, h.writeTools)
	if !gate.Allow {
		run := h.record(skill, trigger, gate.Result, "", gate.Reason)
		return skill, TriggerResponse{RunID: run.ID, Status: StatusBlocked, Result: gate.Result, Error: gate.Reason}, http.StatusOK, false
	}
	if !h.begin(id) {
		run := h.record(skill, trigger, ResultSkipped, "", "skill already running")
		return skill, TriggerResponse{RunID: run.ID, Status: StatusBlocked, Result: ResultSkipped, Error: "skill already running"}, http.StatusConflict, false
	}
	return skill, TriggerResponse{}, 0, true
}

func (h *Handler) execute(ctx context.Context, id string, trigger Trigger) (TriggerResponse, int) {
	skill, early, code, ok := h.lookupReady(id, trigger)
	if !ok {
		return early, code
	}
	defer h.end(id)

	started := h.now().UTC()
	run := h.startRecord(skill, trigger, started)
	prompt := buildPrompt(skill, trigger, started)
	outcome := h.dispatch.Dispatch(ctx, skill, trigger, prompt, h.progressWriter(run.ID))
	if outcome.Status == "" {
		outcome.Status = StatusCompleted
	}
	run = h.completeRecord(run.ID, started, outcome)
	return TriggerResponse{
		RunID:  run.ID,
		Status: outcome.Status,
		Result: outcome.Result,
		Error:  outcome.Error,
	}, http.StatusOK
}

// enqueue starts dispatch in the background so POST /trigger returns immediately with result=running.
func (h *Handler) enqueue(id string, trigger Trigger) (TriggerResponse, int) {
	skill, early, code, ok := h.lookupReady(id, trigger)
	if !ok {
		return early, code
	}
	started := h.now().UTC()
	run := h.startRecord(skill, trigger, started)
	go func() {
		defer h.end(id)
		prompt := buildPrompt(skill, trigger, started)
		outcome := h.dispatch.Dispatch(context.Background(), skill, trigger, prompt, h.progressWriter(run.ID))
		if outcome.Status == "" {
			outcome.Status = StatusCompleted
		}
		h.completeRecord(run.ID, started, outcome)
	}()
	return TriggerResponse{
		RunID:  run.ID,
		Status: StatusStarted,
		Result: ResultRunning,
	}, http.StatusAccepted
}

func (h *Handler) begin(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.inflight[id]; ok {
		return false
	}
	h.inflight[id] = struct{}{}
	return true
}

func (h *Handler) end(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.inflight, id)
}

func (h *Handler) record(skill PatrolSkill, trigger Trigger, result RunResult, evidence, errMsg string) PatrolRun {
	now := h.now().UTC()
	run := PatrolRun{
		ID:         "pat-" + uuid.NewString(),
		SkillID:    skill.ID,
		SkillName:  skill.Name,
		Trigger:    trigger,
		StartedAt:  now.Format(time.RFC3339),
		FinishedAt: now.Format(time.RFC3339),
		DurationMS: 0,
		Result:     result,
		Evidence:   evidence,
		Error:      errMsg,
	}
	if err := h.store.AppendRun(run); err != nil {
		slog.Warn("patrol persist run", "err", err)
	}
	return run
}

func (h *Handler) progressWriter(runID string) progressFn {
	return func(evidence string) {
		_ = h.store.UpdateRun(runID, func(run *PatrolRun) {
			run.Evidence = evidence
		})
	}
}

func (h *Handler) startRecord(skill PatrolSkill, trigger Trigger, started time.Time) PatrolRun {
	run := PatrolRun{
		ID:        "pat-" + uuid.NewString(),
		SkillID:   skill.ID,
		SkillName: skill.Name,
		Trigger:   trigger,
		StartedAt: started.Format(time.RFC3339),
		Result:    ResultRunning,
		Evidence:  "starting…",
	}
	if err := h.store.AppendRun(run); err != nil {
		slog.Warn("patrol persist run start", "err", err)
	}
	return run
}

func (h *Handler) completeRecord(runID string, started time.Time, out dispatchOutcome) PatrolRun {
	finished := h.now().UTC()
	var updated PatrolRun
	err := h.store.UpdateRun(runID, func(run *PatrolRun) {
		run.FinishedAt = finished.Format(time.RFC3339)
		run.DurationMS = finished.Sub(started).Milliseconds()
		run.Result = out.Result
		run.Evidence = out.Evidence
		run.Error = out.Error
		updated = *run
	})
	if err != nil {
		slog.Warn("patrol persist run finish", "err", err, "run_id", runID)
		return PatrolRun{ID: runID, Result: out.Result, Evidence: out.Evidence, Error: out.Error}
	}
	return updated
}

func (h *Handler) listSkills() []PatrolSkill {
	now := h.now().UTC()
	out := make([]PatrolSkill, 0, len(h.skills))
	for _, s := range h.skills {
		out = append(out, h.decorate(s, now))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (h *Handler) publicSkill(id string) (PatrolSkill, bool) {
	h.mu.Lock()
	s, ok := h.byID[id]
	h.mu.Unlock()
	if !ok {
		return PatrolSkill{}, false
	}
	return h.decorate(s, h.now().UTC()), true
}

func (h *Handler) decorate(s PatrolSkill, now time.Time) PatrolSkill {
	s.Enabled = h.store.Enabled(s.ID, s.Enabled)
	if last := h.store.LastRun(s.ID); last != nil {
		s.LastRunAt = last.StartedAt
		s.LastResult = last.Result
	}
	h.mu.Lock()
	_, flying := h.inflight[s.ID]
	due := h.nextDue[s.ID]
	h.mu.Unlock()
	if flying {
		s.LastResult = ResultRunning
	}
	if due.IsZero() {
		if next, err := NextAfter(s.Schedule, now); err == nil {
			due = next
		}
	}
	if !due.IsZero() && s.Enabled {
		s.NextRunAt = due.UTC().Format(time.RFC3339)
	}
	return s
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
