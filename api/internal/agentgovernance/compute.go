package agentgovernance

import (
	"fmt"
	"sort"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/mcp"
	"github.com/weitingzhao/bifrost-platform/api/internal/remediation"
)

func ComputePerformance(jobs []remediation.Job) PerformanceResponse {
	now := time.Now().UTC()
	w7 := windowStats(jobs, now.Add(-7*24*time.Hour), "7d")
	w30 := windowStats(jobs, now.Add(-30*24*time.Hour), "30d")
	var mttr *float64
	if v := meanMttrSeconds(jobs); v >= 0 {
		mttr = &v
	}
	return PerformanceResponse{
		GeneratedAt: now,
		Windows:     []PerformanceWindow{w7, w30},
		MttrSeconds: mttr,
		DataSource:  "remediation_jobs",
		JobCount:    len(jobs),
	}
}

func windowStats(jobs []remediation.Job, since time.Time, label string) PerformanceWindow {
	var total, ok, fail, esc int
	var durSum float64
	var durN int
	for _, j := range jobs {
		if j.CreatedAt.Before(since) {
			continue
		}
		total++
		switch j.Status {
		case remediation.JobDone:
			ok++
		case remediation.JobFailed:
			fail++
		case remediation.JobCancelled:
			// cancelled jobs count toward total but not success/failure KPI
		}
		if countEscalations(j) > 0 {
			esc++
		}
		if j.Status != remediation.JobRunning {
			d := j.UpdatedAt.Sub(j.CreatedAt).Seconds()
			if d > 0 && d < 86400*7 {
				durSum += d
				durN++
			}
		}
	}
	w := PerformanceWindow{Window: label, TotalExecutions: total, SuccessCount: ok, FailureCount: fail, EscalationCount: esc}
	if total > 0 {
		w.SuccessRate = float64(ok) / float64(total)
		w.InterventionRate = float64(esc) / float64(total)
	}
	if durN > 0 {
		w.MeanDurationMs = durSum / float64(durN) * 1000
	}
	return w
}

func meanMttrSeconds(jobs []remediation.Job) float64 {
	var sum float64
	var n int
	for _, j := range jobs {
		if j.Status != remediation.JobDone {
			continue
		}
		d := j.UpdatedAt.Sub(j.CreatedAt).Seconds()
		if d <= 0 || d >= 86400*7 {
			continue
		}
		sum += d
		n++
	}
	if n == 0 {
		return -1
	}
	return sum / float64(n)
}

func countEscalations(j remediation.Job) int {
	n := 0
	for _, ev := range j.Events {
		if ev.Type == remediation.EventApprovalRequest {
			n++
		}
	}
	return n
}

func ComputeTrustMatrix(jobs []remediation.Job) TrustMatrixResponse {
	return ApplyTrustOverrides(computeTrustMatrixRaw(jobs), nil)
}

func computeTrustMatrixRaw(jobs []remediation.Job) TrustMatrixResponse {
	now := time.Now().UTC()
	byScope := groupJobsByScope(jobs)
	entries := make([]TrustMatrixEntry, 0, len(TaskCatalog()))
	for _, task := range TaskCatalog() {
		scopeJobs := byScope[task.Scope]
		entry := trustEntryForTask(task, scopeJobs)
		entries = append(entries, entry)
	}
	return TrustMatrixResponse{
		GeneratedAt: now,
		Entries:     entries,
		DataSource:  "remediation_jobs+catalog",
	}
}

func ApplyTrustOverrides(resp TrustMatrixResponse, overrides map[string]TrustOverride) TrustMatrixResponse {
	if len(overrides) == 0 {
		return resp
	}
	out := resp
	out.Entries = make([]TrustMatrixEntry, len(resp.Entries))
	copy(out.Entries, resp.Entries)
	for i := range out.Entries {
		o, ok := overrides[out.Entries[i].SkillID]
		if !ok {
			continue
		}
		if o.Level == "L0" || o.Level == "L1" || o.Level == "L2" {
			out.Entries[i].CurrentLevel = o.Level
		}
		if !o.AppliedAt.IsZero() {
			out.Entries[i].LastOverrideAt = o.AppliedAt.UTC().Format(time.RFC3339)
		}
		out.Entries[i].LastOverrideBy = o.AppliedBy
		// Recompute promotion eligibility against effective level.
		e := &out.Entries[i]
		e.PromotionEligible = e.CurrentLevel == "L1" && e.ConsecutiveSuccesses >= PromotionThreshold && !e.DemotionTriggered
		if e.PromotionEligible {
			e.SuggestedLevel = "L0"
			if o.Reason == "" {
				e.SuggestedLevelReason = "Earned autonomy — consecutive successes at L1"
			}
		} else if e.DemotionTriggered && e.CurrentLevel == "L0" {
			e.SuggestedLevel = "L1"
			if o.Reason == "" {
				e.SuggestedLevelReason = "Failure spike — demote to confirm before auto actuation"
			}
		} else {
			e.SuggestedLevel = ""
		}
	}
	out.DataSource = resp.DataSource + "+owner_overrides"
	return out
}

