import type { Signal } from '@/lib/control-room/missionSignals'

export type ReadinessChipActionKind = 'navigate' | 'actuate'

export type ReadinessActuation =
  | { kind: 'rollout-restart'; namespace: string; deployment: string }
  | { kind: 'ib-gateway-reconnect' }

export type ReadinessChipAction = {
  kind: ReadinessChipActionKind
  label: string
  tabId?: string
  /** Satellite Bus section scroll target */
  busFocus?: 'rocket' | 'socket' | 'ingest' | 'monitor' | 'trade-apis' | 'workers' | 'cluster'
  /** Prefill Satellite API & Auth Probes env when navigating there */
  apiEnv?: 'dev' | 'stg' | 'prod'
  actuation?: ReadinessActuation
  requiresOperate?: boolean
}

export type ReadinessChipContext = {
  modeId: 'mission-launch' | 'daily-ops' | string
  env: 'stg' | 'prod' | 'platform-stg' | 'platform-prod'
}

const TRADE_NS: Record<'stg' | 'prod', string> = {
  stg: 'bifrost-stg',
  prod: 'bifrost-prod',
}

function chipLabelNorm(chipLabel: string): string {
  return chipLabel.toLowerCase()
}

function chipMatchesTradeApis(label: string): boolean {
  return label.includes('trade apis') || (label.includes('trade') && label.includes('apis'))
}

export type ChipNavigation = {
  tabId: string
  busFocus?: ReadinessChipAction['busFocus']
  /** Prefill Satellite API & Auth Probes env segment when tabId is satellite-api. */
  apiEnv?: 'dev' | 'stg' | 'prod'
}

function tradeApiEnv(ctx: ReadinessChipContext): ChipNavigation['apiEnv'] {
  if (ctx.env === 'prod' || ctx.env === 'platform-prod') return 'prod'
  if (ctx.env === 'stg' || ctx.env === 'platform-stg') return 'stg'
  return 'prod'
}

/** Primary drill-down when operator clicks a readiness chip (ok or not). */
export function primaryChipNavigation(
  chipLabel: string,
  ctx: ReadinessChipContext,
): ChipNavigation | null {
  const label = chipLabelNorm(chipLabel)
  if (label.includes('ib socket') || label.includes('rocket · ib') || label.includes('shared rocket')) {
    return { tabId: 'satellite-bus', busFocus: 'rocket' }
  }
  if (label.includes('pg / redis')) {
    return { tabId: 'satellite-bus', busFocus: 'cluster' }
  }
  if (chipMatchesTradeApis(label)) {
    return { tabId: 'satellite-api', apiEnv: tradeApiEnv(ctx) }
  }
  // Full Trade connectivity matrix (mission tradeProd) — land on API & Auth Probes for that env.
  if (
    label.includes('prod matrix') ||
    label.includes('stg matrix') ||
    (label.includes('matrix') && (label.includes('trade') || label.includes('prod') || label.includes('stg')))
  ) {
    return { tabId: 'satellite-api', apiEnv: tradeApiEnv(ctx) }
  }
  if (label.includes('k8s')) {
    return { tabId: 'cluster' }
  }
  if (label.includes('ci/cd') || label.includes('gate') || label.includes('supply')) {
    const tradeEnv = ctx.env === 'stg' || ctx.env === 'prod'
    return { tabId: tradeEnv ? 'trade-release' : 'platform-release' }
  }
  if (label.includes('promote') || label.includes('cutover') || label.includes('stg release')) {
    return { tabId: 'trade-release' }
  }
  if (label.includes('self-health')) {
    return { tabId: 'platform-release' }
  }
  return null
}

