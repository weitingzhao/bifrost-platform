package retrospective

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/remediation"
)

// newTestJobStore points remediation.NewJobStore at a fresh temp directory so
// each test gets an isolated, file-backed store (matches JobStore's real
// persistence model — no interface mocking needed).
func newTestJobStore(t *testing.T) *remediation.JobStore {
	t.Helper()
	dir := t.TempDir()
	_ = os.Setenv("PLATFORM_REMEDIATION_JOBS_DIR", dir)
	t.Cleanup(func() { _ = os.Unsetenv("PLATFORM_REMEDIATION_JOBS_DIR") })
	return remediation.NewJobStore()
}

// toolEvent builds a tool_call event matching the nested meta shape that
// extractToolName / extractNamespace expect: meta.args.args.namespace.
func toolEvent(id, tool, namespace string) remediation.Event {
	return remediation.Event{
		ID:   id,
		At:   time.Now().UTC(),
		Type: remediation.EventToolCall,
		Text: tool,
		Meta: map[string]any{
			"name": tool,
			"args": map[string]any{
				"args": map[string]any{
					"namespace": namespace,
				},
			},
		},
	}
}

func errorEvent(id, text string) remediation.Event {
	return remediation.Event{ID: id, At: time.Now().UTC(), Type: remediation.EventError, Text: text}
}

func TestAnalyzeEmptyStoreReturnsHealthyDefaults(t *testing.T) {
	store := newTestJobStore(t)
	report := NewAnalyzer(store).Analyze()

	if report.TotalJobs != 0 {
		t.Fatalf("TotalJobs = %d, want 0", report.TotalJobs)
	}
	if report.HealthScore != 100 {
		t.Fatalf("HealthScore = %v, want 100", report.HealthScore)
	}
	if len(report.Patterns) != 0 || len(report.ScopeStats) != 0 || len(report.ToolUsage) != 0 {
		t.Fatalf("expected all empty slices, got patterns=%d scope=%d tools=%d",
			len(report.Patterns), len(report.ScopeStats), len(report.ToolUsage))
	}
	if len(report.Insights) != 0 {
		t.Fatalf("expected no insights for empty job set, got: %+v", report.Insights)
	}
}

func TestGenerateInsightsNoJobsMessage(t *testing.T) {
	insights := generateInsights(AnalysisReport{TotalJobs: 0})
	if len(insights) != 1 || !strings.Contains(insights[0], "No remediation jobs found") {
		t.Fatalf("unexpected insights: %+v", insights)
	}
}

