/**
 * Clipboard pack for AI agents from the IB Client (IB Gateway) surface.
 * Mirrors Massive / Flex Copy for Agent — actionable facts, no secrets.
 */

import { fetchIbGatewaySelfHeal, fetchIbGatewayStatus } from '@/api/network'
import type {
  IbGatewaySelfHealResponse,
  IbGatewaySlotStatus,
  IbGatewayStatusResponse,
} from '@/api/satelliteBusTypes'
import {
  assessAccountSnapshotQuality,
  assessIbGatewaySocketQuality,
} from '@/lib/control-room/fleetSnapshot/buildVendorCell'

export type IbGatewayRemediationFinding = {
  id: string
  severity: 'info' | 'warning' | 'danger'
  title: string
  detail: string
}

export type IbGatewayRemediationAnalysis = {
  findings: IbGatewayRemediationFinding[]
  needsAttention: boolean
  primaryCause: string | null
  socketSignal: 'ok' | 'degraded' | 'fail' | 'unknown'
  socketReason: string
}

export type IbGatewayAgentPackSnapshot = {
  generatedAt: string
  status: IbGatewayStatusResponse | null
  statusError: string | null
  selfHeal: IbGatewaySelfHealResponse | null
  selfHealError: string | null
  analysis: IbGatewayRemediationAnalysis
}

function line(s: string): string {
  return s.replace(/\s+$/g, '')
}

function unixAgeSec(raw: string | undefined, nowMs: number): number | null {
  if (raw == null || String(raw).trim() === '') return null
  let ts = Number(raw)
  if (!Number.isFinite(ts)) return null
  if (ts > 1e12) ts = ts / 1000
  return nowMs / 1000 - ts
}

function formatAgeSec(sec: number | null | undefined): string {
  if (sec == null || !Number.isFinite(sec)) return '—'
  if (sec < 60) return `${Math.round(sec)}s`
  if (sec < 3600) return `${Math.round(sec / 60)}m`
  return `${(sec / 3600).toFixed(1)}h`
}

function parseSnapshotMeta(
  raw: string | undefined,
  nowMs: number,
): {
  updatedAgeSec: number | null
  accountCount: number | null
  hostConnected: boolean | null
  secondaryConnected: boolean | null
} {
  if (raw == null || String(raw).trim() === '') {
    return {
      updatedAgeSec: null,
      accountCount: null,
      hostConnected: null,
      secondaryConnected: null,
    }
  }
  try {
    const snap = typeof raw === 'string' ? JSON.parse(raw) : raw
    const updated = Number(snap?.updated_at ?? 0)
    const updatedAgeSec =
      Number.isFinite(updated) && updated > 0
        ? unixAgeSec(String(updated), nowMs)
        : null
    const accounts = snap?.accounts_snapshot
    const accountCount = Array.isArray(accounts) ? accounts.length : null
    const hostConnected =
      snap?.host_connected === true || String(snap?.host_connected).toLowerCase() === 'true'
        ? true
        : snap?.host_connected === false || String(snap?.host_connected).toLowerCase() === 'false'
          ? false
          : null
    const secondaryConnected =
      snap?.secondary_connected === true ||
      String(snap?.secondary_connected).toLowerCase() === 'true'
        ? true
        : snap?.secondary_connected === false ||
            String(snap?.secondary_connected).toLowerCase() === 'false'
          ? false
          : null
    return { updatedAgeSec, accountCount, hostConnected, secondaryConnected }
  } catch {
    return {
      updatedAgeSec: null,
      accountCount: null,
      hostConnected: null,
      secondaryConnected: null,
    }
  }
}

