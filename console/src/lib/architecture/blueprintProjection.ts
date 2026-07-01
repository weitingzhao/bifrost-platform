/**
 * Blueprint Projection layer — live API capability and actuation progress.
 * Constitution definitions live in blueprintCatalog.ts; this module holds
 * pointers and builders that consume platform-api at render/copy time.
 */

import type { McpToolsResponse, McpToolView } from '@/api/types'
import { ACTUATION_PHASES } from './blueprintCatalog'

export const PROJECTION_AUTHORITY = {
  apiCatalog: 'GET /api/v1/mcp/tools',
  apiSource: 'api/internal/mcp/catalog.go',
  configNote:
    'config/*.yaml mounted to platform-api — probe targets (environments), Spine (ops-context), topology, clusters. See Architecture → Environments.',
} as const

export type ActuationPhaseProgress = {
  phase: string
  implemented: number
  total: number
}

/** Group MCP tools by phase label (P0–P5, Agent, …). */
export function actuationPhaseProgress(tools: McpToolView[]): ActuationPhaseProgress[] {
  const byPhase = new Map<string, { implemented: number; total: number }>()
  for (const t of tools) {
    const phase = t.phase?.trim() !== '' ? t.phase! : 'Other'
    const cur = byPhase.get(phase) ?? { implemented: 0, total: 0 }
    cur.total += 1
    if (t.implemented) cur.implemented += 1
    byPhase.set(phase, cur)
  }
  const order = ['P0', 'P1', 'P2', 'P3', 'P4', 'P5', 'P6', 'Agent', 'Other']
  return [...byPhase.entries()]
    .map(([phase, counts]) => ({ phase, ...counts }))
    .sort((a, b) => {
      const ai = order.indexOf(a.phase)
      const bi = order.indexOf(b.phase)
      return (ai === -1 ? 99 : ai) - (bi === -1 ? 99 : bi)
    })
}

/** P0–P5 constitution phases with live MCP implemented counts when tools are loaded. */
export function constitutionActuationWithProgress(
  tools: McpToolView[] | undefined,
): Array<{ phase: string; deliverables: string; eliminates: string; progress: string | null }> {
  const progressMap = tools != null ? new Map(actuationPhaseProgress(tools).map(p => [p.phase, p])) : null
  return ACTUATION_PHASES.map(row => {
    const key = row.phase
    const live = progressMap?.get(key)
    const progress =
      live != null && live.total > 0 ? `${live.implemented}/${live.total} MCP tools implemented` : null
    return { ...row, progress }
  })
}

export function buildBlueprintProjectionPack(tools?: McpToolsResponse): string {
  const lines: string[] = [
    '## Projection (live capability — fast-changing)',
    '',
    'Do not treat Constitution pack as API inventory. Authoritative endpoints:',
    `- ${PROJECTION_AUTHORITY.apiCatalog}`,
    `- Source: ${PROJECTION_AUTHORITY.apiSource}`,
    '',
    PROJECTION_AUTHORITY.configNote,
    '',
  ]

  if (tools != null) {
    lines.push(
      `### MCP tool catalog (${tools.implemented_count}/${tools.tools.length} implemented)`,
      `Generated: ${tools.generated_at}`,
      '',
    )
    for (const t of tools.tools) {
      const route = t.route != null && t.route !== '' ? ` ${t.method ?? 'GET'} ${t.route}` : ''
      lines.push(
        `- **${t.name}** [${t.level}] phase=${t.phase ?? '—'} implemented=${t.implemented}${route}`,
      )
    }
    lines.push('', '### Actuation phase progress (from MCP phase labels)')
    for (const p of actuationPhaseProgress(tools.tools)) {
      lines.push(`- **${p.phase}**: ${p.implemented}/${p.total} tools implemented`)
    }
  } else {
    lines.push(
      '### Actuation phase progress',
      'Fetch GET /api/v1/mcp/tools and group by phase field for live P0–P5 progress.',
    )
  }

  return lines.join('\n')
}
