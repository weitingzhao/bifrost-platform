

export type ReadinessChipActionKind = 'navigate' | 'actuate'

export type ReadinessActuation =
  | { kind: 'rollout-restart'; namespace: string; deployment: string }
  | { kind: 'ib-gateway-reconnect' }

/**
 * Probe chip to watch after an L1 actuation (FixBar settle beat).
 * STG/PROD: tradeNs is bifrost-stg / bifrost-prod — never scale daemon up (D10).
 */
export type SettleWatchTarget = {
  /** Case-insensitive substring match against readiness chip label. */
  chipLabelIncludes: string
}

export type ReadinessChipAction = {
  kind: ReadinessChipActionKind
  label: string
  tabId?: string
  /** Satellite Bus section scroll target */
  busFocus?:
    | 'rocket'
    | 'socket'
    | 'ingest'
    | 'monitor'
    | 'trade-apis'
    | 'workers'
    | 'cluster'
    /** Trade daemon operate panel (Activity actuation deep-link) */
    | 'operate'
  /** Prefill Satellite Health env when navigating there */
  apiEnv?: 'dev' | 'stg' | 'prod'
  /** Prefill Satellite Health section (Probes | Runtime) */
  healthSection?: SatelliteHealthSection
  actuation?: ReadinessActuation
  requiresOperate?: boolean
  /** Optional settle-watch hint for FixBar four-beat (defaults to primary chip label). */
  settleWatchTarget?: SettleWatchTarget
}

export type ReadinessChipContext = {
  modeId: 'mission-launch' | 'daily-ops' | string
  env: 'stg' | 'prod' | 'platform-stg' | 'platform-prod'
  /**
   * Activity Feed / signal correlate scope. Defaults to `env`.
   * Use `shared` for Shared · IB bus so it does not collide with Trade Prod chips.
   */
  activityEnvScope?: string
}

export type SatelliteHealthSection = 'probes' | 'runtime'

export type ChipNavigation = {
  tabId: string
  busFocus?: ReadinessChipAction['busFocus']
  /** Prefill Satellite Health env segment when tabId is satellite-health. */
  apiEnv?: 'dev' | 'stg' | 'prod'
  /** Prefill Satellite Health view segment. */
  healthSection?: SatelliteHealthSection
}
export { consumeSatelliteApiEnv, consumeSatelliteHealthSection } from '@/domains/tenant/environment/navigation'
