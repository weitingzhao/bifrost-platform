/**
 * Environment identity colors — SSOT for Trade NS / viewer seat chips.
 *
 * Distinct from health lamps (ok/fail/degraded): these mark *which env*,
 * not whether it is healthy.
 *
 * Aligns with ConsoleHeader ViewerEnvChip:
 *   DEV = info (sky) · STG = warning (amber) · PROD = danger (red)
 */

import type { DenseTagVariant } from '@bifrost/ui'

export type TradeEnvId = 'dev' | 'stg' | 'prod'

/** DenseTag / chip variant for env identity. */
export const TRADE_ENV_TAG_VARIANT: Record<TradeEnvId, DenseTagVariant> = {
  dev: 'info',
  stg: 'warning',
  prod: 'danger',
}

/**
 * Selected-state classes for Trade NS SegmentControl buttons.
 * Idle state stays muted (segment idle); only the active env is tinted.
 */
export const TRADE_ENV_SEGMENT_ACTIVE: Record<TradeEnvId, string> = {
  dev:
    'bg-sky-500/15 text-sky-800 dark:text-sky-300 font-semibold shadow-sm z-[1] ring-1 ring-inset ring-sky-500/45',
  stg:
    'bg-amber-500/15 text-amber-900 dark:text-amber-300 font-semibold shadow-sm z-[1] ring-1 ring-inset ring-amber-500/45',
  prod:
    'bg-red-500/15 text-red-800 dark:text-red-300 font-semibold shadow-sm z-[1] ring-1 ring-inset ring-red-500/45',
}

export function isTradeEnvId(v: string): v is TradeEnvId {
  return v === 'dev' || v === 'stg' || v === 'prod'
}

export function tradeEnvTagVariant(env: TradeEnvId): DenseTagVariant {
  return TRADE_ENV_TAG_VARIANT[env]
}

export function tradeEnvSegmentActiveClass(env: TradeEnvId): string {
  return TRADE_ENV_SEGMENT_ACTIVE[env]
}

/** Viewer seat chip (ConsoleHeader) — maps dev-local → same as Dev. */
export function viewerSeatTagVariant(
  env: 'dev' | 'stg' | 'prod' | 'dev-local',
): DenseTagVariant {
  if (env === 'prod') return TRADE_ENV_TAG_VARIANT.prod
  if (env === 'stg') return TRADE_ENV_TAG_VARIANT.stg
  return TRADE_ENV_TAG_VARIANT.dev
}
