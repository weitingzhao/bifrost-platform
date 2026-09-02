/**
 * Control Room — Copy / Ask for Agent diagnose pack.
 * Mirrors Massive / Research Engine packs: actionable facts, no secrets.
 * Page-independent gather so sidebar Ask works without mounting Control Room.
 */

import { fetchContext, fetchMatrix, fetchSelfHealth, isAllMatrices } from '@/api/core'
import { fetchCluster } from '@/api/cluster'
import { fetchSupplyChain } from '@/api/delivery'
import { fetchStgSmoke } from '@/api/promote'
import { fetchRemediationHealth, fetchRemediationJobs } from '@/api/remediation'
import { fetchAgentBridge } from '@/api/agentOps'
import { fetchOperateQueue } from '@/api/operateQueue'
import type { OperateQueueItem } from '@/api/operateQueueTypes'
import { fetchDecisionBriefs } from '@/api/operateBriefs'
import { fetchIbGatewayStatus } from '@/api/network'
import type { IbGatewayStatusResponse } from '@/api/satelliteBusTypes'
import type { MatrixResponse } from '@/api/matrixTypes'
import type { OpsContextResponse } from '@/api/opsContextTypes'
import { isPendingDecisionBrief } from '@/hooks/useDecisionBriefs'
import { findActiveRemediationJobs } from '@/lib/remediation/remediationJobDisplay'
import {
  buildControlRoomAttentionItems,
  buildControlRoomBaySignals,
  controlRoomBayCountsLabel,
  type ControlRoomAttentionItem,
  type ControlRoomBaySignal,
} from '@/lib/control-room/controlRoomBays'
import {
  buildMissionSnapshot,
  missionStatus,
  worst,
  type MissionSnapshot,
  type Signal,
} from '@/lib/control-room/missionSignals'
import { listFailingMatrixTargets } from '@/lib/control-room/controlRoomOperatePack'

export type ControlRoomAgentFinding = {
  id: string
  severity: 'info' | 'warning' | 'danger'
  title: string
  detail: string
}

export type ControlRoomAgentPackSnapshot = {
  generatedAt: string
  missionOverall: Signal
  bayCountsLabel: string
  bays: ControlRoomBaySignal[]
  attention: ControlRoomAttentionItem[]
  operateOpen: OperateQueueItem[]
  pendingBriefCount: number
  activeAgentJobCount: number
  spineFocus: string | null
  failingMatrix: Array<{ environment: string; id: string; reachability: string; detail?: string }>
  ibGateway: IbGatewayStatusResponse | null
  ibGatewayError: string | null
  mission: MissionSnapshot | null
  missionError: string | null
}

export type ControlRoomAgentAnalysis = {
  findings: ControlRoomAgentFinding[]
  primaryCause: string | null
  needsAttention: boolean
  overall: Signal
}

function line(s: string): string {
  return s.replace(/\s+$/g, '')
}

function rejectMsg(reason: unknown): string {
  return (reason as Error)?.message ?? 'fetch failed'
}

async function gatherMissionSnapshot(): Promise<{
  snapshot: MissionSnapshot
  matrices: MatrixResponse[]
}> {
  const [cluster, supply, stg, self, runner, bridge, matrixRaw] = await Promise.all([
    fetchCluster(),
    fetchSupplyChain(),
    fetchStgSmoke(),
    fetchSelfHealth(),
    fetchRemediationHealth(),
    fetchAgentBridge(),
    fetchMatrix(),
  ])
  const matrices = isAllMatrices(matrixRaw) ? matrixRaw.matrices : [matrixRaw]
  const snapshot = buildMissionSnapshot({
    cluster,
    supply,
    stg,
    self,
    runner,
    bridge,
    matrices,
  })
  return { snapshot, matrices }
}