func TestAnalyzeProducesPatternsScopesAndInsights(t *testing.T) {
	store := newTestJobStore(t)
	t0 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	// Cluster 1: prod scope with a restart + OOM error + one failure → critical / platform_defect.
	store.Put(remediation.Job{
		ID: "job-prod-1", Scope: "platform-self-health-recover", Status: remediation.JobFailed,
		CreatedAt: t0, UpdatedAt: t0.Add(5 * time.Minute),
		Events: []remediation.Event{
			toolEvent("e1", "delete_pod", "bifrost-prod"),
			errorEvent("e2", "OOMKilled: out of memory"),
		},
	})
	store.Put(remediation.Job{
		ID: "job-prod-2", Scope: "platform-self-health-recover", Status: remediation.JobDone,
		CreatedAt: t0.Add(2 * time.Hour), UpdatedAt: t0.Add(2*time.Hour + 10*time.Minute),
		InitBrief: "CrashLoopBackOff detected in bifrost-prod",
		Events: []remediation.Event{
			toolEvent("e3", "delete_pod", "bifrost-prod"),
			toolEvent("e4", "kubectl_logs", "bifrost-prod"),
		},
	})

	// Cluster 2: dev scope, git/config tools, no errors → low severity / config_drift.
	store.Put(remediation.Job{
		ID: "job-dev-1", Scope: "gitops-config-repair", Status: remediation.JobDone,
		CreatedAt: t0.Add(24 * time.Hour), UpdatedAt: t0.Add(24*time.Hour + 5*time.Minute),
		Events: []remediation.Event{toolEvent("e5", "git_commit", "bifrost-dev")},
	})
	store.Put(remediation.Job{
		ID: "job-dev-2", Scope: "gitops-config-repair", Status: remediation.JobDone,
		CreatedAt: t0.Add(48 * time.Hour), UpdatedAt: t0.Add(48*time.Hour + 5*time.Minute),
		Events: []remediation.Event{toolEvent("e6", "kubectl_apply", "bifrost-dev")},
	})

	// A still-running job in the prod scope: counted in totals/scope stats but
	// excluded from pattern clustering.
	store.Put(remediation.Job{
		ID: "job-running-1", Scope: "platform-self-health-recover", Status: remediation.JobRunning,
		CreatedAt: t0.Add(3 * time.Hour), UpdatedAt: t0.Add(3 * time.Hour),
	})

	report := NewAnalyzer(store).Analyze()

	if report.TotalJobs != 5 {
		t.Fatalf("TotalJobs = %d, want 5", report.TotalJobs)
	}
	if report.HealthScore != 60 {
		t.Fatalf("HealthScore = %v, want 60 (75 success - 15 critical-pattern penalty)", report.HealthScore)
	}
	if len(report.Patterns) != 2 {
		t.Fatalf("Patterns = %d, want 2", len(report.Patterns))
	}

	// Patterns are sorted by severity rank desc, then occurrences desc — the
	// prod/critical cluster must come first.
	prod := report.Patterns[0]
	if prod.Label != "platform-self-health-recover → bifrost-prod" {
		t.Fatalf("prod pattern label = %q", prod.Label)
	}
	if prod.Severity != SeverityCritical {
		t.Fatalf("prod pattern severity = %q, want critical", prod.Severity)
	}
	if prod.Occurrences != 2 {
		t.Fatalf("prod pattern occurrences = %d, want 2", prod.Occurrences)
	}
	if prod.RootCause != RootCausePlatformDefect {
		t.Fatalf("prod pattern root cause = %q, want platform_defect", prod.RootCause)
	}
	if prod.SuccessRate != 50 {
		t.Fatalf("prod pattern success rate = %v, want 50", prod.SuccessRate)
	}
	if prod.Trending != "stable" {
		t.Fatalf("prod pattern trending = %q, want stable (only 2 jobs)", prod.Trending)
	}

	dev := report.Patterns[1]
	if dev.Label != "gitops-config-repair → bifrost-dev" {
		t.Fatalf("dev pattern label = %q", dev.Label)
	}
	if dev.Severity != SeverityLow {
		t.Fatalf("dev pattern severity = %q, want low", dev.Severity)
	}
	if dev.RootCause != RootCauseConfigDrift {
		t.Fatalf("dev pattern root cause = %q, want config_drift", dev.RootCause)
	}
	if dev.SuccessRate != 100 {
		t.Fatalf("dev pattern success rate = %v, want 100", dev.SuccessRate)
	}

	// Scope stats.
	var prodScope, devScope *ScopeStats
	for i := range report.ScopeStats {
		switch report.ScopeStats[i].Scope {
		case "platform-self-health-recover":
			prodScope = &report.ScopeStats[i]
		case "gitops-config-repair":
			devScope = &report.ScopeStats[i]
		}
	}
	if prodScope == nil || devScope == nil {
		t.Fatalf("missing scope stats: %+v", report.ScopeStats)
	}
	if prodScope.Total != 3 || prodScope.Done != 1 || prodScope.Failed != 1 || prodScope.Running != 1 {
		t.Fatalf("prod scope stats = %+v", prodScope)
	}
	if prodScope.SuccessRate != 50 {
		t.Fatalf("prod scope success rate = %v, want 50", prodScope.SuccessRate)
	}
	if devScope.Total != 2 || devScope.Done != 2 || devScope.SuccessRate != 100 {
		t.Fatalf("dev scope stats = %+v", devScope)
	}

	// Tool usage: delete_pod used twice across 2 jobs is the clear top entry.
	if len(report.ToolUsage) == 0 || report.ToolUsage[0].Tool != "delete_pod" {
		t.Fatalf("tool usage top entry = %+v", report.ToolUsage)
	}
	if report.ToolUsage[0].Count != 2 || report.ToolUsage[0].Jobs != 2 {
		t.Fatalf("delete_pod usage = %+v", report.ToolUsage[0])
	}

	// Namespace activity: bifrost-prod has 3 tool calls (2 delete_pod + 1 kubectl_logs)
	// vs. bifrost-dev's 2, so it must rank first.
	if len(report.Namespaces) == 0 || report.Namespaces[0].Namespace != "bifrost-prod" {
		t.Fatalf("namespace activity top entry = %+v", report.Namespaces)
	}
	if report.Namespaces[0].ToolCalls != 3 || report.Namespaces[0].Jobs != 2 {
		t.Fatalf("bifrost-prod namespace activity = %+v", report.Namespaces[0])
	}

	if len(report.RootCauseDist) != 2 {
		t.Fatalf("RootCauseDist = %d entries, want 2", len(report.RootCauseDist))
	}

	joined := strings.Join(report.Insights, " | ")
	if !strings.Contains(joined, `Most recurring pattern: "platform-self-health-recover → bifrost-prod"`) {
		t.Fatalf("insights missing top-pattern line: %+v", report.Insights)
	}
	if !strings.Contains(joined, "Overall success rate: 75%") {
		t.Fatalf("insights missing success rate line: %+v", report.Insights)
	}
	if !strings.Contains(joined, "Most active namespace: bifrost-prod") {
		t.Fatalf("insights missing namespace line: %+v", report.Insights)
	}
	if !strings.Contains(joined, "Most used tool: delete_pod") {
		t.Fatalf("insights missing tool line: %+v", report.Insights)
	}
	if !strings.Contains(joined, "Root cause distribution:") {
		t.Fatalf("insights missing root cause distribution line: %+v", report.Insights)
	}
}

