package retrospective

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/remediation"
)

// Analyzer performs cross-job pattern analysis on remediation history.
type Analyzer struct {
	store *remediation.JobStore
}

func NewAnalyzer(store *remediation.JobStore) *Analyzer {
	return &Analyzer{store: store}
}

// Analyze reads all jobs and produces a structured report.
func (a *Analyzer) Analyze() AnalysisReport {
	jobs := a.store.List()
	now := time.Now().UTC()

	report := AnalysisReport{
		GeneratedAt: now,
		TotalJobs:   len(jobs),
	}

	if len(jobs) == 0 {
		report.HealthScore = 100
		return report
	}

	oldest := jobs[len(jobs)-1].CreatedAt
	report.AnalysisWindow = fmt.Sprintf("%s to %s", oldest.Format("2006-01-02"), now.Format("2006-01-02"))

	report.ScopeStats = computeScopeStats(jobs)
	report.ToolUsage = computeToolUsage(jobs)
	report.Namespaces = computeNamespaceActivity(jobs)
	report.Patterns = clusterPatterns(jobs)
	report.RootCauseDist = computeRootCauseDist(report.Patterns)
	report.HealthScore = computeHealthScore(jobs, report.Patterns)
	report.Insights = generateInsights(report)

	return report
}

// --- scope stats ---

func computeScopeStats(jobs []remediation.Job) []ScopeStats {
	type accum struct {
		total, done, failed, cancelled, running int
		durations                                []float64
	}
	m := map[string]*accum{}
	for _, j := range jobs {
		scope := j.Scope
		if scope == "" {
			scope = "unknown"
		}
		a, ok := m[scope]
		if !ok {
			a = &accum{}
			m[scope] = a
		}
		a.total++
		switch j.Status {
		case remediation.JobDone:
			a.done++
		case remediation.JobFailed:
			a.failed++
		case remediation.JobCancelled:
			a.cancelled++
		case remediation.JobRunning:
			a.running++
		}
		dur := j.UpdatedAt.Sub(j.CreatedAt).Seconds()
		if dur > 0 && dur < 86400*7 && j.Status != remediation.JobRunning {
			a.durations = append(a.durations, dur)
		}
	}

	out := make([]ScopeStats, 0, len(m))
	for scope, a := range m {
		ss := ScopeStats{
			Scope:     scope,
			Total:     a.total,
			Done:      a.done,
			Failed:    a.failed,
			Cancelled: a.cancelled,
			Running:   a.running,
		}
		completed := a.done + a.failed
		if completed > 0 {
			ss.SuccessRate = float64(a.done) / float64(completed) * 100
		}
		if len(a.durations) > 0 {
			var sum float64
			for _, d := range a.durations {
				sum += d
			}
			ss.AvgDuration = sum / float64(len(a.durations))
		}
		out = append(out, ss)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Total > out[j].Total })
	return out
}

// --- tool usage ---