export async function gatherControlRoomAgentSnapshot(): Promise<ControlRoomAgentPackSnapshot> {
  const generatedAt = new Date().toISOString()

  const [missionRes, queueRes, briefsRes, jobsRes, contextRes, ibRes] = await Promise.allSettled([
    gatherMissionSnapshot(),
    fetchOperateQueue(),
    fetchDecisionBriefs(),
    fetchRemediationJobs(),
    fetchContext(),
    fetchIbGatewayStatus(),
  ])

  let mission: MissionSnapshot | null = null
  let matrices: MatrixResponse[] = []
  let missionError: string | null = null
  if (missionRes.status === 'fulfilled') {
    mission = missionRes.value.snapshot
    matrices = missionRes.value.matrices
  } else {
    missionError = rejectMsg(missionRes.reason)
  }

  const operateOpen =
    queueRes.status === 'fulfilled' ? queueRes.value.open ?? [] : ([] as OperateQueueItem[])
  const pendingBriefCount =
    briefsRes.status === 'fulfilled'
      ? (briefsRes.value ?? []).filter(isPendingDecisionBrief).length
      : 0
  const activeAgentJobCount =
    jobsRes.status === 'fulfilled'
      ? findActiveRemediationJobs(jobsRes.value?.jobs ?? []).length
      : 0

  const context: OpsContextResponse | null =
    contextRes.status === 'fulfilled' ? contextRes.value : null
  const spineFocus = context?.focus?.headline?.trim() || null

  let ibGateway: IbGatewayStatusResponse | null = null
  let ibGatewayError: string | null = null
  if (ibRes.status === 'fulfilled') {
    ibGateway = ibRes.value
    if (ibGateway.error) ibGatewayError = ibGateway.error
  } else {
    ibGatewayError = rejectMsg(ibRes.reason)
  }

  const emptySnap = buildMissionSnapshot({
    cluster: undefined,
    supply: undefined,
    stg: undefined,
    self: undefined,
    runner: undefined,
    bridge: undefined,
    matrices: [],
  })
  const snapshot = mission ?? emptySnap

  const bays = buildControlRoomBaySignals({
    snapshot,
    operateOpenCount: operateOpen.length,
    pendingBriefCount,
    activeAgentJobCount,
    showHealth: true,
  })
  const attention = buildControlRoomAttentionItems(bays)

  const failingMatrix = matrices.length > 0 ? listFailingMatrixTargets(matrices) : []

  return {
    generatedAt,
    missionOverall: snapshot.missionOverall,
    bayCountsLabel: controlRoomBayCountsLabel(bays),
    bays,
    attention,
    operateOpen,
    pendingBriefCount,
    activeAgentJobCount,
    spineFocus,
    failingMatrix,
    ibGateway,
    ibGatewayError,
    mission,
    missionError,
  }
}

export function analyzeControlRoomAgent(snap: ControlRoomAgentPackSnapshot): ControlRoomAgentAnalysis {
  const findings: ControlRoomAgentFinding[] = []

  for (const item of snap.attention) {
    findings.push({
      id: item.id,
      severity: item.severity === 'critical' ? 'danger' : item.severity === 'warning' ? 'warning' : 'info',
      title: item.summary,
      detail: `Bay ${item.bayId}`,
    })
  }

  if (snap.operateOpen.length > 0) {
    findings.push({
      id: 'operate-handoffs',
      severity: 'warning',
      title: `Operate queue: ${snap.operateOpen.length} open handoff(s)`,
      detail: snap.operateOpen
        .slice(0, 6)
        .map(h => h.title)
        .join('; '),
    })
  }

  if (snap.pendingBriefCount > 0) {
    findings.push({
      id: 'pending-briefs',
      severity: 'warning',
      title: `${snap.pendingBriefCount} pending decision brief(s)`,
      detail: 'Owner Start / Prepare / Dismiss still outstanding',
    })
  }

  if (snap.failingMatrix.length > 0) {
    findings.push({
      id: 'matrix-fail',
      severity: 'danger',
      title: `${snap.failingMatrix.length} failing/degraded matrix target(s)`,
      detail: snap.failingMatrix
        .slice(0, 8)
        .map(t => `${t.environment}/${t.id}`)
        .join(', '),
    })
  }

  const ibSum = (snap.ibGateway?.summary ?? '').toLowerCase()
  const ibReady = snap.ibGateway?.deployment?.ready ?? ''
  let ibSignal: Signal = 'ok'
  if (
    snap.ibGatewayError ||
    ibReady.startsWith('0/') ||
    ibSum.includes('fail') ||
    ibSum.includes('stale')
  ) {
    ibSignal = 'fail'
    findings.push({
      id: 'ib-gateway',
      severity: 'danger',
      title: 'IB Gateway / Live stream caution',
      detail:
        snap.ibGatewayError ??
        snap.ibGateway?.summary ??
        `deployment ${ibReady || '—'}`,
    })
  }

  if (snap.missionError) {
    findings.push({
      id: 'mission-gather',
      severity: 'warning',
      title: 'Mission snapshot gather failed',
      detail: snap.missionError,
    })
  }

  const overall = worst(
    snap.missionOverall,
    ibSignal,
    ...snap.bays.map(b => b.signal),
    snap.attention.some(a => a.severity === 'critical') ? 'fail' : 'ok',
    snap.attention.some(a => a.severity === 'warning') ? 'degraded' : 'ok',
    snap.missionError ? 'degraded' : 'ok',
  )

  const primary =
    findings.find(f => f.severity === 'danger') ??
    findings.find(f => f.severity === 'warning') ??
    null

  return {
    findings,
    primaryCause: primary?.title ?? null,
    needsAttention: findings.some(f => f.severity !== 'info') || snap.attention.length > 0,
    overall,
  }
}

