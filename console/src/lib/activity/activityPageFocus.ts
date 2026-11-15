import type { ActivityEvent } from '@/lib/activity/activityTypes'
import '@/lib/task-mode/readinessChipActions'

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

export function isActivityInFlight(ev: ActivityEvent): boolean {
  return ev.phase === 'requested' || ev.phase === 'applying'
}
