export function llmSection(title: string, lines: string[]): string[] {
  return ['', `## ${title}`, '', ...lines]
}

export function llmTableRow(cells: string[]): string {
  return `| ${cells.join(' | ')} |`
}

export function clusterProbeHeaderLines(
  clusterLabel?: string,
  clusterId?: string,
  generatedAt?: string,
): string[] {
  const lines = [
    'Mode: Ops',
    '',
    '## K3s cluster category snapshot',
    'Source: Ops Console → Operate → Cluster → category Copy for LLM',
  ]
  if (clusterLabel != null && clusterLabel !== '') {
    lines.push(`Cluster: ${clusterLabel}${clusterId != null && clusterId !== '' ? ` (${clusterId})` : ''}`)
  }
  if (generatedAt != null && generatedAt !== '') {
    lines.push(`Probe time: ${generatedAt}`)
  }
  return lines
}

export function gapAnalysisGuidance(categoryLabel: string): string[] {
  return llmSection('Gap analysis (Agent)', [
    `Compare this **${categoryLabel}** snapshot against the owner's target requirements.`,
    '',
    'Output format:',
    '1. **Current state** — one paragraph from live probes above.',
    '2. **Target state** — from planned architecture sections (if included).',
    '3. **Gaps** — table: Requirement | Current | Target | Severity (blocker / degraded / ok).',
    '4. **Next actions** — ordered, single-variable steps; prefer Ops Console actuation over ad-hoc kubectl.',
    '',
    'Cross-reference (static catalogs):',
    '- Architecture → Data Layer — PG/Redis target topology',
    '- Architecture → K3s Architecture — node pools & CI/CD',
    '- Architecture → Environments — hardware & env matrix',
    '- Architecture → Milestones — spine progress (k3s-data-layer, etc.)',
    '',
    'Constraints:',
    '- Mode Ops — cluster/infrastructure only; bifrost-trader-engine/ is read-only reference.',
    '- Do not edit bifrost-trade-frontend unless explicitly cross-linked.',
  ])
}
