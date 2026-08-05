package retrospective

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// pathInText finds bifrost-platform-ish source paths in free text.
var pathInText = regexp.MustCompile(`(?:^|[\s"'` + "`" + `(])((?:api/internal|console/src|agent/remediation|config/programs|config/)[A-Za-z0-9_./-]+\.(?:go|ts|tsx|yaml|yml))`)

// lineHintInText captures "file.go:123" or "file.go#L10-L20" style hints.
var lineHintInText = regexp.MustCompile(`([A-Za-z0-9_./-]+\.(?:go|ts|tsx)):(\d+)(?:-(\d+))?`)

// scopeCodeMap maps remediation scopes to likely platform source files.
// Heuristic only — confidence is capped; Agents should verify before fix-PR.
var scopeCodeMap = map[string][]CodeAttribution{
	"platform-self-health-recover": {
		{File: "api/internal/selfhealth/", Evidence: "scope platform-self-health-recover → self-health probes", Confidence: 0.55},
		{File: "api/internal/server/server.go", LineRange: "health routes", Evidence: "control-plane health route registration", Confidence: 0.4},
	},
	"gitops-config-repair": {
		{File: "api/internal/gitops/", Evidence: "scope gitops-config-repair → GitOps handlers", Confidence: 0.55},
		{File: "config/", Evidence: "GitOps/config drift often in config/*.yaml", Confidence: 0.45},
	},
	"registry-pull-recover": {
		{File: "api/internal/delivery/", Evidence: "scope registry-pull-recover → delivery/registry path", Confidence: 0.5},
		{File: "k8s/", Evidence: "imagePull failures often overlay/image refs", Confidence: 0.4},
	},
	"defect-pattern-remediate": {
		{File: "api/internal/retrospective/analyzer.go", Evidence: "pattern remediation routed from retrospective analyzer", Confidence: 0.5},
		{File: "agent/remediation/src/scopedPrompts.ts", Evidence: "defect-pattern-remediate runner prompt", Confidence: 0.55},
		{File: "console/src/pages/DefectsPage.tsx", Evidence: "Defects Fix dispatch UI", Confidence: 0.45},
	},
	"deliver-stg-recover": {
		{File: "api/internal/delivery/", Evidence: "scope deliver-stg-recover → Tekton/delivery APIs", Confidence: 0.55},
		{File: "console/src/lib/architecture/", Evidence: "delivery catalogs / STG recover UX", Confidence: 0.35},
	},
	"trade-release-fix": {
		{File: "console/src/lib/architecture/tradeIbClientMigrationRolloutCatalog.ts", Evidence: "trade release catalogs", Confidence: 0.4},
		{File: "agent/remediation/src/scopedPrompts.ts", Evidence: "trade-release-fix prompt path", Confidence: 0.45},
	},
	"trade-deploy": {
		{File: "api/internal/delivery/", Evidence: "trade-deploy delivery actuation", Confidence: 0.45},
	},
	"release": {
		{File: "api/internal/promote/", Evidence: "release/promote gate path", Confidence: 0.45},
		{File: "api/internal/release/", Evidence: "release state handlers", Confidence: 0.4},
	},
	"release-fix": {
		{File: "api/internal/promote/", Evidence: "release-fix promote path", Confidence: 0.4},
		{File: "agent/remediation/src/scopedPrompts.ts", Evidence: "release-fix scoped prompt", Confidence: 0.45},
	},
	"nightly-drift-autofix": {
		{File: "agent/remediation/src/", Evidence: "nightly drift autofix runner scopes", Confidence: 0.5},
		{File: "config/ops-context.yaml", Evidence: "spine/catalog drift sources", Confidence: 0.4},
	},
	"operator-plane-remediate": {
		{File: "console/src/pages/", Evidence: "Operator Plane console surfaces", Confidence: 0.4},
		{File: "api/internal/agentbridge/", Evidence: "agent bridge / operator plane APIs", Confidence: 0.4},
	},
	"data-layer-recover": {
		{File: "api/internal/mission/", Evidence: "data-layer verify_payload / mission probes", Confidence: 0.5},
	},
	"massive-feed-recover": {
		{File: "api/internal/mission/", Evidence: "massive/polygon matrix probes", Confidence: 0.45},
	},
	"stale-pipeline-triage": {
		{File: "api/internal/delivery/", Evidence: "pipeline run triage", Confidence: 0.5},
	},
}

// aggregateDefectReports builds DefectReports from platform_defect (and high-confidence
// structural) pattern clusters, attaching heuristic code attributions.
func aggregateDefectReports(patterns []PatternCluster) []DefectReport {
	out := make([]DefectReport, 0)
	buildSHA := strings.TrimSpace(os.Getenv("PLATFORM_BUILD_SHA"))
	if buildSHA == "" {
		buildSHA = strings.TrimSpace(os.Getenv("GIT_COMMIT"))
	}

	for _, p := range patterns {
		if !shouldEmitDefect(p) {
			continue
		}
		attrs := attributionsForPattern(p, buildSHA)
		if len(attrs) == 0 {
			attrs = []CodeAttribution{{
				File:       "api/internal/retrospective/analyzer.go",
				Evidence:   "fallback — platform_defect without mapped scope; start at classifier",
				Confidence: 0.25,
			}}
		}
		dr := DefectReport{
			ID:           "def-" + strings.TrimPrefix(p.ID, "pat-"),
			Title:        p.Label,
			Description:  p.Description,
			PatternIDs:   []string{p.ID},
			RootCause:    p.RootCause,
			Confidence:   p.Confidence,
			Severity:     p.Severity,
			Occurrences:  p.Occurrences,
			Component:    p.Component,
			Attributions: attrs,
			SuggestedFix: suggestFix(p),
			Trending:     p.Trending,
		}
		out = append(out, dr)
	}

	sort.Slice(out, func(i, j int) bool {
		si := severityRank(out[i].Severity)
		sj := severityRank(out[j].Severity)
		if si != sj {
			return si > sj
		}
		if out[i].Confidence != out[j].Confidence {
			return out[i].Confidence > out[j].Confidence
		}
		return out[i].Occurrences > out[j].Occurrences
	})
	return out
}