export function buildControlRoomAgentPack(snap: ControlRoomAgentPackSnapshot): string {
  const analysis = analyzeControlRoomAgent(snap)
  const lines: string[] = []
  const push = (...xs: string[]) => {
    for (const x of xs) lines.push(line(x))
  }

  push(
    '# Control Room — Agent repair pack',
    `Generated: ${snap.generatedAt}`,
    'Source: Ops Console → Mission Control → Control Room (Copy for Agent / Ask for Agent)',
    '',
    '## Goal',
    'Diagnose and clear Control Room Attention / Operate caution below.',
    'Prefer durable posture fixes (handoff Start·Prepare·Dismiss, probe recovery, IB Gateway reconnect) over page refreshes.',
    'Constraints: D10 BLOCKED — no live trading / ib:operator:cmd / daemon scale-up for live auto-trade.',
    'Do not treat Operate handoff backlog as Mission probe CRITICAL unless Mission bay is also fail.',
    '',
  )

  push('## Console verdict')
  push(
    `${missionStatus(analysis.overall)} — Mission ${missionStatus(snap.missionOverall)} · Bays ${snap.bayCountsLabel}`,
  )
  if (analysis.primaryCause) push(`primary_cause: ${analysis.primaryCause}`)
  if (snap.spineFocus) push(`spine_focus: ${snap.spineFocus}`)
  push('')

  push('## Bay scan')
  for (const bay of snap.bays) {
    push(`- ${bay.id}: ${bay.signal} — ${bay.reason}`)
  }
  push('')

  push('## Attention')
  if (snap.attention.length === 0) {
    push('CLEAR — no caution/critical bays')
  } else {
    for (const a of snap.attention) {
      push(`- [${a.severity}] ${a.summary} (bay=${a.bayId})`)
    }
  }
  push('')

  push('## Operate queue (open handoffs)')
  push(
    `open=${snap.operateOpen.length} · pending_briefs=${snap.pendingBriefCount} · active_agent_jobs=${snap.activeAgentJobCount}`,
  )
  if (snap.operateOpen.length === 0) {
    push('(none)')
  } else {
    for (const h of snap.operateOpen.slice(0, 12)) {
      const lane = h.operate_lane ?? h.lane ?? '—'
      const risk = h.risk_level ?? '—'
      push(`- ${h.id}: ${h.title} · lane=${lane} · risk=${risk} · source=${h.source ?? '—'}`)
      if (h.description) push(`  ${h.description.slice(0, 200)}`)
      if (h.acceptance_criteria && h.acceptance_criteria.length > 0) {
        push(`  acceptance: ${h.acceptance_criteria.slice(0, 3).join('; ')}`)
      }
    }
    if (snap.operateOpen.length > 12) {
      push(`- … and ${snap.operateOpen.length - 12} more (open Operate Queue / Agent Desk)`)
    }
  }
  push('')

  push('## Findings')
  if (analysis.findings.length === 0) {
    push('- [info] No Attention / Operate / matrix / IB findings')
  } else {
    for (const f of analysis.findings) {
      push(`- [${f.severity}] ${f.title}${f.detail ? ` — ${f.detail}` : ''}`)
    }
  }
  push('')

  push('## Failing matrix targets')
  if (snap.failingMatrix.length === 0) {
    push('(none)')
  } else {
    for (const t of snap.failingMatrix.slice(0, 24)) {
      const detail = t.detail ? ` — ${t.detail}` : ''
      push(`- ${t.environment} · ${t.id} (${t.reachability})${detail}`)
    }
  }
  push('')

  push('## IB Gateway (Live stream)')
  if (snap.ibGateway != null) {
    push(
      `summary: ${snap.ibGateway.summary ?? '—'}`,
      `deployment: ${snap.ibGateway.deployment?.ready ?? '—'} · mode=${snap.ibGateway.mode ?? '—'}`,
      `redis: ${snap.ibGateway.redis_reachability ?? '—'}`,
    )
    for (const s of snap.ibGateway.slots ?? []) {
      push(
        `  slot ${s.slot}: status=${s.status ?? '—'} connected=${s.connected ?? '—'} reach=${s.reachability ?? '—'}`,
      )
    }
    if (snap.ibGatewayError) push(`error: ${snap.ibGatewayError}`)
  } else {
    push(`unavailable: ${snap.ibGatewayError ?? 'no probe'}`)
  }
  push('')

  push(
    '## Suggested investigation order',
    '1. If Attention Mission/Launch CRITICAL → fix matrix / cluster / STG smoke probes first (read-only MCP: get_connectivity_matrix, get_cluster_summary).',
    '2. If Attention is Operate caution with open handoffs → triage Operate Queue: Start / Prepare / Dismiss stale items; do not invent new remediation scopes.',
    '3. If pending decision briefs > 0 → Owner decide on briefs before enqueueing more Agent jobs.',
    '4. If IB Gateway deployment 0/1 or feed stale → rollout restart data/ib-gateway (Reconnect); verify GET /api/v1/plugins/ib-gateway/status tick age < 30s. D10 still BLOCKED.',
    '5. If matrix targets fail only on one env → isolate env overlay / ExternalName / secrets before cluster-wide changes.',
    '6. After fixes: Refresh Control Room; Attention CLEAR or handoff count reduced; re-copy this pack for before/after.',
    '',
    '## Owner ask',
    'Propose the smallest durable fix for the primary Attention/Operate cause, verify with the same endpoints this pack used, then report before/after bay + handoff counts.',
  )

  return lines.join('\n')
}

/** Prefill for Agent Desk when Attention is non-clear. */
export function buildControlRoomDiagnosePrefill(snap: ControlRoomAgentPackSnapshot): string {
  return buildControlRoomAgentPack(snap)
}