func groupJobsByScope(jobs []remediation.Job) map[string][]remediation.Job {
	m := make(map[string][]remediation.Job)
	for _, j := range jobs {
		scope := normalizeScope(j.Scope)
		m[scope] = append(m[scope], j)
	}
	for k := range m {
		sort.Slice(m[k], func(i, j int) bool {
			return m[k][i].UpdatedAt.After(m[k][j].UpdatedAt)
		})
	}
	return m
}

func trustEntryForTask(task TaskDef, jobs []remediation.Job) TrustMatrixEntry {
	level := task.DefaultLevel
	consecutive := consecutiveSuccesses(jobs)
	demotion := demotionTriggered(jobs)
	promotion := level == "L1" && consecutive >= PromotionThreshold && !demotion
	entry := TrustMatrixEntry{
		SkillID:              task.ID,
		SkillLabel:           task.Label,
		CurrentLevel:         level,
		ConsecutiveSuccesses: consecutive,
		PromotionEligible:    promotion,
		DemotionTriggered:    demotion,
	}
	if promotion {
		entry.SuggestedLevel = "L0"
		entry.SuggestedLevelReason = "Earned autonomy — consecutive successes at L1"
	}
	if demotion && level == "L0" {
		entry.SuggestedLevel = "L1"
		entry.SuggestedLevelReason = "Failure spike — demote to confirm before auto actuation"
	}
	return entry
}

func consecutiveSuccesses(jobs []remediation.Job) int {
	n := 0
	for _, j := range jobs {
		if j.Status == remediation.JobDone {
			n++
			continue
		}
		if j.Status == remediation.JobFailed {
			break
		}
	}
	return n
}

func demotionTriggered(jobs []remediation.Job) bool {
	if len(jobs) == 0 {
		return false
	}
	if jobs[0].Status == remediation.JobFailed {
		return true
	}
	fail := 0
	limit := 3
	if len(jobs) < limit {
		limit = len(jobs)
	}
	for i := 0; i < limit; i++ {
		if jobs[i].Status == remediation.JobFailed {
			fail++
		}
	}
	return fail >= 2
}

func ComputeCapabilityMap() CapabilityMapResponse {
	now := time.Now().UTC()
	tools := mcp.Catalog()
	impl := make(map[string]struct{})
	for _, t := range tools {
		if t.Implemented {
			impl[t.Name] = struct{}{}
		}
	}
	entries := make([]CapabilityMapEntry, 0, len(TaskCatalog()))
	gaps := 0
	for _, task := range TaskCatalog() {
		missing := make([]string, 0)
		for _, tool := range task.McpTools {
			if _, ok := impl[tool]; !ok {
				missing = append(missing, tool)
			}
		}
		hasGap := len(missing) > 0
		if hasGap {
			gaps++
		}
		e := CapabilityMapEntry{
			TaskScope:      task.Scope,
			TaskLabel:      task.Label,
			Autonomy:       task.DefaultLevel,
			McpTools:       task.McpTools,
			MissionSignals: task.MissionSignals,
			HasGap:         hasGap,
		}
		if hasGap {
			e.GapDetail = "missing MCP: " + joinStrings(missing)
		}
		entries = append(entries, e)
	}
	return CapabilityMapResponse{
		GeneratedAt:  now,
		Entries:      entries,
		GapCount:     gaps,
		McpToolCount: len(tools),
	}
}

func joinStrings(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	s := parts[0]
	for i := 1; i < len(parts); i++ {
		s += ", " + parts[i]
	}
	return s
}

func ComputeBriefing(jobs []remediation.Job, trust TrustMatrixResponse) BriefingDigest {
	since := time.Now().UTC().Add(-24 * time.Hour)
	done, fail, esc := 0, 0, 0
	for _, j := range jobs {
		if j.UpdatedAt.Before(since) {
			continue
		}
		switch j.Status {
		case remediation.JobDone:
			done++
		case remediation.JobFailed:
			fail++
		}
		esc += countEscalations(j)
	}
	promo, demo := 0, 0
	for _, e := range trust.Entries {
		if e.PromotionEligible {
			promo++
		}
		if e.DemotionTriggered {
			demo++
		}
	}
	summary := "Last 24h: "
	if done+fail == 0 {
		summary += "no remediation jobs completed."
	} else {
		summary += fmt.Sprintf("%d completed, %d failed, %d escalations.", done, fail, esc)
	}
	if promo > 0 {
		summary += fmt.Sprintf(" %d skill(s) eligible for L0 promotion.", promo)
	}
	if demo > 0 {
		summary += fmt.Sprintf(" %d skill(s) flagged for demotion.", demo)
	}
	return BriefingDigest{
		PeriodHours:      24,
		JobsCompleted:    done,
		JobsFailed:       fail,
		Escalations:      esc,
		PromotionPending: promo,
		Demotions:        demo,
		Summary:          summary,
	}
}
