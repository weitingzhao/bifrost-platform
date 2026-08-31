/**
 * Vendor Fleet cell + IB Client feed quality gates.
 */
import type { AgentBridgeResponse } from '@/api/agentTypes'
import type { IbGatewayStatusResponse } from '@/api/satelliteBusTypes'
import type { MatrixResponse } from '@/api/matrixTypes'
import { PROD_ENV_FIX_SCOPE } from '@/lib/agent/prodEnvironmentFixPrompt'
import type { Signal } from '@/lib/control-room/missionSignals'
import {
  type FleetCell,
  type FleetCellSignal,
  type FleetStandard,
} from '@/lib/control-room/fleetSnapshot/types'
import { signalFromStandards, std } from '@/lib/control-room/fleetSnapshot/standards'
import { cellKey } from '@/lib/control-room/fleetSnapshot/nav'
import { severityRank } from '@/lib/control-room/fleetSnapshot/verdict'

const GITOPS_HINT_SCOPE = 'gitops-config-repair'

function isMassiveVendorTarget(t: { id: string; auth?: string }): boolean {
  if (t.auth === 'blocked') return false
  const id = t.id.toLowerCase()
  return id === 'api-massive' || id.includes('massive') || id.includes('polygon')
}

function isIbVendorTarget(t: { id: string; auth?: string; category?: string }): boolean {
  if (t.auth === 'blocked') return false
  if (t.category === 'trade_write') return false
  const id = t.id.toLowerCase()
  // Exclude shared placeholders claimed by Massive (e.g. massive-ib)
  if (id.includes('massive') || id.includes('polygon')) return false
  return id.includes('ib') || id.includes('ibkr')
}

function vendorTargets(matrices: MatrixResponse[]) {
  return matrices.flatMap(m =>
    m.targets.filter(t => isMassiveVendorTarget(t) || isIbVendorTarget(t)),
  )
}

export function buildVendorCell(input: {
  bridge?: AgentBridgeResponse
  matrices?: MatrixResponse[]
  /** Platform IB Gateway plugin status — required for Vendor GO. */
  ibGateway?: IbGatewayStatusResponse
  /**
   * When true, Trade monitor still reports ib_not_connected (execution arm /
   * observe gap). Informational only — does not block Vendor GO.
   */
  daemonIbObserve?: boolean
}): FleetCell {
  const hermes = input.bridge?.nous_hermes ?? input.bridge?.hermes_mcp
  const targets = vendorTargets(input.matrices ?? [])
  const massiveTargets = targets.filter(t => isMassiveVendorTarget(t))
  const ibTargets = targets.filter(t => isIbVendorTarget(t))

  const standards: FleetStandard[] = []
  for (const t of massiveTargets) {
    standards.push(
      std(t.id, t.id, t.reachability as FleetCellSignal, t.detail || t.reachability, 'feed'),
    )
  }
  if (massiveTargets.length === 0) {
    // Stable id for Checklist match (massive|polygon). Informational — do not alone NO-GO.
    standards.push(
      std(
        'massive-polygon',
        'Massive / Polygon feed',
        'unknown',
        'Massive/Polygon matrix targets not present',
        'feed',
        false,
      ),
    )
  }
  // IB Client / Gateway — required for Vendor GO (plugin status). D10: observe/manual only, no Agent Fix.
  const ibProbe = resolveIbClientStandard(input.ibGateway, ibTargets)
  standards.push(ibProbe)

  // Informational: daemon execution arm observe state (D10) — does not block Vendor GO.
  if (input.ibGateway != null && (input.ibGateway.mode ?? '').toLowerCase() === 'live') {
    if (input.daemonIbObserve === true) {
      standards.push(
        std(
          'daemon-exec-arm',
          'Daemon execution arm',
          'degraded',
          'Daemon reports ib_not_connected (observe only — D10)',
          'feed',
          false,
        ),
      )
    }
  }

  const hermesSig: Signal =
    hermes == null
      ? 'unknown'
      : hermes.status === 'ok'
        ? 'ok'
        : hermes.status === 'degraded'
          ? 'degraded'
          : 'fail'
  standards.push(
    std(
      'hermes',
      'Hermes ready',
      hermesSig,
      hermes == null ? 'Hermes status unknown' : `Hermes ${hermes.status}`,
      'tooling',
    ),
  )

  // Git bridge is scored on Engineer (automation) — do not mirror on Vendor (closes Board→Checklist gap).

  const signal = signalFromStandards(standards)
  const required = standards.filter(s => s.required !== false)
  const okRequired = required.filter(s => s.signal === 'ok').length
  const value =
    required.length > 0
      ? `${okRequired}/${required.length}`
      : signal === 'ok'
        ? 'ready'
        : signal === 'fail'
          ? 'down'
          : signal === 'degraded'
            ? 'drift'
            : '…'

  const ibBlocking = ibProbe.signal !== 'ok'
  const otherFeedFail = standards.some(
    s =>
      s.required !== false &&
      s.id !== 'ib-feed' &&
      s.group === 'feed' &&
      s.signal !== 'ok' &&
      s.signal !== 'unknown',
  )
  // Massive/Hermes may use Agent Fix; IB Client stays D10 observe (Plugin Gallery / TWS).
  const agentFixEnabled =
    !ibBlocking && (signal === 'fail' || signal === 'degraded') && otherFeedFail

  return {
    key: cellKey('vendor', 'span'),
    role: 'vendor',
    env: null,
    span: true,
    signal,
    value,
    detail: standards
      .filter(s => s.required !== false)
      .map(s => s.reason)
      .join(' · '),
    probePath: '',
    standards,
    fixScope:
      signal === 'ok'
        ? null
        : ibBlocking && !otherFeedFail
          ? null
          : targets.length > 0
            ? PROD_ENV_FIX_SCOPE
            : GITOPS_HINT_SCOPE,
    agentFixEnabled,
    agentFixDisabledReason: ibBlocking
      ? 'IB Client required — observe only (D10). If TWS is already running: Reconnect Gateway (rollout restart data/ib-gateway), then Re-probe.'
      : signal === 'unknown'
        ? 'Still probing'
        : undefined,
    escalateTabId: ibBlocking ? 'plugin-gallery' : undefined,
    countsTowardVerdict: true,
  }
}

