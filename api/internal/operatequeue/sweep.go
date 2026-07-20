package operatequeue

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/remediation"
)

// EvidenceSource supplies cached checklist/fleet probe rows (no HTTP storm).
type EvidenceSource interface {
	LoadEvidence() (EvidenceBundle, error)
}

// Sweep runs deterministic triage over open queue items.
func (h *Handler) Sweep(req SweepRequest) (SweepResponse, error) {
	list, err := h.store.List()
	if err != nil {
		return SweepResponse{}, err
	}
	ev := EvidenceBundle{Now: time.Now().UTC(), QuietNominal: true}
	if h.evidence != nil {
		loaded, err := h.evidence.LoadEvidence()
		if err == nil {
			ev = loaded
			if ev.Now.IsZero() {
				ev.Now = time.Now().UTC()
			}
		}
	}

	jobRunning := func(jobID string) bool {
		if h.jobs == nil {
			return false
		}
		job, ok := h.jobs.Get(jobID)
		return ok && job.Status == remediation.JobRunning
	}

	resp := SweepResponse{
		Dismissed:  []SweepResult{},
		Queued:     []SweepResult{},
		Decisions:  []DecisionBrief{},
		InProgress: []SweepResult{},
		Held:       []SweepResult{},
	}

	type stillNeeded struct {
		item Item
		cr   ClassifyResult
		sr   SweepResult
	}
	var drainCandidates []stillNeeded

	for _, item := range list.Open {
		heldUntil := ""
		if h.briefs != nil {
			heldUntil = h.briefs.HoldUntilForItem(item.ID, ev.Now)
		}
		cr := ClassifyItem(ClassifyInput{
			Item:       item,
			Evidence:   ev,
			JobRunning: jobRunning,
			HeldUntil:  heldUntil,
		})
		sr := SweepResult{
			ItemID:  item.ID,
			Title:   item.Title,
			Verdict: cr.Verdict,
			Reason:  cr.Reason,
		}

		switch cr.Verdict {
		case VerdictHeld:
			resp.Held = append(resp.Held, sr)
		case VerdictInProgress:
			resp.InProgress = append(resp.InProgress, sr)
		case VerdictStale:
			closed, err := h.store.Dismiss(item.ID, DismissRequest{
				CompletionEvidence: []string{
					"operator: queue drain sweep auto-dismiss",
					"sweep: " + cr.Reason,
					fmt.Sprintf("fleet_signal:%s", cr.FleetSignal),
				},
				Reason: "resolved",
			})
			if err != nil {
				// fall back to decision if dismiss fails
				brief, berr := h.ensureBrief(item, cr, ev.Now)
				if berr == nil {
					resp.Decisions = append(resp.Decisions, brief)
				}
				sr.Verdict = VerdictNeedsDecision
				sr.Reason = "stale dismiss failed: " + err.Error()
				continue
			}
			if h.observer != nil {
				h.observer.OnOperateQueueClosed(closed)
			}
			if h.audit != nil {
				h.audit.RecordDirect("queue-sweep", actuation.RoleOperator, "operate.queue.dismiss", closed.ID, StatusClosed,
					"sweep resolved: "+cr.Reason)
			}
			resp.Dismissed = append(resp.Dismissed, sr)
		case VerdictStillNeeded:
			resp.Queued = append(resp.Queued, sr)
			drainCandidates = append(drainCandidates, stillNeeded{item: item, cr: cr, sr: sr})
		default:
			brief, err := h.ensureBrief(item, cr, ev.Now)
			if err == nil {
				resp.Decisions = append(resp.Decisions, brief)
			}
		}
	}

	// Priority: fail > degraded, then older first
	sort.SliceStable(drainCandidates, func(i, j int) bool {
		ri := scopeSeverity(drainCandidates[i].cr)
		rj := scopeSeverity(drainCandidates[j].cr)
		if ri != rj {
			return ri > rj
		}
		return drainCandidates[i].item.CreatedAt < drainCandidates[j].item.CreatedAt
	})
	resp.Queued = make([]SweepResult, 0, len(drainCandidates))
	ordered := make([]Item, 0, len(drainCandidates))
	for _, c := range drainCandidates {
		resp.Queued = append(resp.Queued, c.sr)
		ordered = append(ordered, c.item)
	}

	if req.AutoDrain && len(ordered) > 0 && h.drain != nil {
		h.drain.Enqueue(ordered)
		_ = h.drain.Kick()
	}

	resp.NextSweepAt = ev.Now.Add(30 * time.Minute).Format(time.RFC3339)
	h.setLastSweepSummary(fmt.Sprintf("dismissed=%d queued=%d decisions=%d in_progress=%d held=%d",
		len(resp.Dismissed), len(resp.Queued), len(resp.Decisions), len(resp.InProgress), len(resp.Held)))
	return resp, nil
}

func (h *Handler) ensureBrief(item Item, cr ClassifyResult, now time.Time) (DecisionBrief, error) {
	if h.briefs == nil {
		return BuildDecisionBrief(item, cr, now), nil
	}
	brief := BuildDecisionBrief(item, cr, now)
	return h.briefs.UpsertPending(brief)
}

func scopeSeverity(cr ClassifyResult) int {
	d := strings.ToLower(cr.FleetDetail + " " + cr.Reason)
	if strings.Contains(d, "=fail") || strings.Contains(d, " fail") {
		return 3
	}
	if strings.Contains(d, "degraded") {
		return 2
	}
	return 1
}

func (h *Handler) setLastSweepSummary(s string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastSweepSummary = s
}

func (h *Handler) LastSweepSummary() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.lastSweepSummary
}