/** Built-in fix actions for a failing readiness chip (L0 navigate + L1 actuation). */
export function readinessChipFixActions(
  chipLabel: string,
  signal: Signal,
  ctx: ReadinessChipContext,
): ReadinessChipAction[] {
  if (signal === 'ok') return []

  const label = chipLabelNorm(chipLabel)
  const actions: ReadinessChipAction[] = []
  const tradeEnv = ctx.env === 'prod' ? 'prod' : ctx.env === 'stg' ? 'stg' : null
  const tradeNs = tradeEnv != null ? TRADE_NS[tradeEnv] : null

  const pushNavigate = (
    tabId: string,
    actionLabel: string,
    busFocus?: ReadinessChipAction['busFocus'],
    apiEnv?: ReadinessChipAction['apiEnv'],
  ) => {
    actions.push({ kind: 'navigate', label: actionLabel, tabId, busFocus, apiEnv })
  }

  if (
    (label.includes('ib socket') || label.includes('rocket · ib') || label.includes('shared rocket')) &&
    tradeNs != null
  ) {
    pushNavigate('satellite-bus', 'Rocket IB bus', 'rocket')
    pushNavigate('satellite-bus', 'Socket matrix', 'socket')
    pushNavigate('plugin-gallery', 'IB Gateway plugin')
    actions.push({
      kind: 'actuate',
      label: 'Restart api-monitor',
      requiresOperate: true,
      actuation: { kind: 'rollout-restart', namespace: tradeNs, deployment: 'api-monitor' },
    })
    actions.push({
      kind: 'actuate',
      label: 'Gateway reconnect',
      requiresOperate: true,
      actuation: { kind: 'ib-gateway-reconnect' },
    })
    return actions
  }

  if (label.includes('pg / redis')) {
    pushNavigate('satellite-bus', 'Ground cluster', 'cluster')
    pushNavigate('cluster', 'Datastore domains')
    return actions
  }

  if (chipMatchesTradeApis(label)) {
    pushNavigate('satellite-api', 'API & Auth Probes', undefined, tradeApiEnv(ctx))
    pushNavigate('satellite-bus', 'API reachability', 'trade-apis')
    return actions
  }

  if (
    label.includes('prod matrix') ||
    label.includes('stg matrix') ||
    (label.includes('matrix') && (label.includes('trade') || label.includes('prod') || label.includes('stg')))
  ) {
    pushNavigate('satellite-api', 'API & Auth Probes', undefined, tradeApiEnv(ctx))
    pushNavigate('control-room', 'Control Room')
    return actions
  }

  if (label.includes('k8s')) {
    pushNavigate('cluster', 'Cluster workloads')
    if (tradeNs != null) {
      actions.push({
        kind: 'actuate',
        label: 'Restart api-monitor',
        requiresOperate: true,
        actuation: { kind: 'rollout-restart', namespace: tradeNs, deployment: 'api-monitor' },
      })
    }
    return actions
  }

  if (label.includes('ci/cd') || label.includes('gate') || label.includes('supply')) {
    const tradeEnv = ctx.env === 'stg' || ctx.env === 'prod'
    pushNavigate(
      tradeEnv ? 'trade-release' : 'platform-release',
      tradeEnv ? 'Deploy Satellite' : 'Launch Rocket',
    )
    return actions
  }

  if (label.includes('self-health')) {
    pushNavigate('platform-release', 'Launch Rocket')
    return actions
  }

  const primary = primaryChipNavigation(chipLabel, ctx)
  if (primary != null) {
    pushNavigate(primary.tabId, 'Open details', primary.busFocus, primary.apiEnv)
  }
  return actions
}

export const SATELLITE_BUS_FOCUS_KEY = 'bifrost.satelliteBus.focus'
export const SATELLITE_API_ENV_KEY = 'bifrost.satelliteApi.env'

export function setSatelliteBusFocus(focus: ReadinessChipAction['busFocus'] | undefined): void {
  if (focus == null) {
    sessionStorage.removeItem(SATELLITE_BUS_FOCUS_KEY)
    return
  }
  sessionStorage.setItem(SATELLITE_BUS_FOCUS_KEY, focus)
}

export function peekSatelliteBusFocus(): ReadinessChipAction['busFocus'] | null {
  const raw = sessionStorage.getItem(SATELLITE_BUS_FOCUS_KEY)
  if (
    raw === 'rocket' ||
    raw === 'socket' ||
    raw === 'ingest' ||
    raw === 'monitor' ||
    raw === 'trade-apis' ||
    raw === 'workers' ||
    raw === 'cluster'
  ) {
    return raw
  }
  return null
}

export function clearSatelliteBusFocus(): void {
  sessionStorage.removeItem(SATELLITE_BUS_FOCUS_KEY)
}

/** Read and clear focus. Prefer peek + clear after successful scroll (avoids lost deep-links). */
export function consumeSatelliteBusFocus(): ReadinessChipAction['busFocus'] | null {
  const focus = peekSatelliteBusFocus()
  clearSatelliteBusFocus()
  return focus
}

export function setSatelliteApiEnv(env: ReadinessChipAction['apiEnv'] | undefined): void {
  if (env == null) {
    sessionStorage.removeItem(SATELLITE_API_ENV_KEY)
    return
  }
  sessionStorage.setItem(SATELLITE_API_ENV_KEY, env)
}

export function consumeSatelliteApiEnv(): ReadinessChipAction['apiEnv'] | null {
  const raw = sessionStorage.getItem(SATELLITE_API_ENV_KEY)
  sessionStorage.removeItem(SATELLITE_API_ENV_KEY)
  if (raw === 'dev' || raw === 'stg' || raw === 'prod') return raw
  return null
}
