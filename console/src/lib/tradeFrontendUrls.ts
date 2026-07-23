/**
 * Trade Monitoring SPA URLs per environment (Dev / Stg / Prod).
 *
 * Defaults match K8s Traefik Host ingress (see tradeEnvAccess / environments.yaml):
 *   DEV  → https://dev.trader.bifrost.lan/
 *   STG  → https://stg.trader.bifrost.lan/
 *   PROD → https://trader.bifrost.lan/
 *
 * Override with Vite env when needed (local compose, tunnel, etc.):
 *   VITE_TRADE_FRONTEND_URL_DEV / _STG / _PROD
 */

import type { FleetViewerEnv } from '@/lib/control-room/fleetSnapshot'
import { TRADE_ENV_ACCESS } from '@/lib/delivery/tradeEnvAccess'

export type TradeFrontendEnv = 'dev' | 'stg' | 'prod'

/** Alias used by sidebar peer chips. */
export type TradeFrontendEnvId = TradeFrontendEnv

export type TradeFrontendUrlMap = Record<TradeFrontendEnv, string>

export const TRADE_FRONTEND_ENV_OPTIONS: ReadonlyArray<{
  id: TradeFrontendEnv
  label: string
}> = [
  { id: 'dev', label: 'Dev' },
  { id: 'stg', label: 'Stg' },
  { id: 'prod', label: 'Prod' },
]

/** Map Fleet Viewer seat → Trade Monitoring env chip. */
export function tradeFrontendEnvFromViewer(
  viewer: FleetViewerEnv,
): TradeFrontendEnv {
  if (viewer === 'stg') return 'stg'
  if (viewer === 'prod') return 'prod'
  return 'dev'
}

/** Canonical LAN HTTPS hosts from tradeEnvAccess (UniFi DNS → Traefik). */
function ingressHttpsDefaults(): TradeFrontendUrlMap {
  const byTier = Object.fromEntries(
    TRADE_ENV_ACCESS.filter(e => e.ingressHost).map(e => [
      e.env.toLowerCase(),
      `https://${e.ingressHost}/`,
    ]),
  ) as Partial<TradeFrontendUrlMap>
  return {
    dev: byTier.dev ?? 'https://dev.trader.bifrost.lan/',
    stg: byTier.stg ?? 'https://stg.trader.bifrost.lan/',
    prod: byTier.prod ?? 'https://trader.bifrost.lan/',
  }
}

function trimUrl(raw: string | undefined): string | null {
  const t = (raw ?? '').trim()
  return t || null
}

/**
 * Resolve Dev / Stg / Prod Trade frontend base URLs.
 * Per-env Vite vars win; else K8s ingress HTTPS defaults.
 */
export function resolveTradeFrontendUrls(
  env: ImportMetaEnv = import.meta.env,
): TradeFrontendUrlMap {
  const defaults = ingressHttpsDefaults()
  return {
    dev: trimUrl(env.VITE_TRADE_FRONTEND_URL_DEV) ?? defaults.dev,
    stg: trimUrl(env.VITE_TRADE_FRONTEND_URL_STG) ?? defaults.stg,
    prod: trimUrl(env.VITE_TRADE_FRONTEND_URL_PROD) ?? defaults.prod,
  }
}

/** Single preferred URL (Fleet Viewer seat). */
export function resolveTradeFrontendUrl(
  preferred: TradeFrontendEnv,
  env: ImportMetaEnv = import.meta.env,
): string {
  return resolveTradeFrontendUrls(env)[preferred]
}

/** Chip / link colors — align with --color-env-* tokens in index.css. */
export function tradeEnvChipClass(
  env: TradeFrontendEnv,
  active: boolean,
): string {
  const palette: Record<TradeFrontendEnv, { idle: string; active: string }> = {
    dev: {
      idle: 'border-[color-mix(in_srgb,var(--color-env-dev)_45%,transparent)] text-[var(--color-env-dev)] hover:bg-[color-mix(in_srgb,var(--color-env-dev)_12%,transparent)]',
      active:
        'border-[var(--color-env-dev)] bg-[color-mix(in_srgb,var(--color-env-dev)_22%,transparent)] text-[var(--color-env-dev)] font-semibold',
    },
    stg: {
      idle: 'border-[color-mix(in_srgb,var(--color-env-stg)_45%,transparent)] text-[var(--color-env-stg)] hover:bg-[color-mix(in_srgb,var(--color-env-stg)_12%,transparent)]',
      active:
        'border-[var(--color-env-stg)] bg-[color-mix(in_srgb,var(--color-env-stg)_22%,transparent)] text-[var(--color-env-stg)] font-semibold',
    },
    prod: {
      idle: 'border-[color-mix(in_srgb,var(--color-env-prod)_45%,transparent)] text-[var(--color-env-prod)] hover:bg-[color-mix(in_srgb,var(--color-env-prod)_12%,transparent)]',
      active:
        'border-[var(--color-env-prod)] bg-[color-mix(in_srgb,var(--color-env-prod)_22%,transparent)] text-[var(--color-env-prod)] font-semibold',
    },
  }
  return active ? palette[env].active : palette[env].idle
}
