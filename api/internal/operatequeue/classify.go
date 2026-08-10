package operatequeue

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentgovernance"
)

const staleAgeThreshold = 48 * time.Hour

// EvidenceSignal is a cached checklist / fleet probe row (no live HTTP).
type EvidenceSignal struct {
	ItemID string
	Signal string // ok | fail | degraded | unknown
	Detail string
}

// EvidenceBundle is the read-only snapshot used by triage.
type EvidenceBundle struct {
	Signals      []EvidenceSignal
	QuietNominal bool // true when no fail/degraded in cached checklist
	Now          time.Time
}

// JobRunningLookup reports whether a remediation job is still running.
type JobRunningLookup func(jobID string) bool

// ClassifyInput is the pure triage input for one open queue item.
type ClassifyInput struct {
	Item           Item
	Evidence       EvidenceBundle
	JobRunning     JobRunningLookup
	HeldUntil      string // RFC3339 hold expiry from pending brief; empty = not held
	HeldDecisionAt string
}

// ClassifyResult is the deterministic verdict for one item.
type ClassifyResult struct {
	Verdict          string
	Reason           string
	FixScope         string
	FleetSignal      string // GO | NO-GO | unknown
	FleetDetail      string
	FailingStandards string
	ChecklistItemID  string
}

var (
	reChecklistItemDesc = regexp.MustCompile(`(?i)handoff for item\s+` + "`?" + `([a-z0-9_-]+)` + "`?")
	reChecklistItemAC   = regexp.MustCompile(`(?i)Checklist item\s+([a-z0-9_-]+)\s+returns`)
)

// ResolveFixScope derives remediation scope from agent_task_id and/or checklist item.
func ResolveFixScope(item Item, checklistItemID string) string {
	if checklistItemID != "" {
		if scope := checklistFixScope(checklistItemID); scope != "" {
			return scope
		}
	}
	taskID := strings.TrimSpace(item.AgentTaskID)
	if taskID == "" {
		return ""
	}
	if task, ok := agentgovernance.TaskByID(taskID); ok {
		return strings.TrimSpace(task.Scope)
	}
	// agent_task_id often equals scope id
	return taskID
}

// ExtractChecklistItemID parses checklist_dispatch item identity from description/AC.
func ExtractChecklistItemID(item Item) string {
	if item.Source != SourceChecklistDispatch {
		return ""
	}
	if m := reChecklistItemDesc.FindStringSubmatch(item.Description); len(m) == 2 {
		return m[1]
	}
	for _, ac := range item.AcceptanceCriteria {
		if m := reChecklistItemAC.FindStringSubmatch(ac); len(m) == 2 {
			return m[1]
		}
	}
	return ""
}