function slotFinding(slot: IbGatewaySlotStatus): IbGatewayRemediationFinding | null {
  const detail = (slot.detail ?? '').toLowerCase()
  const status = (slot.status ?? '').toLowerCase()
  if (slot.reachability === 'fail') {
    return {
      id: `slot-fail-${slot.slot}`,
      severity: 'danger',
      title: `Slot ${slot.slot} unreachable`,
      detail: slot.detail ?? `status=${slot.status ?? '—'}`,
    }
  }
  if (detail.includes('feed stale') || status.includes('feed stale')) {
    return {
      id: `slot-feed-stale-${slot.slot}`,
      severity: 'danger',
      title: `Slot ${slot.slot} connected but feed stale`,
      detail: slot.detail ?? 'Redis connected flag may be stale — verify account snapshot age',
    }
  }
  if (!slot.connected && slot.reachability !== 'ok') {
    return {
      id: `slot-disconnected-${slot.slot}`,
      severity: 'warning',
      title: `Slot ${slot.slot} not connected`,
      detail: slot.detail ?? `status=${slot.status ?? '—'}`,
    }
  }
  return null
}

export function analyzeIbGatewayProbe(
  status: IbGatewayStatusResponse | undefined,
  selfHeal?: IbGatewaySelfHealResponse | undefined,
  nowMs: number = Date.now(),
): IbGatewayRemediationAnalysis {
  const findings: IbGatewayRemediationFinding[] = []

  if (status == null) {
    return {
      findings: [
        {
          id: 'probe-missing',
          severity: 'warning',
          title: 'Probe unavailable',
          detail: 'platform-api IB Gateway status not loaded yet.',
        },
      ],
      needsAttention: true,
      primaryCause: 'Probe unavailable',
      socketSignal: 'unknown',
      socketReason: 'probe missing',
    }
  }

  if (status.error) {
    findings.push({
      id: 'probe-error',
      severity: 'danger',
      title: 'Plugin probe error',
      detail: status.hint ? `${status.error} — ${status.hint}` : status.error,
    })
  }

  const dep = status.deployment
  if (dep != null && (dep.reachability === 'fail' || dep.ready?.startsWith('0/'))) {
    findings.push({
      id: 'deploy-not-ready',
      severity: 'danger',
      title: 'ib-gateway deployment not ready',
      detail: `${dep.name ?? 'ib-gateway'} ${dep.ready ?? '—'} · ${dep.detail ?? '—'}`,
    })
  }

  if (status.redis_reachability === 'fail') {
    findings.push({
      id: 'redis-fail',
      severity: 'danger',
      title: 'redis-ib unreachable',
      detail: 'Trade namespaces cannot read shared IB bus until redis-ib ExternalName resolves.',
    })
  }

  for (const slot of status.slots ?? []) {
    const f = slotFinding(slot)
    if (f != null) findings.push(f)
  }

  const socketQ = assessIbGatewaySocketQuality(status, nowMs)
  if (socketQ.signal === 'fail' || socketQ.signal === 'degraded') {
    findings.push({
      id: 'socket-quality',
      severity: socketQ.signal === 'fail' ? 'danger' : 'warning',
      title: 'Socket / feed quality gate',
      detail: socketQ.reason,
    })
  }

  const snapQ = assessAccountSnapshotQuality(status.account_snapshot, nowMs)
  if (snapQ != null && snapQ.signal !== 'ok') {
    findings.push({
      id: 'snapshot-quality',
      severity: snapQ.signal === 'fail' ? 'danger' : 'warning',
      title: 'Account snapshot ghost-session check',
      detail: snapQ.reason,
    })
  }

  if (selfHeal != null) {
    const streak = selfHeal.stale_streak ?? 0
    if (streak >= 3) {
      findings.push({
        id: 'self-heal-streak',
        severity: 'danger',
        title: 'Self-heal stale streak elevated',
        detail: `streak=${streak} · last_action=${selfHeal.last_action ?? '—'} · snapshot_age=${formatAgeSec(selfHeal.snapshot_age_sec)}`,
      })
    }
    if (selfHeal.rollout_recommended) {
      findings.push({
        id: 'self-heal-rollout',
        severity: 'warning',
        title: 'Self-heal recommends rollout restart',
        detail: selfHeal.reason ?? 'L1 rollout recommended — verify OPS_IB_AUTOREPAIR_ENABLED',
      })
    }
    if (selfHeal.enabled === false) {
      findings.push({
        id: 'self-heal-disabled',
        severity: 'info',
        title: 'Plugin self-heal disabled',
        detail: 'In-pod ladder idle — rely on Console Reconnect or enable autorepair on platform-api.',
      })
    }
  }

  const cutover = status.cutover
  if (cutover != null && cutover.reachability !== 'ok') {
    findings.push({
      id: 'cutover-degraded',
      severity: 'warning',
      title: 'Trade cutover check degraded',
      detail: `legacy_retired=${cutover.legacy_socket_retired ?? '—'} · reach=${cutover.reachability ?? '—'}`,
    })
  }

  for (const env of cutover?.environments ?? []) {
    if (env.legacy_ib_replicas > 0) {
      findings.push({
        id: `legacy-replicas-${env.namespace}`,
        severity: 'warning',
        title: `Legacy IB replicas in ${env.namespace}`,
        detail: `${env.legacy_ib_replicas} legacy replica(s) — cutover incomplete`,
      })
    }
  }

  const needsAttention =
    findings.some(f => f.severity === 'danger' || f.severity === 'warning') ||
    socketQ.signal === 'fail' ||
    socketQ.signal === 'degraded'

  const primaryCause =
    findings.find(f => f.severity === 'danger')?.title ??
    (socketQ.signal === 'fail' ? socketQ.reason : null) ??
    (findings.length > 0 ? findings[0].title : null)

  return {
    findings,
    needsAttention,
    primaryCause,
    socketSignal: socketQ.signal === 'unavailable' ? 'unknown' : socketQ.signal,
    socketReason: socketQ.reason,
  }
}