func computeToolUsage(jobs []remediation.Job) []ToolUsage {
	type info struct {
		count int
		jobIDs map[string]bool
	}
	m := map[string]*info{}

	for _, j := range jobs {
		for _, ev := range j.Events {
			if ev.Type != remediation.EventToolCall {
				continue
			}
			toolName := extractToolName(ev)
			if toolName == "" {
				continue
			}
			inf, ok := m[toolName]
			if !ok {
				inf = &info{jobIDs: map[string]bool{}}
				m[toolName] = inf
			}
			inf.count++
			inf.jobIDs[j.ID] = true
		}
	}

	out := make([]ToolUsage, 0, len(m))
	for tool, inf := range m {
		out = append(out, ToolUsage{
			Tool:  tool,
			Count: inf.count,
			Jobs:  len(inf.jobIDs),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	if len(out) > 20 {
		out = out[:20]
	}
	return out
}

// --- namespace activity ---

func computeNamespaceActivity(jobs []remediation.Job) []NamespaceActivity {
	type nsInfo struct {
		toolCalls int
		jobIDs    map[string]bool
		tools     map[string]int
	}
	m := map[string]*nsInfo{}

	for _, j := range jobs {
		for _, ev := range j.Events {
			if ev.Type != remediation.EventToolCall {
				continue
			}
			ns := extractNamespace(ev)
			if ns == "" {
				continue
			}
			inf, ok := m[ns]
			if !ok {
				inf = &nsInfo{jobIDs: map[string]bool{}, tools: map[string]int{}}
				m[ns] = inf
			}
			inf.toolCalls++
			inf.jobIDs[j.ID] = true
			toolName := extractToolName(ev)
			if toolName != "" {
				inf.tools[toolName]++
			}
		}
	}

	out := make([]NamespaceActivity, 0, len(m))
	for ns, inf := range m {
		na := NamespaceActivity{
			Namespace: ns,
			ToolCalls: inf.toolCalls,
			Jobs:      len(inf.jobIDs),
		}
		for tool, count := range inf.tools {
			na.TopActions = append(na.TopActions, ActionTaken{Tool: tool, Count: count})
		}
		sort.Slice(na.TopActions, func(i, j int) bool { return na.TopActions[i].Count > na.TopActions[j].Count })
		if len(na.TopActions) > 5 {
			na.TopActions = na.TopActions[:5]
		}
		out = append(out, na)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ToolCalls > out[j].ToolCalls })
	return out
}

// --- pattern clustering ---

func clusterPatterns(jobs []remediation.Job) []PatternCluster {
	clusters := map[string]*clusterAccum{}

	for _, j := range jobs {
		if j.Status == remediation.JobRunning {
			continue
		}
		scope := j.Scope
		if scope == "" {
			scope = "unknown"
		}
		namespaces := extractAllNamespaces(j)
		if len(namespaces) == 0 {
			namespaces = []string{"_global"}
		}
		for _, ns := range namespaces {
			key := scope + ":" + ns
			ca, ok := clusters[key]
			if !ok {
				ca = &clusterAccum{
					scope:     scope,
					namespace: ns,
					actions:   map[string]int{},
				}
				clusters[key] = ca
			}
			ca.jobs = append(ca.jobs, j)
			if j.InitBrief != "" {
				ca.initBriefs = append(ca.initBriefs, truncate(j.InitBrief, 500))
			}
			if j.Summary != "" {
				ca.summaries = append(ca.summaries, truncate(j.Summary, 500))
			}
			for _, ev := range j.Events {
				if ev.Type == remediation.EventToolCall {
					if tn := extractToolName(ev); tn != "" {
						ca.actions[tn]++
					}
				}
				if ev.Type == remediation.EventError && ev.Text != "" {
					ca.errors = append(ca.errors, truncate(ev.Text, 200))
				}
				if ev.Type == remediation.EventApprovalRequest {
					ca.approvals++
				}
			}
			if j.Error != "" {
				ca.errors = append(ca.errors, truncate(j.Error, 200))
			}
		}
	}

	// Only emit clusters with >= 2 occurrences (that's what makes a "pattern")
	out := make([]PatternCluster, 0)
	for key, ca := range clusters {
		if len(ca.jobs) < 2 {
			continue
		}
		pc := PatternCluster{
			ID:          "pat-" + sanitizeKey(key),
			Label:       buildPatternLabel(ca.scope, ca.namespace),
			Description: buildPatternDescription(ca),
			Component: ComponentRef{
				Namespace: ca.namespace,
			},
			Occurrences: len(ca.jobs),
		}

		pc.RootCause, pc.Confidence, pc.Signals = classifyRootCauseMultiSignal(ca)
		pc.Severity = classifySeverity(ca)

		// time range
		sort.Slice(ca.jobs, func(i, j int) bool {
			return ca.jobs[i].CreatedAt.Before(ca.jobs[j].CreatedAt)
		})
		pc.FirstSeen = ca.jobs[0].CreatedAt.Format(time.RFC3339)
		pc.LastSeen = ca.jobs[len(ca.jobs)-1].CreatedAt.Format(time.RFC3339)

		// job refs
		for _, j := range ca.jobs {
			pc.Jobs = append(pc.Jobs, JobRef{
				ID:        j.ID,
				Scope:     j.Scope,
				Status:    string(j.Status),
				CreatedAt: j.CreatedAt.Format(time.RFC3339),
			})
		}

		// top actions
		for tool, count := range ca.actions {
			pc.TopActions = append(pc.TopActions, ActionTaken{Tool: tool, Count: count})
		}
		sort.Slice(pc.TopActions, func(i, j int) bool {
			return pc.TopActions[i].Count > pc.TopActions[j].Count
		})
		if len(pc.TopActions) > 5 {
			pc.TopActions = pc.TopActions[:5]
		}

		// success rate
		var done, terminal int
		for _, j := range ca.jobs {
			if j.Status == remediation.JobDone || j.Status == remediation.JobFailed {
				terminal++
				if j.Status == remediation.JobDone {
					done++
				}
			}
		}
		if terminal > 0 {
			pc.SuccessRate = float64(done) / float64(terminal) * 100
		}

		// avg duration (exclude running)
		var durSum float64
		var durCount int
		for _, j := range ca.jobs {
			if j.Status == remediation.JobRunning {
				continue
			}
			dur := j.UpdatedAt.Sub(j.CreatedAt).Seconds()
			if dur > 0 && dur < 86400 {
				durSum += dur
				durCount++
			}
		}
		if durCount > 0 {
			pc.AvgDuration = durSum / float64(durCount)
		}

		// trending (compare first half vs second half frequency)
		pc.Trending = computeTrend(ca.jobs)

		out = append(out, pc)
	}

	sort.Slice(out, func(i, j int) bool {
		si := severityRank(out[i].Severity)
		sj := severityRank(out[j].Severity)
		if si != sj {
			return si > sj
		}
		return out[i].Occurrences > out[j].Occurrences
	})
	return out
}

// --- health score ---

func computeHealthScore(jobs []remediation.Job, patterns []PatternCluster) float64 {
	if len(jobs) == 0 {
		return 100
	}
	var done, failed int
	for _, j := range jobs {
		switch j.Status {
		case remediation.JobDone:
			done++
		case remediation.JobFailed:
			failed++
		}
	}
	successRate := 100.0
	if done+failed > 0 {
		successRate = float64(done) / float64(done+failed) * 100
	}

	// Penalize recurring critical/high patterns
	var patternPenalty float64
	for _, p := range patterns {
		switch p.Severity {
		case SeverityCritical:
			patternPenalty += 15
		case SeverityHigh:
			patternPenalty += 8
		case SeverityMedium:
			patternPenalty += 3
		}
	}
	if patternPenalty > 40 {
		patternPenalty = 40
	}

	score := successRate - patternPenalty
	if score < 0 {
		score = 0
	}
	return score
}

// --- insights ---

func generateInsights(report AnalysisReport) []string {
	var ins []string

	if report.TotalJobs == 0 {
		return []string{"No remediation jobs found — system is stable or agent has not been active."}
	}

	// Top pattern
	if len(report.Patterns) > 0 {
		top := report.Patterns[0]
		ins = append(ins, fmt.Sprintf(
			"Most recurring pattern: %q (%d occurrences, severity %s, success rate %.0f%%)",
			top.Label, top.Occurrences, top.Severity, top.SuccessRate))
	}

	// Overall success rate
	var done, failed int
	for _, ss := range report.ScopeStats {
		done += ss.Done
		failed += ss.Failed
	}
	if done+failed > 0 {
		rate := float64(done) / float64(done+failed) * 100
		ins = append(ins, fmt.Sprintf("Overall success rate: %.0f%% (%d done / %d failed)", rate, done, failed))
	}

	// Most active namespace
	if len(report.Namespaces) > 0 {
		top := report.Namespaces[0]
		ins = append(ins, fmt.Sprintf("Most active namespace: %s (%d tool calls across %d jobs)", top.Namespace, top.ToolCalls, top.Jobs))
	}

	// Most used tool
	if len(report.ToolUsage) > 0 {
		top := report.ToolUsage[0]
		ins = append(ins, fmt.Sprintf("Most used tool: %s (%d calls across %d jobs)", top.Tool, top.Count, top.Jobs))
	}

	// Root cause distribution
	if len(report.RootCauseDist) > 0 {
		var parts []string
		for _, rc := range report.RootCauseDist {
			parts = append(parts, fmt.Sprintf("%s: %d (%.0f%%)", rc.Cause, rc.Count, rc.Fraction*100))
		}
		ins = append(ins, "Root cause distribution: "+strings.Join(parts, ", "))
	}

	// Trending up patterns
	for _, p := range report.Patterns {
		if p.Trending == "up" {
			ins = append(ins, fmt.Sprintf("⚠ Pattern %q is trending up (classified as %s, confidence %.0f%%) — investigate root cause",
				p.Label, p.RootCause, p.Confidence*100))
		}
	}

	// High-confidence platform defects
	for _, p := range report.Patterns {
		if p.RootCause == RootCausePlatformDefect && p.Confidence >= 0.6 && p.Trending != "down" {
			ins = append(ins, fmt.Sprintf("Platform defect: %q (%.0f%% confidence, %d occurrences) — consider structural fix",
				p.Label, p.Confidence*100, p.Occurrences))
		}
	}

	return ins
}

// --- helpers ---

func extractToolName(ev remediation.Event) string {
	meta := ev.Meta
	if meta == nil {
		return ""
	}
	name, _ := meta["name"].(string)
	if name == "mcp" {
		if args, ok := meta["args"].(map[string]any); ok {
			if tn, ok := args["toolName"].(string); ok {
				return tn
			}
		}
	}
	if name != "" {
		return name
	}
	return ""
}

func extractNamespace(ev remediation.Event) string {
	meta := ev.Meta
	if meta == nil {
		return ""
	}
	if args, ok := meta["args"].(map[string]any); ok {
		if inner, ok := args["args"].(map[string]any); ok {
			if ns, ok := inner["namespace"].(string); ok && ns != "" {
				return ns
			}
		}
	}
	return ""
}

func extractAllNamespaces(j remediation.Job) []string {
	seen := map[string]bool{}
	for _, ev := range j.Events {
		if ev.Type != remediation.EventToolCall {
			continue
		}
		ns := extractNamespace(ev)
		if ns != "" {
			seen[ns] = true
		}
	}
	out := make([]string, 0, len(seen))
	for ns := range seen {
		out = append(out, ns)
	}
	sort.Strings(out)
	return out
}

func buildPatternLabel(scope, namespace string) string {
	if namespace == "_global" {
		return scope
	}
	return scope + " → " + namespace
}

func buildPatternDescription(ca *clusterAccum) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("%d occurrences of %s scope targeting %s", len(ca.jobs), ca.scope, ca.namespace))
	if len(ca.errors) > 0 {
		unique := uniqueStrings(ca.errors)
		if len(unique) == 1 {
			parts = append(parts, "error: "+unique[0])
		} else {
			parts = append(parts, fmt.Sprintf("%d distinct errors", len(unique)))
		}
	}
	return strings.Join(parts, "; ")
}

