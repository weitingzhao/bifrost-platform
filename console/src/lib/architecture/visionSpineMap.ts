/**
 * Vision V1–V5 ↔ spine milestone map — single source for the Vision page archive
 * and the spine catalog CI check (scripts/ci/check_spine_catalog.sh).
 */

export const VISION_SPINE_MAP_VERSION = '2026-06-19'
export const VISION_SPINE_MAP_SOURCE = 'console/src/lib/architecture/visionSpineMap.ts'

export type VisionSpineEntry = {
  visionId: string
  title: string
  spineMilestoneId: string
  spineLabel: string
  hook: string
  nextAfter?: string
}

/** Maps convergence milestones (V1–V5) to ops-context.yaml milestone IDs. */
export const VISION_SPINE_MAP: VisionSpineEntry[] = [
  {
    visionId: 'V1',
    title: 'Dev inner-loop on K3s',
    spineMilestoneId: 'vision-v1-dev-topology',
    spineLabel: 'Vision V1 — Dev inner-loop on K3s',
    hook: 'Mac thin + K3s bifrost-dev :30882 · .env.development.k3s',
    nextAfter: 'k3s-stg-v2-deliver',
  },
  {
    visionId: 'V2',
    title: 'Dev Agent closed-loop',
    spineMilestoneId: 'vision-v2-dev-agent',
    spineLabel: 'Vision V2 — Dev Agent closed-loop',
    hook: 'push → Tekton → STG deliver → verify → report',
    nextAfter: 'vision-v1-dev-topology',
  },
  {
    visionId: 'V3',
    title: 'Ops Agent L1/L2',
    spineMilestoneId: 'vision-v3-ops-agent',
    spineLabel: 'Vision V3 — Ops Agent L1/L2',
    hook: 'data NS CNPG + redis-live/queue · MCP PG/Redis · AlertManager → Ops Agent',
    nextAfter: 'vision-v2-dev-agent',
  },
  {
    visionId: 'V4',
    title: 'Business Agent read-only',
    spineMilestoneId: 'vision-v4-business-agent',
    spineLabel: 'Vision V4 — Business Agent read-only',
    hook: 'mcp-trade-api + scheduled daily brief via SDK',
    nextAfter: 'vision-v3-ops-agent',
  },
  {
    visionId: 'V5',
    title: 'Full convergence',
    spineMilestoneId: 'vision-v5-convergence',
    spineLabel: 'Vision V5 — Full convergence',
    hook: 'Single Cursor window: code + deploy + ops + trade intelligence',
    nextAfter: 'vision-v4-business-agent',
  },
]

export function visionSpineEntryById(visionId: string): VisionSpineEntry | undefined {
  return VISION_SPINE_MAP.find(e => e.visionId === visionId)
}