/** Map IB Gateway plugin status (+ optional matrix IB targets) → required Vendor feed standard. */
export function resolveIbClientStandard(
  ibGateway: IbGatewayStatusResponse | undefined,
  ibTargets: { id: string; reachability: string; detail?: string }[] = [],
): FleetStandard {
  if (ibGateway != null) {
    const assessed = assessIbGatewaySocketQuality(ibGateway)
    return std('ib-feed', 'IB Client / Gateway', assessed.signal, assessed.reason, 'feed', true)
  }
  // Prefer a real matrix IB probe if present (rare — most IB matrix rows are trade_write blocked).
  if (ibTargets.length > 0) {
    const worst = ibTargets.reduce((a, b) =>
      severityRank(b.reachability as FleetCellSignal) > severityRank(a.reachability as FleetCellSignal)
        ? b
        : a,
    )
    const raw = worst.reachability as string
    const sig: FleetCellSignal =
      raw === 'ok' ||
      raw === 'degraded' ||
      raw === 'fail' ||
      raw === 'unavailable' ||
      raw === 'unknown'
        ? raw
        : 'unknown'
    return std(
      'ib-feed',
      'IB Client / Gateway',
      sig,
      worst.detail || `IB matrix ${worst.id}: ${worst.reachability}`,
      'feed',
      true,
    )
  }
  // No plugin payload yet — required unknown so Vendor cannot GO (loading or API down).
  return std(
    'ib-feed',
    'IB Client / Gateway',
    'unknown',
    'IB Gateway status not loaded — Vendor cannot GO without IB Client',
    'feed',
    true,
  )
}

/**
 * Socket-quality gate for Vendor IB: Redis "connected" alone is not enough.
 * Aligns with api/internal/ibgateway assessSocketFeedQuality.
 */
