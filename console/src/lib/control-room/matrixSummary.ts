import type { MatrixResponse, OpsContextResponse, Reachability, Target } from '@/api/types'
import { getBay, type BayDefinition } from '@/lib/control-room/bayRegistry'

export type BayLamp = Reachability | 'unknown'

export type MatrixSummary = {
  ok: number
  fail: number
  degraded: number
  unknown: number
  total: number
  worstReach: BayLamp
}

export type PromoteStatus = {
  ready: boolean
  blockedByDecision: boolean
  prodFails: boolean
  prodOk: boolean
  gateDone: boolean
  gatePass: boolean
  cutoverBlocker: string | null
  reasons: string[]
}

export function summarizeMatrix(m: MatrixResponse): MatrixSummary {
  let ok = 0
  let fail = 0
  let degraded = 0
  let unknown = 0
  for (const t of m.targets) {
    if (t.reachability === 'ok') ok += 1
    else if (t.reachability === 'fail') fail += 1
    else if (t.reachability === 'degraded') degraded += 1
    else unknown += 1
  }
  const worstReach: BayLamp =
    fail > 0 ? 'fail' : degraded > 0 ? 'degraded' : ok > 0 ? 'ok' : 'unknown'
  return { ok, fail, degraded, unknown, total: m.targets.length, worstReach }
}

export function getProdMatrix(matrices: MatrixResponse[]): MatrixResponse | undefined {
  return matrices.find(m => m.environment === 'prod')
}

export function prodMatrixHealthy(matrices: MatrixResponse[]): boolean {
  const prod = getProdMatrix(matrices)
  if (!prod) return false
  return prod.targets.every(
    t =>
      t.category === 'trade_write' ||
      t.id === 'redis' ||
      t.reachability === 'ok' ||
      t.reachability === 'degraded' ||
      t.reachability === 'unknown',
  )
}

export function hasProdFailures(matrices: MatrixResponse[]): boolean {
  const prod = getProdMatrix(matrices)
  if (!prod) return true
  return prod.targets.some(
    t => t.reachability === 'fail' && t.id !== 'redis' && t.category !== 'trade_write',
  )
}

export function filterTargetsForBay(bay: BayDefinition, matrices: MatrixResponse[]): Target[] {
  const envs = bay.prodOnly ? matrices.filter(m => m.environment === 'prod') : matrices
  const out: Target[] = []
  for (const m of envs) {
    for (const t of m.targets) {
      if (t.category === 'trade_write') continue
      if (bay.matrixTargets?.includes(t.id)) out.push(t)
      else if (bay.matrixIdPrefixes?.some(p => t.id.startsWith(p))) out.push(t)
    }
  }
  return out
}

function worstFromTargets(targets: Target[]): BayLamp {
  if (targets.length === 0) return 'unknown'
  if (targets.some(t => t.reachability === 'fail')) return 'fail'
  if (targets.some(t => t.reachability === 'degraded')) return 'degraded'
  if (targets.every(t => t.reachability === 'ok')) return 'ok'
  return 'unknown'
}

export function lampForBay(
  bayId: string,
  matrices: MatrixResponse[],
  context: OpsContextResponse | undefined,
): BayLamp {
  const bay = getBay(bayId)
  if (!bay) return 'unknown'
  if (bay.staticLamp) return bay.staticLamp

  if (bayId === 'bay_promote_gate') {
    if (!context) return 'unknown'
    const status = evaluatePromoteStatus(context, matrices)
    return status.ready ? 'ok' : status.blockedByDecision || status.prodFails ? 'fail' : 'degraded'
  }

  const targets = filterTargetsForBay(bay, matrices)
  return worstFromTargets(targets)
}

export function summaryForBay(
  bayId: string,
  matrices: MatrixResponse[],
): MatrixSummary | null {
  const bay = getBay(bayId)
  if (!bay || bay.staticLamp) return null
  const targets = filterTargetsForBay(bay, matrices)
  if (targets.length === 0) return null
  let ok = 0
  let fail = 0
  let degraded = 0
  let unknown = 0
  for (const t of targets) {
    if (t.reachability === 'ok') ok += 1
    else if (t.reachability === 'fail') fail += 1
    else if (t.reachability === 'degraded') degraded += 1
    else unknown += 1
  }
  return {
    ok,
    fail,
    degraded,
    unknown,
    total: targets.length,
    worstReach: worstFromTargets(targets),
  }
}