export async function gatherIbGatewayAgentSnapshot(): Promise<IbGatewayAgentPackSnapshot> {
  const generatedAt = new Date().toISOString()
  const [statusRes, selfHealRes] = await Promise.allSettled([
    fetchIbGatewayStatus(),
    fetchIbGatewaySelfHeal(),
  ])

  const status = statusRes.status === 'fulfilled' ? statusRes.value : null
  const statusError =
    statusRes.status === 'rejected'
      ? ((statusRes.reason as Error)?.message ?? 'fetch failed')
      : status?.error ?? null

  const selfHeal = selfHealRes.status === 'fulfilled' ? selfHealRes.value : null
  const selfHealError =
    selfHealRes.status === 'rejected'
      ? ((selfHealRes.reason as Error)?.message ?? 'fetch failed')
      : selfHeal?.error ?? null

  const analysis = analyzeIbGatewayProbe(status ?? undefined, selfHeal ?? undefined)

  return {
    generatedAt,
    status,
    statusError,
    selfHeal,
    selfHealError,
    analysis,
  }
}

export function buildIbGatewayDiagnosePrefill(
  snap: IbGatewayAgentPackSnapshot,
): string {
  const st = snap.status
  const lines = [
    'IB Gateway (IB Client) plugin — assisted diagnose (L0 read-only + operator reconnect only).',
    'D10: do NOT enable live trading, ib:operator:cmd writes, or daemon scale-up for auto-trade.',
    '',
    `Verdict: ${st?.reachability ?? 'unknown'} · ${st?.summary ?? '—'}`,
    `Socket quality: ${snap.analysis.socketSignal} — ${snap.analysis.socketReason}`,
  ]

  if (snap.analysis.primaryCause) {
    lines.push(`Primary cause: ${snap.analysis.primaryCause}`)
  }

  if ((st?.slots?.length ?? 0) > 0) {
    lines.push('', 'slots:')
    for (const s of st!.slots!) {
      lines.push(
        `- ${s.slot}: status=${s.status ?? '—'} connected=${s.connected ?? '—'} reach=${s.reachability ?? '—'}`,
      )
    }
  }

  if (snap.analysis.findings.length > 0) {
    lines.push('', 'Findings:')
    for (const f of snap.analysis.findings) {
      lines.push(`- [${f.severity}] ${f.title}: ${f.detail}`)
    }
  }

  lines.push(
    '',
    'Remediation plan (operator approval for rollout):',
    '1. Confirm mode=live and deployment ib-gateway data NS ready 1/1.',
    '2. Compare ingestor last_msg_ts vs account_snapshot.updated_at — ghost session if snapshot frozen while heartbeat fresh.',
    '3. POST reconnect (rollout restart ib-gateway) when soft reconnect exhausted; verify snapshot_age < 90s.',
    '4. Ensure platform-api OPS_IB_AUTOREPAIR_ENABLED=true for L1 auto-rollout; plugin self-heal key ib:control:gateway_self_heal.',
    '5. Verify Trade cutover: legacy_ib_replicas=0 and redis-ib ExternalName ok in dev/stg/prod.',
    '6. Report evidence before TWS credential or K8s secret changes. D10 remains BLOCKED.',
  )

  return lines.join('\n')
}