export function assessIbGatewaySocketQuality(
  ib: IbGatewayStatusResponse,
  nowMs: number = Date.now(),
): { signal: FleetCellSignal; reason: string } {
  const mode = (ib.mode ?? '').toLowerCase()
  const baseReach = (ib.reachability ??
    (ib.reachable === true ? 'ok' : ib.reachable === false ? 'fail' : 'unknown')) as FleetCellSignal
  const baseReason =
    ib.summary ?? ib.error ?? ib.deployment?.detail ?? `IB Gateway ${baseReach}`

  if (mode !== 'live') {
    return { signal: normalizeReach(baseReach), reason: baseReason }
  }

  const ing = ib.ingestor_health ?? {}
  const acc = ib.account_health ?? {}
  const connected = String(ing.connected ?? '').toLowerCase() === 'true'
  if (!connected) {
    return {
      signal: 'fail',
      reason: 'IB ingestor not connected — TWS API socket down',
    }
  }
  const cid = String(ing.client_id ?? '').trim()
  const hostCid = String(acc.host_client_id ?? '').trim()
  if ((!cid || cid === '0') && (!hostCid || hostCid === '0')) {
    return {
      signal: 'fail',
      reason: 'connected flag set but no client_id — TWS API session missing',
    }
  }

  // Ghost-session detector (works on weekends when RTH BBO rule does not fire):
  // plugin may keep Redis "connected" while TWS has no live API clients.
  const snapQ = assessAccountSnapshotQuality(ib.account_snapshot, nowMs)
  if (snapQ) return snapQ

  const ingAge = unixAgeSec(ing.last_msg_ts, nowMs)
  if (ingAge != null && ingAge > 90) {
    return {
      signal: 'fail',
      reason: `IB socket heartbeat stale (${Math.round(ingAge)}s) — treat as dead API client`,
    }
  }
  const accAge = unixAgeSec(acc.last_msg_ts, nowMs)
  if (accAge != null && accAge > 90) {
    return {
      signal: 'fail',
      reason: `IB account heartbeat stale (${Math.round(accAge)}s)`,
    }
  }

  const tickRaw = ib.sample_tick_nvda
  if (tickRaw == null || String(tickRaw).trim() === '') {
    const sig = worseSignal(normalizeReach(baseReach), 'degraded')
    return { signal: sig, reason: `${baseReason} · no sample tick (NVDA)` }
  }

  let bid: number
  let ask: number
  let last: number
  let tickTs: number
  try {
    const tick = typeof tickRaw === 'string' ? JSON.parse(tickRaw) : tickRaw
    bid = Number(tick?.bid ?? 0)
    ask = Number(tick?.ask ?? 0)
    last = Number(tick?.last ?? 0)
    tickTs = Number(tick?.ts ?? 0)
  } catch {
    return {
      signal: worseSignal(normalizeReach(baseReach), 'degraded'),
      reason: `${baseReason} · sample tick unparseable`,
    }
  }
  if (tickTs > 0) {
    const tickAge = unixAgeSec(String(tickTs), nowMs)
    if (tickAge != null && tickAge > 180) {
      return {
        signal: 'fail',
        reason: `sample tick stale (${Math.round(tickAge)}s) — socket not delivering`,
      }
    }
  }
  if (inUSEquityRTH(nowMs) && bid <= 0 && ask <= 0) {
    return {
      signal: 'fail',
      reason: `RTH but no usable BBO (bid/ask≤0)${last > 0 ? ` · last=${last}` : ''} — TWS socket/market-data suspect`,
    }
  }

  return { signal: normalizeReach(baseReach), reason: baseReason }
}

/** Empty / missing account snapshot while claiming connected → ghost TWS API client. */
export function assessAccountSnapshotQuality(
  raw: string | undefined,
  nowMs: number,
): { signal: FleetCellSignal; reason: string } | null {
  if (raw == null || String(raw).trim() === '') {
    return {
      signal: 'fail',
      reason: 'no account snapshot on redis-ib — TWS API session not verified',
    }
  }
  try {
    const snap = typeof raw === 'string' ? JSON.parse(raw) : raw
    const updated = Number(snap?.updated_at ?? 0)
    if (Number.isFinite(updated) && updated > 0) {
      const age = unixAgeSec(String(updated), nowMs)
      if (age != null && age > 90) {
        return {
          signal: 'fail',
          reason: `account snapshot stale (${Math.round(age)}s)`,
        }
      }
    }
    const accounts = snap?.accounts_snapshot
    const count = Array.isArray(accounts) ? accounts.length : 0
    if (count === 0) {
      const hostClaim = snap?.host_connected === true || String(snap?.host_connected).toLowerCase() === 'true'
      const secClaim =
        snap?.secondary_connected === true || String(snap?.secondary_connected).toLowerCase() === 'true'
      if (hostClaim || secClaim) {
        return {
          signal: 'fail',
          reason: 'connected but accounts_snapshot empty — ghost TWS API client',
        }
      }
      return {
        signal: 'fail',
        reason: 'account snapshot has no managed accounts',
      }
    }
  } catch {
    return {
      signal: 'degraded',
      reason: 'account snapshot unparseable',
    }
  }
  return null
}

function normalizeReach(s: FleetCellSignal): FleetCellSignal {
  if (s === 'ok' || s === 'degraded' || s === 'fail' || s === 'unavailable' || s === 'unknown') {
    return s
  }
  return 'unknown'
}

function worseSignal(a: FleetCellSignal, b: FleetCellSignal): FleetCellSignal {
  return severityRank(b) > severityRank(a) ? b : a
}

function unixAgeSec(raw: string | undefined, nowMs: number): number | null {
  if (raw == null || String(raw).trim() === '') return null
  let ts = Number(raw)
  if (!Number.isFinite(ts)) return null
  if (ts > 1e12) ts = ts / 1000
  return nowMs / 1000 - ts
}

function inUSEquityRTH(nowMs: number): boolean {
  const fmt = new Intl.DateTimeFormat('en-US', {
    timeZone: 'America/New_York',
    weekday: 'short',
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
  })
  const parts = Object.fromEntries(fmt.formatToParts(new Date(nowMs)).map(p => [p.type, p.value]))
  const wd = parts.weekday
  if (wd === 'Sat' || wd === 'Sun') return false
  const hour = Number(parts.hour)
  const minute = Number(parts.minute)
  const mins = hour * 60 + minute
  return mins >= 9 * 60 + 30 && mins < 16 * 60
}


