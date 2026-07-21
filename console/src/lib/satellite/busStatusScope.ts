/** Bus Status page — section scope legend (Rocket vs Trade satellite vs ground). */

export type BusStatusTradeEnv = 'dev' | 'stg' | 'prod'

export const BUS_TRADE_NS: Record<BusStatusTradeEnv, string> = {
  dev: 'bifrost-dev',
  stg: 'bifrost-stg',
  prod: 'bifrost-prod',
}

/** K3s trade ingress used by platform-api bus-deep pull. */
export const BUS_TRADE_INGRESS: Record<BusStatusTradeEnv, string> = {
  dev: '192.168.10.73:30882',
  stg: '192.168.10.73:30880',
  prod: '192.168.10.70',
}

export type BusStatusScope = 'rocket' | 'trade-multi-env' | 'trade-single-env' | 'ground'

export function tradeSingleEnvScope(env: BusStatusTradeEnv): string {
  const ns = BUS_TRADE_NS[env]
  const ingress = BUS_TRADE_INGRESS[env]
  return `${env.toUpperCase()} · ${ns} · :${ingress.split(':').pop()}`
}

export function tradeSingleEnvProbeSource(env: BusStatusTradeEnv): string {
  return `monitor /status + ops APIs @ ${BUS_TRADE_INGRESS[env]}`
}

/** Left accent bar class for Bus Status section containers (scope color, not health). */
export function busScopeGroupClass(scope: BusStatusScope): string {
  return `satellite-bus-group--${scope}`
}