func TestClassifyRootCauseMultiSignalResourceLimitWinsOnOOM(t *testing.T) {
	ca := &clusterAccum{
		scope: "generic-scope", namespace: "ns", actions: map[string]int{},
		jobs:   []remediation.Job{{ID: "j1"}},
		errors: []string{"OOMKilled: container exceeded memory limit"},
	}
	cause, confidence, signals := classifyRootCauseMultiSignal(ca)
	if cause != RootCauseResourceLimit {
		t.Fatalf("cause = %q, want resource_limit", cause)
	}
	if confidence != 1.0 {
		t.Fatalf("confidence = %v, want 1.0 (single unambiguous signal)", confidence)
	}
	if len(signals) != 1 || signals[0].Name != "error_oom" {
		t.Fatalf("signals = %+v", signals)
	}
}

func TestClassifyRootCauseMultiSignalNoSignalsIsUnknown(t *testing.T) {
	ca := &clusterAccum{scope: "", namespace: "", actions: map[string]int{}, jobs: []remediation.Job{{ID: "j1"}}}
	cause, confidence, signals := classifyRootCauseMultiSignal(ca)
	if cause != RootCauseUnknown {
		t.Fatalf("cause = %q, want unknown", cause)
	}
	if confidence != 0.0 {
		t.Fatalf("confidence = %v, want 0.0", confidence)
	}
	if len(signals) != 0 {
		t.Fatalf("signals = %+v, want none", signals)
	}
}