func shouldEmitDefect(p PatternCluster) bool {
	if p.RootCause == RootCausePlatformDefect {
		return true
	}
	// High-confidence probe_drift can be a sensor (platform) bug worth tracking.
	if p.RootCause == RootCauseProbeDrift && p.Confidence >= 0.65 && p.Occurrences >= 2 {
		return true
	}
	return false
}

func attributionsForPattern(p PatternCluster, buildSHA string) []CodeAttribution {
	seen := map[string]bool{}
	var out []CodeAttribution

	add := func(a CodeAttribution) {
		key := a.File + "|" + a.LineRange
		if a.File == "" || seen[key] {
			return
		}
		seen[key] = true
		if a.CommitSHA == "" && buildSHA != "" {
			a.CommitSHA = buildSHA
		}
		if a.Confidence > 1 {
			a.Confidence = 1
		}
		out = append(out, a)
	}

	// 1) Paths / line hints from pattern description + signal details + job scopes context
	texts := []string{p.Description, p.Label}
	for _, s := range p.Signals {
		if s.Detail != "" {
			texts = append(texts, s.Detail)
		}
	}
	blob := strings.Join(texts, "\n")
	for _, m := range pathInText.FindAllStringSubmatch(blob, -1) {
		if len(m) < 2 {
			continue
		}
		add(CodeAttribution{
			File:       m[1],
			Evidence:   "path extracted from pattern/signal text",
			Confidence: 0.7,
		})
	}
	for _, m := range lineHintInText.FindAllStringSubmatch(blob, -1) {
		if len(m) < 3 {
			continue
		}
		file := m[1]
		// Prefer repo-relative if we already saw a full path ending with this base.
		lineRange := m[2]
		if m[3] != "" {
			lineRange = m[2] + "-" + m[3]
		}
		add(CodeAttribution{
			File:       file,
			LineRange:  lineRange,
			Evidence:   "line hint extracted from text",
			Confidence: 0.65,
		})
	}

	// 2) Scope → known code map (dominant job scope)
	scope := dominantScope(p)
	if mapped, ok := scopeCodeMap[scope]; ok {
		for _, a := range mapped {
			add(a)
		}
	} else if scope != "" && scope != "unknown" {
		// Partial match on scope key
		for key, mapped := range scopeCodeMap {
			if strings.Contains(strings.ToLower(scope), key) || strings.Contains(key, strings.ToLower(scope)) {
				for _, a := range mapped {
					add(a)
				}
				break
			}
		}
	}

	// 3) Namespace hint
	ns := p.Component.Namespace
	switch ns {
	case "bifrost-platform-prod", "bifrost-platform-stg", "bifrost-platform-dev":
		add(CodeAttribution{
			File:       "api/",
			Evidence:   fmt.Sprintf("namespace %s → platform control plane", ns),
			Confidence: 0.35,
		})
	case "bifrost-prod", "bifrost-stg", "bifrost-dev":
		add(CodeAttribution{
			File:       "k8s/overlays/",
			Evidence:   fmt.Sprintf("namespace %s → trade overlay manifests", ns),
			Confidence: 0.35,
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Confidence > out[j].Confidence })
	if len(out) > 6 {
		out = out[:6]
	}
	return out
}

func dominantScope(p PatternCluster) string {
	counts := map[string]int{}
	for _, j := range p.Jobs {
		s := j.Scope
		if s == "" {
			s = "unknown"
		}
		counts[s]++
	}
	best := ""
	bestN := 0
	for s, n := range counts {
		if n > bestN {
			best = s
			bestN = n
		}
	}
	if best == "" {
		// Fall back to label prefix before " → "
		if i := strings.Index(p.Label, " → "); i > 0 {
			return strings.TrimSpace(p.Label[:i])
		}
		return strings.TrimSpace(p.Label)
	}
	return best
}

func suggestFix(p PatternCluster) string {
	scope := dominantScope(p)
	switch {
	case p.RootCause == RootCauseProbeDrift:
		return "Review matrix probe host vs in-cluster DNS; fix sensor false-negative before cluster actuation."
	case scope == "gitops-config-repair" || strings.Contains(scope, "gitops"):
		return "Propose config/GitOps patch PR (dry-run); request_operator_approval before commit/push."
	case scope == "registry-pull-recover":
		return "Check image tags/pull secrets; propose overlay or registry fix PR if code/config."
	case scope == "platform-self-health-recover":
		return "Inspect self-health probe + Deployment; prefer rollout with approval, then structural PR if recurring."
	case p.RootCause == RootCausePlatformDefect:
		return "Draft fix-PR from attributions (dry-run); Owner approval before git_commit / gh pr create."
	default:
		return "Investigate pattern; prefer read-only report unless live failure confirmed."
	}
}