export function buildIbGatewayAgentPack(snap: IbGatewayAgentPackSnapshot): string {
  const lines: string[] = []
  const push = (...xs: string[]) => {
    for (const x of xs) lines.push(line(x))
  }
  const nowMs = Date.parse(snap.generatedAt)
  const st = snap.status
  const meta = parseSnapshotMeta(st?.account_snapshot, nowMs)

  push(
    '# IB Gateway (IB Client) Plugin — Agent repair pack',
    `Generated: ${snap.generatedAt}`,
    'Source: Ops Console → Plugin → IB Client (Copy for Agent)',
    '',
    '## Goal',
    'Diagnose and fix red/yellow IB Client signals (ghost TWS session, feed stale, deployment down).',
    'Prefer durable fixes (rollout restart, autorepair ladder, cutover hygiene) over page refresh.',
    'Constraints: D10 BLOCKED — no live trading / ib:operator:cmd / daemon scale-up for auto-trade.',
    'Ground truth = redis-ib ingestor heartbeat + account_snapshot.updated_at — not Redis "connected" alone.',
    '',
    '## Verdict',
    `reachability: ${st?.reachability ?? '—'}`,
    `summary: ${st?.summary ?? '—'}`,
    `socket_quality: ${snap.analysis.socketSignal} — ${snap.analysis.socketReason}`,
    `primary_cause: ${snap.analysis.primaryCause ?? 'none flagged'}`,
    '',
  )

  if (st != null) {
    push(
      '## Deployment & bus',
      `mode: ${st.mode ?? '—'}`,
      `deployment: ${st.deployment?.ready ?? '—'} (${st.deployment?.namespace ?? 'data'}/${st.deployment?.name ?? 'ib-gateway'})`,
      `redis_reachability: ${st.redis_reachability ?? '—'}`,
      `autonomy: ${st.autonomy ?? '—'}`,
      '',
    )

    const ing = st.ingestor_health ?? {}
    const acc = st.account_health ?? {}
    push(
      '## Ingestor / account health (redis-ib)',
      `ingestor.connected: ${ing.connected ?? '—'}`,
      `ingestor.client_id: ${ing.client_id ?? '—'}`,
      `ingestor.last_msg_age: ${formatAgeSec(unixAgeSec(ing.last_msg_ts, nowMs))}`,
      `account.host_client_id: ${acc.host_client_id ?? '—'}`,
      `account.last_msg_age: ${formatAgeSec(unixAgeSec(acc.last_msg_ts, nowMs))}`,
      '',
    )

    push(
      '## Account snapshot (ghost-session detector)',
      `updated_age: ${formatAgeSec(meta.updatedAgeSec)}`,
      `accounts_count: ${meta.accountCount ?? '—'}`,
      `host_connected: ${meta.hostConnected ?? '—'}`,
      `secondary_connected: ${meta.secondaryConnected ?? '—'}`,
      '',
    )

    if ((st.slots?.length ?? 0) > 0) {
      push('## Slots')
      for (const s of st.slots!) {
        push(
          `- ${s.slot}: account=${s.account_id ?? '—'} status=${s.status ?? '—'} connected=${s.connected} reach=${s.reachability ?? '—'}${s.detail ? ` · ${s.detail}` : ''}`,
        )
      }
      push('')
    }

    const tickAge = unixAgeSec(st.sample_tick_nvda ? tryTickTs(st.sample_tick_nvda) : undefined, nowMs)
    push('## Sample tick (NVDA)', `tick_age: ${formatAgeSec(tickAge)}`, '')
  } else {
    push(`## Deployment & bus`, `unavailable: ${snap.statusError ?? 'no probe'}`, '')
  }

  const sh = snap.selfHeal
  push('## Self-heal ladder (plugin + platform-api)')
  if (sh != null) {
    push(
      `reachability: ${sh.reachability ?? '—'}`,
      `enabled: ${sh.enabled ?? '—'}`,
      `auto_repair_enabled: ${sh.auto_repair_enabled ?? '—'}`,
      `last_action: ${sh.last_action ?? 'idle'}`,
      `stale_streak: ${sh.stale_streak ?? 0}`,
      `snapshot_age_sec: ${sh.snapshot_age_sec ?? '—'}`,
      `rollout_recommended: ${sh.rollout_recommended ?? false}`,
      `reason: ${sh.reason ?? '—'}`,
    )
  } else {
    push(`unavailable: ${snap.selfHealError ?? 'no self-heal probe'}`)
  }
  push(
    '',
    'Ladder: L0 soft reconnect → L0.5 pod exit when snapshot stale after reconnect → L1 rollout (OPS_IB_AUTOREPAIR_ENABLED).',
    'Self-heal state key: ib:control:gateway_self_heal (redis-ib ACL).',
    '',
  )

  const cut = st?.cutover
  if (cut != null) {
    push(
      '## Trade cutover',
      `legacy_socket_retired: ${cut.legacy_socket_retired ?? '—'}`,
      `reachability: ${cut.reachability ?? '—'}`,
    )
    for (const env of cut.environments ?? []) {
      push(
        `- ${env.namespace}: legacy_replicas=${env.legacy_ib_replicas ?? 0} redis_ext=${env.redis_ib_external_name_ok ?? '—'} reach=${env.reachability ?? '—'}`,
      )
    }
    push('')
  }

  if (snap.analysis.findings.length > 0) {
    push('## Findings')
    for (const f of snap.analysis.findings) {
      push(`- [${f.severity}] ${f.title}: ${f.detail}`)
    }
    push('')
  }

  push(
    '## Suggested investigation order',
    '1. GET /api/v1/plugins/ib-gateway/status — compare ingestor last_msg_ts vs account_snapshot.updated_at.',
    '2. GET /api/v1/plugins/ib-gateway/self-heal — stale_streak, auto_repair_enabled, rollout_recommended.',
    '3. If connected+feed stale or snapshot_age > 90s → operator POST reconnect (rollout restart data/ib-gateway).',
    '4. If self-heal idle but streak high → confirm plugin 0.2.1+ writes ib:control:gateway_self_heal; platform-api OPS_IB_AUTOREPAIR_ENABLED=true.',
    '5. kubectl logs deployment/ib-gateway -n data — _market_loop timeout / soft reconnect loop.',
    '6. Verify cutover: legacy_ib_replicas=0; redis-ib ExternalName in bifrost-{dev,stg,prod}.',
    '7. Re-copy this pack after fix for before/after evidence. D10 remains BLOCKED.',
    '',
  )

  if (snap.statusError) push(`status_error: ${snap.statusError}`)
  if (snap.selfHealError) push(`self_heal_error: ${snap.selfHealError}`)

  return lines.join('\n')
}

function tryTickTs(raw: string): string | undefined {
  try {
    const tick = typeof raw === 'string' ? JSON.parse(raw) : raw
    const ts = tick?.ts
    return ts != null ? String(ts) : undefined
  } catch {
    return undefined
  }
}