// classifyRootCauseMultiSignal scores each root-cause category across
// multiple independent signals, then picks the highest-scoring category.
// Returns the winner, a 0.0–1.0 confidence, and the contributing signals.
func classifyRootCauseMultiSignal(ca *clusterAccum) (RootCause, float64, []ClassificationSignal) {
	scores := map[RootCause]float64{}
	var signals []ClassificationSignal

	emit := func(cause RootCause, weight float64, name, detail string) {
		scores[cause] += weight
		signals = append(signals, ClassificationSignal{
			Name: name, Weight: weight, Cause: cause, Detail: detail,
		})
	}

	// --- 1. Tool-pattern signals ---
	restartTools := ca.actions["delete_pod"] + ca.actions["rollout_restart_deployment"]
	diagnosticTools := ca.actions["kubectl_logs"] + ca.actions["kubectl_describe_pod"] + ca.actions["kubectl_exec"]
	gitTools := ca.actions["git_commit"] + ca.actions["git_workspace_status"]
	configTools := ca.actions["apply_manifest"] + ca.actions["kubectl_apply"]

	if restartTools > 0 && len(ca.errors) == 0 {
		emit(RootCauseTransient, 3.0, "restart_tools_no_error",
			fmt.Sprintf("restart tools (%d calls) with no errors → likely transient", restartTools))
	}
	if restartTools > 0 && len(ca.errors) > 0 {
		emit(RootCausePlatformDefect, 1.5, "restart_tools_with_errors",
			fmt.Sprintf("restart tools used but errors present → may indicate deeper issue"))
	}
	if gitTools > 0 {
		emit(RootCauseConfigDrift, 2.0, "git_tools_used",
			fmt.Sprintf("git operations (%d calls) suggest config/code changes needed", gitTools))
	}
	if configTools > 0 {
		emit(RootCauseConfigDrift, 1.5, "config_apply_tools",
			fmt.Sprintf("manifest/config apply tools (%d calls) → config correction", configTools))
	}
	if diagnosticTools > 10*len(ca.jobs) {
		emit(RootCausePlatformDefect, 1.0, "heavy_diagnostics",
			fmt.Sprintf("deep diagnostic investigation (%d calls) → complex/systemic issue", diagnosticTools))
	}

	// --- 2. Scope-based signals ---
	scopeLower := strings.ToLower(ca.scope)
	if strings.Contains(scopeLower, "drift") {
		emit(RootCauseConfigDrift, 3.0, "scope_drift", "scope explicitly mentions drift")
	}
	if strings.Contains(scopeLower, "release") {
		emit(RootCausePlatformDefect, 1.5, "scope_release", "release scope — failures indicate platform/CI issues")
	}
	if scopeLower == "agent-desk" {
		emit(RootCauseTransient, 0.5, "scope_agent_desk", "ad-hoc dispatch — often one-off investigations")
	}
	if strings.Contains(scopeLower, "health") || strings.Contains(scopeLower, "nightly") {
		emit(RootCauseTransient, 1.0, "scope_scheduled_check", "scheduled verification — routine maintenance")
	}

	// --- 3. Error text signals ---
	for _, e := range uniqueStrings(ca.errors) {
		lower := strings.ToLower(e)
		if strings.Contains(lower, "oom") || strings.Contains(lower, "out of memory") || strings.Contains(lower, "evict") {
			emit(RootCauseResourceLimit, 4.0, "error_oom", truncate(e, 80))
		}
		if strings.Contains(lower, "memory") && !strings.Contains(lower, "oom") {
			emit(RootCauseResourceLimit, 2.0, "error_memory", truncate(e, 80))
		}
		if strings.Contains(lower, "disk") || strings.Contains(lower, "quota") || strings.Contains(lower, "no space") {
			emit(RootCauseResourceLimit, 3.0, "error_disk_quota", truncate(e, 80))
		}
		if strings.Contains(lower, "timeout") || strings.Contains(lower, "connection refused") || strings.Contains(lower, "unreachable") {
			emit(RootCauseExternal, 3.0, "error_connectivity", truncate(e, 80))
		}
		if strings.Contains(lower, "dns") || strings.Contains(lower, "name or service not known") || strings.Contains(lower, "resolve") {
			if strings.Contains(lower, "svc.cluster.local") {
				emit(RootCauseProbeDrift, 3.5, "error_probe_drift_dns",
					"in-cluster DNS probe from Mac host — matrix vs cluster contradiction")
			} else {
				emit(RootCauseConfigDrift, 2.5, "error_dns_resolve", truncate(e, 80))
			}
		}
		if strings.Contains(lower, "probe_drift") || (strings.Contains(lower, "cluster_api") && strings.Contains(lower, "matrix fail")) {
			emit(RootCauseProbeDrift, 3.0, "error_probe_drift", truncate(e, 80))
		}
		if strings.Contains(lower, "permission") || strings.Contains(lower, "owner") || strings.Contains(lower, "forbidden") {
			emit(RootCausePlatformDefect, 3.0, "error_permission", truncate(e, 80))
		}
		if strings.Contains(lower, "crashloop") || strings.Contains(lower, "backoff") {
			emit(RootCausePlatformDefect, 2.0, "error_crashloop", truncate(e, 80))
		}
		if strings.Contains(lower, "image") && (strings.Contains(lower, "pull") || strings.Contains(lower, "not found")) {
			emit(RootCausePlatformDefect, 3.0, "error_image_pull", truncate(e, 80))
		}
	}

	// --- 4. InitBrief signals (what triggered the job) ---
	for _, brief := range uniqueStrings(ca.initBriefs) {
		lower := strings.ToLower(brief)
		if strings.Contains(lower, "crashloopbackoff") || strings.Contains(lower, "failing pod") {
			emit(RootCausePlatformDefect, 2.0, "brief_crashloop", "init brief mentions CrashLoopBackOff/failing pods")
		}
		if strings.Contains(lower, "no issues") || strings.Contains(lower, "health verification pass") {
			emit(RootCauseTransient, 1.5, "brief_healthy", "init brief reports no issues — routine check")
		}
	}

	// --- 5. Summary signals (Agent's own analysis) ---
	for _, summary := range uniqueStrings(ca.summaries) {
		lower := strings.ToLower(summary)
		if strings.Contains(lower, "configmap") || strings.Contains(lower, "config drift") || strings.Contains(lower, "config mismatch") {
			emit(RootCauseConfigDrift, 3.0, "summary_config_issue", "Agent summary identifies ConfigMap/config issue")
		}
		if strings.Contains(lower, "redis.host") || strings.Contains(lower, "fqdn") || strings.Contains(lower, "service name") {
			emit(RootCauseConfigDrift, 2.5, "summary_service_discovery", "Agent summary references service discovery misconfiguration")
		}
		if strings.Contains(lower, "owner") && strings.Contains(lower, "postgres") {
			emit(RootCausePlatformDefect, 3.0, "summary_db_owner", "Agent summary identifies DB object ownership issue")
		}
	}

	// --- 6. Temporal signals ---
	if len(ca.jobs) >= 2 {
		sorted := make([]remediation.Job, len(ca.jobs))
		copy(sorted, ca.jobs)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].CreatedAt.Before(sorted[j].CreatedAt)
		})
		span := sorted[len(sorted)-1].CreatedAt.Sub(sorted[0].CreatedAt)
		avgGap := span / time.Duration(len(sorted)-1)
		if avgGap < 2*time.Hour {
			emit(RootCauseTransient, 1.5, "temporal_burst", fmt.Sprintf("avg gap %.0fmin → burst pattern (transient)", avgGap.Minutes()))
		} else if avgGap > 24*time.Hour {
			emit(RootCausePlatformDefect, 1.5, "temporal_persistent", fmt.Sprintf("avg gap %.0fh → persistent/recurring defect", avgGap.Hours()))
		}
	}

	// --- 7. Approval signals ---
	if ca.approvals > 0 {
		emit(RootCausePlatformDefect, 1.0, "required_approval",
			fmt.Sprintf("%d approval requests → non-trivial remediation", ca.approvals))
	}

	// --- 8. Failure rate signal ---
	failCount := 0
	for _, j := range ca.jobs {
		if j.Status == remediation.JobFailed {
			failCount++
		}
	}
	if failCount > 0 {
		emit(RootCausePlatformDefect, float64(failCount)*1.5, "job_failures",
			fmt.Sprintf("%d jobs failed → indicates hard-to-fix issue", failCount))
	}

	// --- Pick winner ---
	if len(scores) == 0 {
		return RootCauseUnknown, 0.0, signals
	}

	var bestCause RootCause
	var bestScore float64
	var totalScore float64
	for cause, score := range scores {
		totalScore += score
		if score > bestScore {
			bestScore = score
			bestCause = cause
		}
	}

	confidence := bestScore / totalScore
	if confidence > 1.0 {
		confidence = 1.0
	}

	// Sort signals by weight descending for display
	sort.Slice(signals, func(i, j int) bool {
		return signals[i].Weight > signals[j].Weight
	})
	if len(signals) > 8 {
		signals = signals[:8]
	}

	return bestCause, confidence, signals
}

