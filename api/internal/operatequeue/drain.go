package operatequeue

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/remediation"
)

const (
	drainPollInterval = 3 * time.Second
	drainJobTimeout   = 45 * time.Minute
)

// RemediationStarter starts runner jobs (implemented by remediation.Handler).
type RemediationStarter interface {
	StartInternal(ctx context.Context, req remediation.StartRunnerRequest) (*remediation.Job, error)
}

// DrainWorker serially executes STILL_NEEDED items (max 1 concurrent).
type DrainWorker struct {
	h *Handler

	mu              sync.Mutex
	queue           []Item
	active          bool
	currentItemID   string
	currentJobID    string
	currentTitle    string
	lastError       string
	lastCompletedAt string
	running         bool
}

func NewDrainWorker(h *Handler) *DrainWorker {
	return &DrainWorker{h: h}
}

func (d *DrainWorker) Enqueue(items []Item) {
	d.mu.Lock()
	defer d.mu.Unlock()
	seen := map[string]bool{}
	for _, existing := range d.queue {
		seen[existing.ID] = true
	}
	if d.currentItemID != "" {
		seen[d.currentItemID] = true
	}
	for _, item := range items {
		if seen[item.ID] {
			continue
		}
		d.queue = append(d.queue, item)
		seen[item.ID] = true
	}
}

func (d *DrainWorker) Status() DrainStatus {
	d.mu.Lock()
	defer d.mu.Unlock()
	summary := ""
	if d.h != nil {
		summary = d.h.LastSweepSummary()
	}
	ids := make([]string, 0, len(d.queue))
	for _, item := range d.queue {
		ids = append(ids, item.ID)
	}
	paused := false
	pauseReason := ""
	if !d.running && len(d.queue) > 0 && d.h != nil && d.h.hasOtherRunningJobs("") {
		paused = true
		pauseReason = "another remediation job is running"
	} else if strings.HasPrefix(d.lastError, "drain paused:") {
		paused = true
		pauseReason = d.lastError
	}
	return DrainStatus{
		Active:           d.active || d.running,
		CurrentItemID:    d.currentItemID,
		CurrentJobID:     d.currentJobID,
		CurrentTitle:     d.currentTitle,
		QueuedCount:      len(d.queue),
		QueuedItemIDs:    ids,
		Paused:           paused,
		PauseReason:      pauseReason,
		LastError:        d.lastError,
		LastCompletedAt:  d.lastCompletedAt,
		LastSweepSummary: summary,
	}
}

// Kick starts the background drain loop if idle.
func (d *DrainWorker) Kick() bool {
	d.mu.Lock()
	if d.running {
		d.mu.Unlock()
		return false
	}
	if len(d.queue) == 0 {
		d.mu.Unlock()
		return false
	}
	if d.h != nil && d.h.hasOtherRunningJobs("") {
		d.lastError = "drain paused: another remediation job is running"
		d.mu.Unlock()
		return false
	}
	d.running = true
	d.mu.Unlock()
	go d.loop()
	return true
}

func (d *DrainWorker) loop() {
	defer func() {
		d.mu.Lock()
		d.running = false
		d.active = false
		d.currentItemID = ""
		d.currentJobID = ""
		d.currentTitle = ""
		d.mu.Unlock()
	}()

	for {
		item, ok := d.pop()
		if !ok {
			return
		}
		if err := d.runOne(item); err != nil {
			d.mu.Lock()
			d.lastError = err.Error()
			d.mu.Unlock()
		}
		// Post-run re-sweep (triage only — do not auto-chain unbounded drains)
		if d.h != nil {
			_, _ = d.h.Sweep(SweepRequest{AutoDrain: false})
		}
	}
}

func (d *DrainWorker) pop() (Item, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.queue) == 0 {
		return Item{}, false
	}
	if d.h != nil && d.h.hasOtherRunningJobs("") {
		d.lastError = "drain paused: another remediation job is running"
		return Item{}, false
	}
	item := d.queue[0]
	d.queue = d.queue[1:]
	d.active = true
	d.currentItemID = item.ID
	d.currentTitle = item.Title
	d.currentJobID = ""
	return item, true
}

