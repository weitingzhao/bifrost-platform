/**
 * Catalog ↔ spine parity — detect hardcoded progress in Architecture catalogs
 * that diverges from live spine (GET /api/v1/context).
 *
 * Governance Phase 4 / briefingReconciliationCatalog gate-catalog-spine-parity.
 */

import type { OpsContextResponse } from '@/api/types'
import {
  LEGACY_RETIREMENT_MILESTONE_ID,
  MAINLINE_PHASE_DEFINITIONS,
} from '@/lib/architecture/deployMainlineCatalog'
import {
  PROD_CUTOVER_MILESTONE_ID,
  STG_DELIVER_MILESTONE_ID,
} from '@/lib/architecture/spineProjection'
import { VISION_SPINE_MAP } from '@/lib/architecture/visionSpineMap'

export const CATALOG_SPINE_PARITY_VERSION = '2026-07-01'

export type CatalogSpineParityFinding = {
  ruleId: 'gate-catalog-spine-parity' | 'gate-catalog-milestone-refs'
  severity: 'warning'
  message: string
}

/** Deploy Mainline rows bound to spine milestones (Constitution catalog — should not embed live status). */
export const DEPLOY_MAINLINE_MILESTONE_BINDINGS = [
  { seq: 4, milestoneId: STG_DELIVER_MILESTONE_ID, label: 'K3s STG v2 deliver' },
  { seq: 5, milestoneId: PROD_CUTOVER_MILESTONE_ID, label: 'K3s Prod overlay + deliver-prod' },
  { seq: 7, milestoneId: LEGACY_RETIREMENT_MILESTONE_ID, label: 'Phase 3 Legacy retirement' },
] as const

/** Milestone ids referenced by Architecture / Delivery catalogs — must exist on spine. */
export function architectureCatalogMilestoneRefs(): string[] {
  const ids = new Set<string>([
    STG_DELIVER_MILESTONE_ID,
    PROD_CUTOVER_MILESTONE_ID,
    LEGACY_RETIREMENT_MILESTONE_ID,
    ...DEPLOY_MAINLINE_MILESTONE_BINDINGS.map(b => b.milestoneId),
    ...VISION_SPINE_MAP.map(v => v.spineMilestoneId),
  ])
  return [...ids].sort()
}

const LEGACY_PROGRESS_PATTERN =
  /\bIN_PROGRESS\b|IN PROGRESS|\bACTIVE\s*[—-]|\bCLOSED\b|\bSIGNED\b/i

/** Spine-bound catalog rows must not carry historicalNote or other progress prose. */
export function checkDeployMainlineCatalogConstitution(): CatalogSpineParityFinding[] {
  const findings: CatalogSpineParityFinding[] = []
  for (const binding of DEPLOY_MAINLINE_MILESTONE_BINDINGS) {
    const def = MAINLINE_PHASE_DEFINITIONS.find(p => p.seq === binding.seq)
    if (def == null) continue
    if (def.spineMilestoneId !== binding.milestoneId) {
      findings.push({
        ruleId: 'gate-catalog-spine-parity',
        severity: 'warning',
        message: `CATALOG_DRIFT — deployMainline seq ${binding.seq}: spineMilestoneId mismatch (expected \`${binding.milestoneId}\`)`,
      })
      continue
    }
    if (def.historicalNote != null && LEGACY_PROGRESS_PATTERN.test(def.historicalNote)) {
      findings.push({
        ruleId: 'gate-catalog-spine-parity',
        severity: 'warning',
        message: `CATALOG_DRIFT — deployMainline seq ${binding.seq} (${binding.milestoneId}): catalog still embeds progress prose — use Projection only`,
      })
    }
  }
  return findings
}

/** Ensure catalog-referenced milestone ids exist on live spine. */
export function checkCatalogMilestoneRefs(ctx: OpsContextResponse): CatalogSpineParityFinding[] {
  const spineIds = new Set(ctx.milestones.map(m => m.id))
  const findings: CatalogSpineParityFinding[] = []

  for (const ref of architectureCatalogMilestoneRefs()) {
    if (!spineIds.has(ref)) {
      findings.push({
        ruleId: 'gate-catalog-milestone-refs',
        severity: 'warning',
        message: `CATALOG_DRIFT — Architecture catalog references milestone \`${ref}\` missing from spine milestones`,
      })
    }
  }

  return findings
}

export function reconcileCatalogSpineParity(ctx?: OpsContextResponse): CatalogSpineParityFinding[] {
  if (ctx == null) return checkDeployMainlineCatalogConstitution()
  return [...checkDeployMainlineCatalogConstitution(), ...checkCatalogMilestoneRefs(ctx)]
}

export function hasCatalogDriftFindings(
  findings: Array<{ ruleId: string }>,
): boolean {
  return findings.some(
    f => f.ruleId === 'gate-catalog-spine-parity' || f.ruleId === 'gate-catalog-milestone-refs',
  )
}
