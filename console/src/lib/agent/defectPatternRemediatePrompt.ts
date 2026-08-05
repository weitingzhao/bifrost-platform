import type { RetrospectivePatternCluster } from '@/api/agentTypes'
import { DEFECT_PATTERN_REMEDIATE_SCOPE } from '@/lib/agent/agentScopes'

function inferTrack(p: RetrospectivePatternCluster): string {
  const label = p.label.toLowerCase()
  if (label.includes('deliver') || label.includes('pipeline') || label.includes('release')) return 'playbook'
  if (label.includes('crash') || label.includes('config') || label.includes('nginx')) return 'product'
  if (label.includes('node') || label.includes('postgres') || label.includes('redis')) return 'infra'
  return 'agent-adhoc'
}

export function buildDefectPatternRemediatePrompt(pattern: RetrospectivePatternCluster): string {
  const track = inferTrack(pattern)
  const topTools = (pattern.top_actions ?? []).slice(0, 5).map(a => `${a.tool}×${a.count}`).join(', ')
  const isPlatformDefect = pattern.root_cause === 'platform_defect'

  return [
    `Scope: ${DEFECT_PATTERN_REMEDIATE_SCOPE}`,
    '',
    '## Defects pattern',
    `- id: ${pattern.id}`,
    `- label: ${pattern.label}`,
    `- root_cause: ${pattern.root_cause}`,
    `- occurrences: ${pattern.occurrences}`,
    `- success_rate: ${(pattern.success_rate ?? 0).toFixed(0)}%`,
    `- inferred_track: ${track}`,
    `- top_tools: ${topTools || 'none'}`,
    '',
    '## Code attribution',
    'Call GET /api/v1/agent/retrospective/defects (or report.defects) and match pattern id.',
    'Use attributions[].file / line_range / confidence when drafting a fix-PR proposal.',
    '',
    '## Routing',
    isPlatformDefect
      ? '→ Fix-PR proposal path (dry-run): draft PR title/body + files from DefectReport attributions; request_operator_approval before git_commit / gh pr create. No push without Owner approval.'
      : track === 'playbook' || pattern.label.toLowerCase().includes('release')
        ? '→ Execute deliver-stg-recover workflow (get_delivery_run_logs, fix pipeline, re-run bifrost-deliver-stg).'
        : track === 'product'
          ? '→ Consider spawn_trade_release_fix or release-fix for structural repo fix.'
          : pattern.root_cause === 'transient'
            ? '→ READ ONLY: investigate if trending; no destructive cluster actions unless live issues confirmed.'
            : '→ cluster_issues_full_auto if open pod/node issues; else report findings.',
    '',
    'verify_mission_snapshot before closing.',
  ].join('\n')
}
