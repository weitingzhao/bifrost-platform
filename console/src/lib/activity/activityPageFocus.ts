import type { ActivityEvent } from '@/lib/activity/activityTypes'
import { setSatelliteBusFocus } from '@/lib/task-mode/readinessChipActions'

/** Workload name to highlight on Satellite Bus (e.g. account-sync). */
export const SATELLITE_BUS_WORKLOAD_FOCUS_KEY = 'bifrost.satelliteBus.workloadFocus'
/** Prefill Trade NS segment when Activity deep-links into Bus Status. */
export const SATELLITE_BUS_TRADE_ENV_FOCUS_KEY = 'bifrost.satelliteBus.tradeEnvFocus'

export type SatelliteBusTradeEnvFocus = 'dev' | 'stg' | 'prod'

export function parseActivityTarget(
  target: string | undefined,
): { namespace?: string; workload?: string } {
  if (target == null || target === '') return {}
  const parts = target.split('/').filter(Boolean)
  if (parts.length >= 2) {
    return { namespace: parts[0], workload: parts[parts.length - 1] }
  }
  return { workload: parts[0] }
}

export function tradeEnvFromNamespace(namespace: string | undefined): SatelliteBusTradeEnvFocus | null {
  if (namespace === 'bifrost-prod') return 'prod'
  if (namespace === 'bifrost-stg') return 'stg'
  if (namespace === 'bifrost-dev') return 'dev'
  return null
}

/** Map K8s workload name → Runtime Consumers row id. */
export function workloadToRuntimeConsumerId(workload: string | undefined): string | null {
  if (workload == null || workload === '') return null
  if (workload === 'account-sync') return 'account-sync'
  if (workload === 'daemon') return 'trading_engine'
  return workload
}

export function setSatelliteBusWorkloadFocus(workload: string | undefined): void {
  if (workload == null || workload === '') {
    sessionStorage.removeItem(SATELLITE_BUS_WORKLOAD_FOCUS_KEY)
    return
  }
  sessionStorage.setItem(SATELLITE_BUS_WORKLOAD_FOCUS_KEY, workload)
}

export function peekSatelliteBusWorkloadFocus(): string | null {
  const raw = sessionStorage.getItem(SATELLITE_BUS_WORKLOAD_FOCUS_KEY)
  return raw != null && raw !== '' ? raw : null
}

export function clearSatelliteBusWorkloadFocus(): void {
  sessionStorage.removeItem(SATELLITE_BUS_WORKLOAD_FOCUS_KEY)
}

export function setSatelliteBusTradeEnvFocus(env: SatelliteBusTradeEnvFocus | undefined): void {
  if (env == null) {
    sessionStorage.removeItem(SATELLITE_BUS_TRADE_ENV_FOCUS_KEY)
    return
  }
  sessionStorage.setItem(SATELLITE_BUS_TRADE_ENV_FOCUS_KEY, env)
}

export function peekSatelliteBusTradeEnvFocus(): SatelliteBusTradeEnvFocus | null {
  const raw = sessionStorage.getItem(SATELLITE_BUS_TRADE_ENV_FOCUS_KEY)
  if (raw === 'dev' || raw === 'stg' || raw === 'prod') return raw
  return null
}

export function clearSatelliteBusTradeEnvFocus(): void {
  sessionStorage.removeItem(SATELLITE_BUS_TRADE_ENV_FOCUS_KEY)
}

/**
 * Before navigating to satellite-bus from Activity: set section + workload + Trade NS.
 * Page consumes these on mount / data ready.
 */
export function prepareSatelliteBusActivityFocus(ev: ActivityEvent): void {
  const { namespace, workload } = parseActivityTarget(ev.target)
  const env = tradeEnvFromNamespace(namespace)
  if (env != null) setSatelliteBusTradeEnvFocus(env)
  setSatelliteBusFocus('operate')
  if (workload != null) setSatelliteBusWorkloadFocus(workload)
}

export function isActivityInFlight(ev: ActivityEvent): boolean {
  return ev.phase === 'requested' || ev.phase === 'applying'
}