// ClassifyItem applies deterministic Queue Drain triage rules (no LLM, no HTTP).
func ClassifyItem(in ClassifyInput) ClassifyResult {
	item := in.Item
	now := in.Evidence.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}

	checklistID := ExtractChecklistItemID(item)
	fixScope := ResolveFixScope(item, checklistID)
	fleetSig, fleetDetail, failing := fleetEvidenceForScope(fixScope, checklistID, in.Evidence)

	out := ClassifyResult{
		FixScope:         fixScope,
		FleetSignal:      fleetSig,
		FleetDetail:      fleetDetail,
		FailingStandards: failing,
		ChecklistItemID:  checklistID,
	}

	// Hold: skip reclassification until expiry
	if until := strings.TrimSpace(in.HeldUntil); until != "" {
		if t, err := time.Parse(time.RFC3339, until); err == nil && now.Before(t) {
			out.Verdict = VerdictHeld
			out.Reason = fmt.Sprintf("owner hold until %s", until)
			return out
		}
	}

	// IN_PROGRESS
	if jobID := strings.TrimSpace(item.ExecutionJobID); jobID != "" && in.JobRunning != nil && in.JobRunning(jobID) {
		out.Verdict = VerdictInProgress
		out.Reason = "execution job still running: " + jobID
		return out
	}

	// Hard NEEDS_DECISION gates (override STALE)
	if item.HandoffKind == HandoffRecurringSetup {
		out.Verdict = VerdictNeedsDecision
		out.Reason = "handoff_kind=recurring_setup requires owner verification"
		return out
	}
	if isLiveTradingScope(fixScope) || isLiveTradingChecklistItem(checklistID) {
		out.Verdict = VerdictNeedsDecision
		out.Reason = "D10: live-trading related scope always needs owner decision"
		return out
	}
	if strings.EqualFold(strings.TrimSpace(item.RiskLevel), RiskHigh) {
		out.Verdict = VerdictNeedsDecision
		out.Reason = "risk_level=high"
		return out
	}

	// STALE paths
	if fleetSig == "GO" && fixScope != "" {
		out.Verdict = VerdictStale
		out.Reason = fmt.Sprintf("fleet cell for scope %s is GO", fixScope)
		return out
	}
	if item.Source == SourceChecklistDispatch && checklistID != "" {
		if sig, ok := signalByItem(in.Evidence.Signals, checklistID); ok && sig.Signal == "ok" {
			out.Verdict = VerdictStale
			out.Reason = fmt.Sprintf("checklist item %s is ok in cached probe", checklistID)
			return out
		}
	}
	if age := itemAge(item.CreatedAt, now); age >= staleAgeThreshold && in.Evidence.QuietNominal {
		out.Verdict = VerdictStale
		out.Reason = fmt.Sprintf("enqueued %s ago and fleet checklist is nominal", formatAge(age))
		return out
	}

	// STILL_NEEDED
	taskOK := strings.TrimSpace(item.AgentTaskID) != ""
	if taskOK {
		if _, ok := agentgovernance.TaskByID(strings.TrimSpace(item.AgentTaskID)); !ok {
			taskOK = false
		}
	}
	if taskOK && fixScope != "" && fleetSig == "NO-GO" {
		out.Verdict = VerdictStillNeeded
		out.Reason = fmt.Sprintf("scope %s still NO-GO; agent_task_id=%s", fixScope, item.AgentTaskID)
		return out
	}

	// NEEDS_DECISION fallbacks
	if strings.TrimSpace(item.AgentTaskID) == "" {
		out.Verdict = VerdictNeedsDecision
		out.Reason = "missing agent_task_id"
		return out
	}
	if fixScope == "" {
		out.Verdict = VerdictNeedsDecision
		out.Reason = "fix_scope is null / unresolved"
		return out
	}
	if !taskOK {
		out.Verdict = VerdictNeedsDecision
		out.Reason = "invalid agent_task_id"
		return out
	}
	if fleetSig == "unknown" {
		out.Verdict = VerdictNeedsDecision
		out.Reason = "fleet evidence unknown for scope " + fixScope
		return out
	}

	out.Verdict = VerdictNeedsDecision
	out.Reason = "ambiguous triage state"
	return out
}

func fleetEvidenceForScope(scope, checklistID string, ev EvidenceBundle) (signal, detail, failing string) {
	if checklistID != "" {
		if sig, ok := signalByItem(ev.Signals, checklistID); ok {
			return signalToFleet(sig.Signal), formatSignalDetail(checklistID, sig), failingFromSignals(ev.Signals, scope, checklistID)
		}
	}
	if scope == "" {
		if ev.QuietNominal && len(ev.Signals) > 0 {
			return "GO", "checklist cache nominal (no scope)", ""
		}
		return "unknown", "no fix_scope and no checklist item", ""
	}

	related := signalsForScope(ev.Signals, scope)
	if len(related) == 0 {
		if probe, ok := FleetProbeForScope(scope); ok {
			label := probe.Role
			if probe.Env != "" {
				label += ":" + probe.Env
			}
			if ev.QuietNominal && len(ev.Signals) > 0 {
				return "GO", fmt.Sprintf("%s — no scoped checklist rows; overall nominal", label), ""
			}
			return "unknown", fmt.Sprintf("%s — no cached checklist rows for scope", label), ""
		}
		return "unknown", "unmapped scope: " + scope, ""
	}

	worst := "ok"
	var parts []string
	var failIDs []string
	for _, s := range related {
		parts = append(parts, fmt.Sprintf("%s=%s", s.ItemID, s.Signal))
		if s.Signal == "fail" {
			worst = "fail"
			failIDs = append(failIDs, s.ItemID)
		} else if s.Signal == "degraded" && worst != "fail" {
			worst = "degraded"
			failIDs = append(failIDs, s.ItemID)
		} else if s.Signal == "unknown" && worst == "ok" {
			worst = "unknown"
		}
	}
	probeLabel := scope
	if p, ok := FleetProbeForScope(scope); ok {
		probeLabel = p.Role
		if p.Env != "" {
			probeLabel += ":" + p.Env
		}
	}
	return signalToFleet(worst), fmt.Sprintf("%s (%s)", probeLabel, strings.Join(parts, ", ")), strings.Join(failIDs, ", ")
}

