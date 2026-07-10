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
  actuation?: ReadinessActuation
  requiresOperate?: boolean
}

export type ReadinessChipContext = {
  modeId: 'rocket-launch' | 'satellite-deploy' | 'daily-ops' | string
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

/** Primary drill-down when operator clicks a failing readiness chip. */
export function primaryChipNavigation(
  chipLabel: string,
  ctx: ReadinessChipContext,
): { tabId: string; busFocus?: ReadinessChipAction['busFocus'] } | null {
  const label = chipLabelNorm(chipLabel)
  if (label.includes('ib socket')) {
    return { tabId: 'satellite-bus', busFocus: 'rocket' }
  }
  if (label.includes('pg / redis')) {
    return { tabId: 'satellite-bus', busFocus: 'cluster' }
  }
  if (chipMatchesTradeApis(label)) {
    return { tabId: 'satellite-bus', busFocus: 'trade-apis' }
  }
  if (label.includes('trade prod matrix') || label.includes('prod matrix')) {
    return { tabId: 'satellite-bus', busFocus: 'socket' }
  }
  if (label.includes('k8s')) {
    return { tabId: 'cluster' }
  }
  if (label.includes('ci/cd') || label.includes('gate') || label.includes('supply')) {
    return { tabId: ctx.modeId === 'satellite-deploy' ? 'trade-release' : 'platform-release' }
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
  ) => {
    actions.push({ kind: 'navigate', label: actionLabel, tabId, busFocus })
  }

  if (label.includes('ib socket') && tradeNs != null) {
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
    pushNavigate('satellite-bus', 'API reachability', 'trade-apis')
    pushNavigate('api-health', 'API health')
    return actions
  }

  if (label.includes('trade prod matrix') || label.includes('prod matrix')) {
    pushNavigate('satellite-bus', 'Socket matrix', 'socket')
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
    pushNavigate(
      ctx.modeId === 'satellite-deploy' ? 'trade-release' : 'platform-release',
      'Release pipeline',
    )
    return actions
  }

  if (label.includes('self-health')) {
    pushNavigate('platform-release', 'Platform release')
    return actions
  }

  const primary = primaryChipNavigation(chipLabel, ctx)
  if (primary != null) {
    pushNavigate(primary.tabId, 'Open details', primary.busFocus)
  }
  return actions
}

export const SATELLITE_BUS_FOCUS_KEY = 'bifrost.satelliteBus.focus'

export function setSatelliteBusFocus(focus: ReadinessChipAction['busFocus'] | undefined): void {
  if (focus == null) {
    sessionStorage.removeItem(SATELLITE_BUS_FOCUS_KEY)
    return
  }
  sessionStorage.setItem(SATELLITE_BUS_FOCUS_KEY, focus)
}

export function consumeSatelliteBusFocus(): ReadinessChipAction['busFocus'] | null {
  const raw = sessionStorage.getItem(SATELLITE_BUS_FOCUS_KEY)
  sessionStorage.removeItem(SATELLITE_BUS_FOCUS_KEY)
  if (raw === 'rocket' || raw === 'socket' || raw === 'ingest' || raw === 'monitor' || raw === 'trade-apis' || raw === 'workers' || raw === 'cluster') {
    return raw
  }
  return null
}
