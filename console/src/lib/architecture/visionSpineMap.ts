/**
 * Vision V1–V5 ↔ spine milestone map — single source for Briefing, Control Room, and S3 gate.
 *
 * Authoritative for Ops Console → Architecture → Vision (Briefing alignment section)
 * and Agent Briefing vision milestone appendix.
 */

import type { OpsContextResponse } from '@/api/types'
import { VISION_MILESTONES } from '@/lib/architecture/dualFlywheelVisionCatalog'

export const VISION_SPINE_MAP_VERSION = '2026-06-19'
export const VISION_SPINE_MAP_SOURCE = 'console/src/lib/architecture/visionSpineMap.ts'

export type VisionSpineEntry = {
  visionId: string
  title: string
  spineMilestoneId: string
  spineLabel: string
  briefingHook: string
  nextAfter?: string
}

/** Maps convergence milestones (V1–V5) to ops-context.yaml milestone IDs. */
export const VISION_SPINE_MAP: VisionSpineEntry[] = [
  {
    visionId: 'V1',
    title: 'Dev inner-loop on K3s',
    spineMilestoneId: 'vision-v1-dev-topology',
    spineLabel: 'Vision V1 — Dev inner-loop on K3s',
    briefingHook: 'Mac thin + K3s bifrost-dev :30882 · .env.development.k3s',
    nextAfter: 'k3s-stg-v2-deliver',
  },
  {
    visionId: 'V2',
    title: 'Dev Agent closed-loop',
    spineMilestoneId: 'vision-v2-dev-agent',
    spineLabel: 'Vision V2 — Dev Agent closed-loop',
    briefingHook: 'push → Tekton → STG deliver → verify → report',
    nextAfter: 'vision-v1-dev-topology',
  },
  {
    visionId: 'V3',
    title: 'Ops Agent L1/L2',
    spineMilestoneId: 'vision-v3-ops-agent',
    spineLabel: 'Vision V3 — Ops Agent L1/L2',
    briefingHook: 'MCP K8s/Redis/PG + AlertManager webhook → Agent diagnosis',
    nextAfter: 'vision-v2-dev-agent',
  },
  {
    visionId: 'V4',
    title: 'Business Agent read-only',
    spineMilestoneId: 'vision-v4-business-agent',
    spineLabel: 'Vision V4 — Business Agent read-only',
    briefingHook: 'mcp-trade-api + scheduled daily brief via SDK',
    nextAfter: 'vision-v3-ops-agent',
  },
  {
    visionId: 'V5',
    title: 'Full convergence',
    spineMilestoneId: 'vision-v5-convergence',
    spineLabel: 'Vision V5 — Full convergence',
    briefingHook: 'Single Cursor window: code + deploy + ops + trade intelligence',
    nextAfter: 'vision-v4-business-agent',
  },
]

export function visionSpineEntryById(visionId: string): VisionSpineEntry | undefined {
  return VISION_SPINE_MAP.find(e => e.visionId === visionId)
}

export function resolveVisionSpineStatus(
  entry: VisionSpineEntry,
  context: OpsContextResponse | undefined,
): string {
  const m = context?.milestones.find(ms => ms.id === entry.spineMilestoneId)
  return m?.status ?? 'NOT_STARTED'
}

/** Briefing appendix: Vision milestones with live spine status. */
export function formatVisionBriefingSection(context: OpsContextResponse | undefined): string {
  const lines = [
    '## Vision convergence (V1–V5 ↔ spine)',
    '',
    `Source: ${VISION_SPINE_MAP_SOURCE} v${VISION_SPINE_MAP_VERSION}`,
    '',
    '| Vision | Title | Spine milestone | Status | Briefing hook |',
    '|--------|-------|-----------------|--------|---------------|',
  ]
  for (const entry of VISION_SPINE_MAP) {
    const status = resolveVisionSpineStatus(entry, context)
    lines.push(
      `| ${entry.visionId} | ${entry.title} | \`${entry.spineMilestoneId}\` | ${status} | ${entry.briefingHook} |`,
    )
  }
  lines.push('')
  lines.push('Catalog deliverables (dualFlywheelVisionCatalog.ts):')
  for (const m of VISION_MILESTONES) {
    lines.push(`- **${m.id}** ${m.title}: ${m.unlocks}`)
  }
  return lines.join('\n')
}

/** Governance lane queue items for active Vision milestones. */
export function visionGovernanceQueueItems(context: OpsContextResponse | undefined): {
  id: string
  label: string
  status: 'done' | 'in_progress' | 'pending' | 'blocked'
  note?: string
}[] {
  return VISION_SPINE_MAP.map(entry => {
    const spineStatus = resolveVisionSpineStatus(entry, context)
    let status: 'done' | 'in_progress' | 'pending' | 'blocked' = 'pending'
    if (spineStatus === 'SIGNED' || spineStatus === 'CLOSED') status = 'done'
    else if (spineStatus === 'IN_PROGRESS') status = 'in_progress'
    else if (spineStatus === 'BLOCKED_ON') status = 'blocked'
    return {
      id: `vision-${entry.visionId.toLowerCase()}`,
      label: `${entry.visionId}: ${entry.title}`,
      status,
      note: entry.briefingHook,
    }
  })
}