func TestClassifySeverity(t *testing.T) {
	mk := func(namespace string, statuses ...remediation.JobStatus) *clusterAccum {
		jobs := make([]remediation.Job, 0, len(statuses))
		for i, st := range statuses {
			jobs = append(jobs, remediation.Job{ID: "j", Status: st, CreatedAt: time.Now().Add(time.Duration(i) * time.Hour)})
		}
		return &clusterAccum{namespace: namespace, jobs: jobs}
	}

	cases := []struct {
		name string
		ca   *clusterAccum
		want Severity
	}{
		{"prod_with_failure_is_critical", mk("bifrost-prod", remediation.JobFailed, remediation.JobDone), SeverityCritical},
		{"prod_no_failure_many_jobs_is_high", mk("bifrost-prod", remediation.JobDone, remediation.JobDone, remediation.JobDone), SeverityHigh},
		{"nonprod_with_failure_is_high", mk("bifrost-dev", remediation.JobFailed), SeverityHigh},
		{"nonprod_many_jobs_no_failure_is_medium", mk("bifrost-dev", remediation.JobDone, remediation.JobDone, remediation.JobDone, remediation.JobDone, remediation.JobDone), SeverityMedium},
		{"nonprod_few_jobs_no_failure_is_low", mk("bifrost-dev", remediation.JobDone, remediation.JobDone), SeverityLow},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifySeverity(tc.ca); got != tc.want {
				t.Fatalf("classifySeverity() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestComputeTrend(t *testing.T) {
	t0 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	mkJobs := func(times ...time.Duration) []remediation.Job {
		jobs := make([]remediation.Job, 0, len(times))
		for i, d := range times {
			jobs = append(jobs, remediation.Job{ID: "j", CreatedAt: t0.Add(d), UpdatedAt: t0.Add(d)})
			_ = i
		}
		return jobs
	}

	if got := computeTrend(mkJobs(0, time.Hour, 2*time.Hour)); got != "stable" {
		t.Fatalf("computeTrend(<4 jobs) = %q, want stable", got)
	}

	// First half spread over 10 days (low rate), second half within 1 hour (high rate) → up.
	up := mkJobs(0, 10*24*time.Hour, 20*24*time.Hour, 20*24*time.Hour+time.Hour)
	if got := computeTrend(up); got != "up" {
		t.Fatalf("computeTrend(burst) = %q, want up", got)
	}

	// First half tight (1h), second half spread over 10 days → down.
	down := mkJobs(0, time.Hour, 10*24*time.Hour, 10*24*time.Hour+10*24*time.Hour)
	if got := computeTrend(down); got != "down" {
		t.Fatalf("computeTrend(cooling) = %q, want down", got)
	}

	// Equal spacing in both halves → stable.
	stable := mkJobs(0, 10*time.Hour, 20*time.Hour, 30*time.Hour)
	if got := computeTrend(stable); got != "stable" {
		t.Fatalf("computeTrend(equal spacing) = %q, want stable", got)
	}
}

func TestExtractToolName(t *testing.T) {
	cases := []struct {
		name string
		ev   remediation.Event
		want string
	}{
		{"nil_meta", remediation.Event{}, ""},
		{"plain_name", remediation.Event{Meta: map[string]any{"name": "delete_pod"}}, "delete_pod"},
		{
			"mcp_wrapped", remediation.Event{Meta: map[string]any{
				"name": "mcp",
				"args": map[string]any{"toolName": "kubectl_get"},
			}}, "kubectl_get",
		},
		{"mcp_without_tool_name", remediation.Event{Meta: map[string]any{"name": "mcp"}}, "mcp"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractToolName(tc.ev); got != tc.want {
				t.Fatalf("extractToolName() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExtractNamespace(t *testing.T) {
	withNS := toolEvent("e1", "delete_pod", "bifrost-prod")
	if got := extractNamespace(withNS); got != "bifrost-prod" {
		t.Fatalf("extractNamespace() = %q, want bifrost-prod", got)
	}
	if got := extractNamespace(remediation.Event{}); got != "" {
		t.Fatalf("extractNamespace(nil meta) = %q, want empty", got)
	}
	if got := extractNamespace(remediation.Event{Meta: map[string]any{"args": map[string]any{}}}); got != "" {
		t.Fatalf("extractNamespace(no inner args) = %q, want empty", got)
	}
}

func TestExtractAllNamespacesDedupesAndSorts(t *testing.T) {
	job := remediation.Job{Events: []remediation.Event{
		toolEvent("e1", "delete_pod", "z-namespace"),
		toolEvent("e2", "delete_pod", "a-namespace"),
		toolEvent("e3", "delete_pod", "z-namespace"),
		errorEvent("e4", "not a tool call"),
	}}
	got := extractAllNamespaces(job)
	want := []string{"a-namespace", "z-namespace"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("extractAllNamespaces() = %+v, want %+v", got, want)
	}
}

func TestSanitizeKeyTruncateUniqueStrings(t *testing.T) {
	if got := sanitizeKey("scope: a/b c"); got != "scope--a-b-c" {
		t.Fatalf("sanitizeKey() = %q", got)
	}
	if got := truncate("hello", 10); got != "hello" {
		t.Fatalf("truncate(short) = %q", got)
	}
	if got := truncate("hello world", 5); got != "hello…" {
		t.Fatalf("truncate(long) = %q", got)
	}
	got := uniqueStrings([]string{"a", "b", "a", "c", "b"})
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("uniqueStrings() = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("uniqueStrings()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSeverityRank(t *testing.T) {
	if severityRank(SeverityCritical) <= severityRank(SeverityHigh) {
		t.Fatal("critical should outrank high")
	}
	if severityRank(SeverityHigh) <= severityRank(SeverityMedium) {
		t.Fatal("high should outrank medium")
	}
	if severityRank(SeverityMedium) <= severityRank(SeverityLow) {
		t.Fatal("medium should outrank low")
	}
	if severityRank(Severity("bogus")) != 0 {
		t.Fatal("unknown severity should rank 0")
	}
}

func TestHandlerReportCachesUntilForceRefresh(t *testing.T) {
	store := newTestJobStore(t)
	analyzer := NewAnalyzer(store)
	h := NewHandler(analyzer)

	first := h.getOrRefresh(false)
	store.Put(remediation.Job{ID: "job-new", Scope: "x", Status: remediation.JobDone, CreatedAt: time.Now(), UpdatedAt: time.Now()})
	cached := h.getOrRefresh(false)
	if cached.TotalJobs != first.TotalJobs {
		t.Fatalf("expected cached report to ignore new job: got %d want %d", cached.TotalJobs, first.TotalJobs)
	}
	refreshed := h.getOrRefresh(true)
	if refreshed.TotalJobs != first.TotalJobs+1 {
		t.Fatalf("expected forced refresh to pick up new job: got %d want %d", refreshed.TotalJobs, first.TotalJobs+1)
	}
}

func TestAggregateDefectReportsFromPlatformDefect(t *testing.T) {
	patterns := []PatternCluster{
		{
			ID: "pat-platform-self-health-recover-bifrost-prod",
			Label: "platform-self-health-recover → bifrost-prod",
			Description: "crash in api/internal/selfhealth/probe.go:42",
			RootCause: RootCausePlatformDefect,
			Confidence: 0.8,
			Severity: SeverityCritical,
			Occurrences: 3,
			Component: ComponentRef{Namespace: "bifrost-prod"},
			Jobs: []JobRef{
				{ID: "j1", Scope: "platform-self-health-recover", Status: "failed"},
				{ID: "j2", Scope: "platform-self-health-recover", Status: "done"},
			},
			Trending: "up",
		},
		{
			ID: "pat-gitops-config-repair-bifrost-dev",
			Label: "gitops-config-repair → bifrost-dev",
			Description: "config drift",
			RootCause: RootCauseConfigDrift,
			Confidence: 0.7,
			Severity: SeverityLow,
			Occurrences: 2,
			Component: ComponentRef{Namespace: "bifrost-dev"},
			Jobs: []JobRef{{ID: "j3", Scope: "gitops-config-repair", Status: "done"}},
		},
	}

	defs := aggregateDefectReports(patterns)
	if len(defs) != 1 {
		t.Fatalf("defects = %d, want 1 (only platform_defect)", len(defs))
	}
	d := defs[0]
	if d.ID != "def-platform-self-health-recover-bifrost-prod" {
		t.Fatalf("defect id = %q", d.ID)
	}
	if len(d.PatternIDs) != 1 || d.PatternIDs[0] != patterns[0].ID {
		t.Fatalf("pattern_ids = %+v", d.PatternIDs)
	}
	if len(d.Attributions) == 0 {
		t.Fatal("expected attributions")
	}
	foundSelfHealth := false
	foundLine := false
	for _, a := range d.Attributions {
		if strings.Contains(a.File, "selfhealth") {
			foundSelfHealth = true
		}
		if a.LineRange == "42" || strings.HasPrefix(a.LineRange, "42") {
			foundLine = true
		}
	}
	if !foundSelfHealth {
		t.Fatalf("expected selfhealth attribution, got %+v", d.Attributions)
	}
	if !foundLine {
		t.Fatalf("expected line 42 from text, got %+v", d.Attributions)
	}
	if d.SuggestedFix == "" {
		t.Fatal("expected suggested_fix")
	}
}

func TestAnalyzeIncludesDefectsForPlatformPatterns(t *testing.T) {
	store := newTestJobStore(t)
	t0 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	store.Put(remediation.Job{
		ID: "job-prod-1", Scope: "platform-self-health-recover", Status: remediation.JobFailed,
		CreatedAt: t0, UpdatedAt: t0.Add(5 * time.Minute),
		Events: []remediation.Event{
			toolEvent("e1", "delete_pod", "bifrost-prod"),
			errorEvent("e2", "CrashLoopBackOff in platform-api"),
		},
	})
	store.Put(remediation.Job{
		ID: "job-prod-2", Scope: "platform-self-health-recover", Status: remediation.JobDone,
		CreatedAt: t0.Add(2 * time.Hour), UpdatedAt: t0.Add(2*time.Hour + 10*time.Minute),
		InitBrief: "CrashLoopBackOff detected in bifrost-prod",
		Events: []remediation.Event{
			toolEvent("e3", "delete_pod", "bifrost-prod"),
		},
	})

	report := NewAnalyzer(store).Analyze()
	if len(report.Defects) == 0 {
		t.Fatalf("expected defects for platform_defect pattern, patterns=%+v", report.Patterns)
	}
	if report.Defects[0].Attributions == nil || len(report.Defects[0].Attributions) == 0 {
		t.Fatal("defect missing attributions")
	}
}
