package agentgovernance

import (
	"sort"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/remediation"
)

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
		if o.AppliedAt != "" {
			out.Entries[i].LastOverrideAt = o.AppliedAt
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
