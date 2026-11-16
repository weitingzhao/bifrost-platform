export type SatelliteBusTradeEnvFocus = 'dev' | 'stg' | 'prod'

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