func signalToFleet(sig string) string {
	switch sig {
	case "ok":
		return "GO"
	case "fail", "degraded":
		return "NO-GO"
	default:
		return "unknown"
	}
}

func formatSignalDetail(id string, sig EvidenceSignal) string {
	d := strings.TrimSpace(sig.Detail)
	if d == "" {
		return fmt.Sprintf("%s=%s", id, sig.Signal)
	}
	return fmt.Sprintf("%s=%s (%s)", id, sig.Signal, d)
}

func failingFromSignals(signals []EvidenceSignal, scope, checklistID string) string {
	var ids []string
	if checklistID != "" {
		if sig, ok := signalByItem(signals, checklistID); ok && (sig.Signal == "fail" || sig.Signal == "degraded") {
			return checklistID
		}
	}
	for _, s := range signalsForScope(signals, scope) {
		if s.Signal == "fail" || s.Signal == "degraded" {
			ids = append(ids, s.ItemID)
		}
	}
	return strings.Join(ids, ", ")
}

func signalsForScope(signals []EvidenceSignal, scope string) []EvidenceSignal {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return nil
	}
	var out []EvidenceSignal
	for _, s := range signals {
		if checklistFixScope(s.ItemID) == scope {
			out = append(out, s)
		}
	}
	return out
}

func signalByItem(signals []EvidenceSignal, id string) (EvidenceSignal, bool) {
	for _, s := range signals {
		if s.ItemID == id {
			return s, true
		}
	}
	return EvidenceSignal{}, false
}

// checklistFixScope mirrors checklist.CatalogItems FixScope without importing checklist
// (avoids import cycle: checklist → operatequeue).
func checklistFixScope(itemID string) string {
	switch strings.TrimSpace(itemID) {
	case "cluster-api", "nodes-ready", "failing-pods", "postgres", "redis", "nginx-edge", "trade-apis", "massive-polygon":
		return "cluster_issues_full_auto"
	case "db-backup-fresh":
		return "data-layer-backup"
	case "platform-api", "platform-console", "argo-apps":
		return "platform-self-health-recover"
	case "runners-ha", "hermes-tooling":
		return "operator-plane-remediate"
	case "git-bridge":
		return "git-dirty-remediate"
	case "deliver-pipeline", "stg-smoke":
		return "deliver-stg-recover"
	case "ib-feed", "mac-probe-bridge":
		return ""
	default:
		return ""
	}
}

func itemAge(createdAt string, now time.Time) time.Duration {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(createdAt))
	if err != nil {
		return 0
	}
	if now.Before(t) {
		return 0
	}
	return now.Sub(t)
}

func formatAge(d time.Duration) string {
	if d < time.Minute {
		return d.Round(time.Second).String()
	}
	if d < time.Hour {
		return d.Round(time.Minute).String()
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h >= 48 {
		return fmt.Sprintf("%dh%dm", h, m)
	}
	return fmt.Sprintf("%dh%dm", h, m)
}

func formatItemAge(createdAt string, now time.Time) string {
	return formatAge(itemAge(createdAt, now))
}