// computeRootCauseDist builds the distribution of root causes across patterns.
func computeRootCauseDist(patterns []PatternCluster) []RootCauseDistribution {
	counts := map[RootCause]int{}
	for _, p := range patterns {
		counts[p.RootCause]++
	}
	total := len(patterns)
	out := make([]RootCauseDistribution, 0, len(counts))
	for cause, count := range counts {
		out = append(out, RootCauseDistribution{
			Cause:    cause,
			Count:    count,
			Fraction: float64(count) / float64(total),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	return out
}

func classifySeverity(ca *clusterAccum) Severity {
	failCount := 0
	for _, j := range ca.jobs {
		if j.Status == remediation.JobFailed {
			failCount++
		}
	}

	isProd := ca.namespace == "bifrost-prod" || ca.namespace == "bifrost-platform-prod"

	if isProd && failCount > 0 {
		return SeverityCritical
	}
	if isProd && len(ca.jobs) >= 3 {
		return SeverityHigh
	}
	if failCount > 0 {
		return SeverityHigh
	}
	if len(ca.jobs) >= 5 {
		return SeverityMedium
	}
	return SeverityLow
}

func computeTrend(jobs []remediation.Job) string {
	if len(jobs) < 4 {
		return "stable"
	}
	mid := len(jobs) / 2
	firstHalf := jobs[:mid]
	secondHalf := jobs[mid:]

	if len(firstHalf) == 0 || len(secondHalf) == 0 {
		return "stable"
	}

	firstSpan := firstHalf[len(firstHalf)-1].CreatedAt.Sub(firstHalf[0].CreatedAt)
	secondSpan := secondHalf[len(secondHalf)-1].CreatedAt.Sub(secondHalf[0].CreatedAt)

	if firstSpan <= 0 || secondSpan <= 0 {
		return "stable"
	}

	firstRate := float64(len(firstHalf)) / firstSpan.Hours()
	secondRate := float64(len(secondHalf)) / secondSpan.Hours()

	if secondRate > firstRate*1.5 {
		return "up"
	}
	if secondRate < firstRate*0.5 {
		return "down"
	}
	return "stable"
}

func severityRank(s Severity) int {
	switch s {
	case SeverityCritical:
		return 4
	case SeverityHigh:
		return 3
	case SeverityMedium:
		return 2
	case SeverityLow:
		return 1
	default:
		return 0
	}
}

func sanitizeKey(s string) string {
	r := strings.NewReplacer(":", "-", " ", "-", "/", "-")
	return r.Replace(s)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

func uniqueStrings(ss []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0)
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

type clusterAccum struct {
	scope      string
	namespace  string
	jobs       []remediation.Job
	actions    map[string]int
	errors     []string
	initBriefs []string
	summaries  []string
	approvals  int
}