export function evaluatePromoteStatus(
  context: OpsContextResponse,
  matrices: MatrixResponse[],
): PromoteStatus {
  const cutover = context.milestones.find(m => m.id === '2c-b-prod-cutover')
  const blockedByDecision = cutover?.status === 'BLOCKED_ON'
  const prodOk = prodMatrixHealthy(matrices)
  const prodFails = hasProdFailures(matrices)
  const gate = context.promotion.last_gate
  const gateDone = gate.result != null && gate.result !== ''
  const gatePass = gateDone && gate.result === 'pass'
  const ready = !blockedByDecision && prodOk && !prodFails && gatePass

  const reasons: string[] = []
  if (blockedByDecision) reasons.push(`Milestone blocked: ${cutover?.blocker ?? 'decision'}`)
  if (prodFails) reasons.push('Prod matrix has failing targets')
  if (!gateDone) reasons.push('Release gate not recorded')
  else if (!gatePass) reasons.push(`Release gate: ${gate.result}`)

  return {
    ready,
    blockedByDecision: !!blockedByDecision,
    prodFails,
    prodOk,
    gateDone,
    gatePass,
    cutoverBlocker: cutover?.blocker ?? null,
    reasons,
  }
}

export function prodFailingTargetIds(matrices: MatrixResponse[]): string[] {
  const prod = getProdMatrix(matrices)
  if (!prod) return []
  return prod.targets
    .filter(t => t.reachability === 'fail' && t.id !== 'redis' && t.category !== 'trade_write')
    .map(t => t.id)
}

export type StgDeliverStatus = {
  ready: boolean
  smokeOk: boolean
  smokeFails: boolean
  deliverSucceeded: boolean
  reasons: string[]
}

export type StgReleaseStatus = StgDeliverStatus & {
  gatePass: boolean
  tierBReady: boolean
  releaseReady: boolean
  releaseReasons: string[]
}

/** STG deliver track — independent of prod matrix / D1 cutover (see DeliveryReleaseWorkflowPanel). */
export function evaluateStgDeliverStatus(
  stgSmoke?: { targets?: { id: string; reachability?: Reachability }[]; reachability?: Reachability },
  lastDeliverSucceeded = false,
): StgDeliverStatus {
  const targets = stgSmoke?.targets ?? []
  const apiTargets = targets.filter(t => t.id.startsWith('stg-api-'))
  const feTarget = targets.find(t => t.id === 'stg-frontend')
  const apiOk =
    apiTargets.length > 0 &&
    apiTargets.every(t => t.reachability === 'ok' || t.reachability === 'degraded')
  const feOk = !feTarget || feTarget.reachability === 'ok' || feTarget.reachability === 'degraded'
  const smokeOk = apiOk && feOk && targets.length > 0
  const smokeFails = targets.some(t => t.reachability === 'fail')
  const ready = smokeOk && lastDeliverSucceeded

  const reasons: string[] = []
  if (!lastDeliverSucceeded) reasons.push('No recent bifrost-deliver-stg success')
  if (targets.length === 0) reasons.push('Stg smoke not configured')
  else if (!smokeOk) reasons.push('Stg smoke incomplete')
  if (smokeFails) reasons.push('Stg smoke has failing targets')

  return { ready, smokeOk, smokeFails, deliverSucceeded: lastDeliverSucceeded, reasons }
}

/** Full STG release track — deliver + STG-tier gate + Tier B sign-off. */
export function evaluateStgReleaseStatus(
  stgSmoke?: { targets?: { id: string; reachability?: Reachability }[]; reachability?: Reachability },
  lastDeliverSucceeded = false,
  stgGate?: { result?: string; ready?: boolean },
  tierB?: { ready?: boolean; signed_off?: boolean },
): StgReleaseStatus {
  const deliver = evaluateStgDeliverStatus(stgSmoke, lastDeliverSucceeded)
  const gatePass = stgGate?.result === 'pass' && stgGate?.ready === true
  const tierBReady = tierB?.ready === true
  const releaseReady = deliver.ready && gatePass && tierBReady

  const releaseReasons: string[] = [...deliver.reasons]
  if (!gatePass) releaseReasons.push('STG release gate not pass')
  if (!tierBReady) {
    releaseReasons.push(
      tierB?.signed_off ? 'Tier B auto probes incomplete' : 'Tier B Owner sign-off pending',
    )
  }

  return {
    ...deliver,
    gatePass,
    tierBReady,
    releaseReady,
    releaseReasons,
  }
}
