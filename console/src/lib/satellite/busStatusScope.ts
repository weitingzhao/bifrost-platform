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

export type BusStatusSectionMeta = {
  id: string
  scope: BusStatusScope
  title: string
  description: string
}

export const BUS_STATUS_SCOPE_LEGEND: Record<BusStatusScope, string> = {
  rocket:
    'Rocket · Platform — shared infrastructure (data/ib-gateway @ redis-ib). Same bus for Dev, Stg, Prod satellites.',
  'trade-multi-env':
    'Trade satellite — all environments in one matrix (K3s Dev / Stg / Prod + Mac thin-client bridge).',
  'trade-single-env':
    'Trade satellite — follows Trade NS selector above (one namespace per view).',
  ground: 'Ground · Cluster — platform matrix L0, K8s domains, observability (not trade process semantics).',
}

export function tradeSingleEnvScope(env: BusStatusTradeEnv): string {
  const ns = BUS_TRADE_NS[env]
  const ingress = BUS_TRADE_INGRESS[env]
  return `${env.toUpperCase()} · ${ns} · :${ingress.split(':').pop()}`
}

export function tradeSingleEnvProbeSource(env: BusStatusTradeEnv): string {
  return `monitor /status + ops APIs @ ${BUS_TRADE_INGRESS[env]}`
}

/** Left accent bar class for BusPageGroup containers. */
export function busScopeGroupClass(scope: BusStatusScope): string {
  return `satellite-bus-group--${scope}`
}

export const BUS_SCOPE_SUMMARY_LABEL: Record<BusStatusScope, string> = {
  rocket: 'Rocket',
  'trade-multi-env': 'Socket · all envs',
  'trade-single-env': 'Trade · selected NS',
  ground: 'Ground · cluster',
}