func (d *DrainWorker) runOne(item Item) error {
	if d.h == nil || d.h.remediation == nil {
		return d.demote(item, "remediation starter unavailable")
	}

	checklistID := ExtractChecklistItemID(item)
	scope := ResolveFixScope(item, checklistID)
	if scope == "" {
		return d.demote(item, "cannot drain: empty fix_scope")
	}
	if isLiveTradingScope(scope) || item.HandoffKind == HandoffRecurringSetup {
		return d.demote(item, "cannot drain: D10 / recurring_setup")
	}

	prompt := fmt.Sprintf(
		"Queue Drain Workflow — serial remediation for operate queue item `%s`.\nTitle: %s\nScope: %s\n\nDiagnose, remediate safely, then verify_mission_snapshot. D10: no live trading.\n",
		item.ID, item.Title, scope,
	)
	ctx, cancel := context.WithTimeout(context.Background(), drainJobTimeout)
	defer cancel()

	job, err := d.h.remediation.StartInternal(ctx, remediation.StartRunnerRequest{
		Scope:  scope,
		Actor:  "queue-drain",
		Prompt: prompt,
	})
	if err != nil {
		return d.demote(item, "start failed: "+err.Error())
	}

	d.mu.Lock()
	d.currentJobID = job.ID
	d.mu.Unlock()

	if _, err := d.h.store.RecordExecution(item.ID, job.ID); err != nil {
		return d.demote(item, "record execution failed: "+err.Error())
	}
	if d.h.observer != nil {
		if updated, ok := d.h.store.FindByID(item.ID); ok {
			d.h.observer.OnOperateQueueExecution(*updated)
		}
	}

	final, err := d.waitJob(ctx, job.ID)
	if err != nil {
		return d.demote(item, err.Error())
	}

	passed := jobPostFixPassed(final)
	if final.Status == remediation.JobDone && passed {
		closed, cerr := d.h.store.Close(item.ID, CloseRequest{
			CompletionEvidence: []string{
				"job:" + final.ID,
				"post_fix_verification:passed",
				"operator: queue drain serial worker",
			},
			PostFixVerificationPassed: true,
		}, true)
		if cerr != nil {
			return d.demote(item, "close failed: "+cerr.Error())
		}
		if d.h.observer != nil {
			d.h.observer.OnOperateQueueClosed(closed)
		}
		d.mu.Lock()
		d.lastCompletedAt = time.Now().UTC().Format(time.RFC3339)
		d.lastError = ""
		d.mu.Unlock()
		return nil
	}

	reason := fmt.Sprintf("job status=%s post_fix_passed=%v", final.Status, passed)
	if final.Error != "" {
		reason += " error=" + final.Error
	}
	return d.demote(item, reason)
}

func (d *DrainWorker) waitJob(ctx context.Context, jobID string) (*remediation.Job, error) {
	ticker := time.NewTicker(drainPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("drain wait timeout for job %s", jobID)
		case <-ticker.C:
			if d.h.jobs == nil {
				return nil, fmt.Errorf("job store unavailable")
			}
			job, ok := d.h.jobs.Get(jobID)
			if !ok {
				continue
			}
			switch job.Status {
			case remediation.JobDone, remediation.JobFailed, remediation.JobCancelled:
				return job, nil
			}
		}
	}
}

func (d *DrainWorker) demote(item Item, reason string) error {
	if d.h == nil {
		return fmt.Errorf("%s", reason)
	}
	cr := ClassifyResult{
		Verdict:     VerdictNeedsDecision,
		Reason:      "drain demoted: " + reason,
		FixScope:    ResolveFixScope(item, ExtractChecklistItemID(item)),
		FleetSignal: "unknown",
		FleetDetail: reason,
	}
	brief := BuildDecisionBrief(item, cr, time.Now().UTC())
	brief.Suggestion = SuggestionHold
	brief.SuggestionReason = reason
	brief.OpenQuestion = "Remediation did not close cleanly — approve a retry, dismiss, or hold?"
	brief.FullBrief = renderFullBrief(brief, item, defaultString(cr.FixScope, "none"))
	if d.h.briefs != nil {
		_, _ = d.h.briefs.UpsertPending(brief)
	}
	d.mu.Lock()
	d.lastError = reason
	d.mu.Unlock()
	return fmt.Errorf("%s", reason)
}

func jobPostFixPassed(job *remediation.Job) bool {
	if job == nil {
		return false
	}
	if strings.Contains(job.Summary, "Post-fix verification: PASSED") {
		return true
	}
	for _, ev := range job.Events {
		if ev.Meta == nil {
			continue
		}
		kind, _ := ev.Meta["kind"].(string)
		if kind != "post_fix_verification" {
			continue
		}
		if passed, ok := ev.Meta["passed"].(bool); ok && passed {
			return true
		}
	}
	return false
}

func (h *Handler) hasOtherRunningJobs(exceptJobID string) bool {
	if h.jobs == nil {
		return false
	}
	for _, j := range h.jobs.List() {
		if j.Status != remediation.JobRunning {
			continue
		}
		if exceptJobID != "" && j.ID == exceptJobID {
			continue
		}
		return true
	}
	return false
}

// OnRemediationTerminal is invoked when any remediation job reaches a terminal state.
// Triggers a triage-only re-sweep so cascade fixes clear stale queue items.
func (h *Handler) OnRemediationTerminal(job remediation.Job) {
	if job.Status != remediation.JobDone && job.Status != remediation.JobFailed && job.Status != remediation.JobCancelled {
		return
	}
	// If drain owns this job, its loop will re-sweep; avoid double storm.
	if h.drain != nil {
		st := h.drain.Status()
		if st.CurrentJobID == job.ID {
			return
		}
	}
	go func() {
		_, _ = h.Sweep(SweepRequest{AutoDrain: false})
	}()
}

// EnqueueForDrain exposes approve_run → drain path.
func (h *Handler) EnqueueForDrain(item Item) {
	if h.drain == nil {
		return
	}
	h.drain.Enqueue([]Item{item})
	_ = h.drain.Kick()
}
