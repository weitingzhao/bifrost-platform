/**
 * Safe Grafana deep-link builder.
 * - Rejects non-http(s) bases
 * - Encodes query params
 * - Never invents UIDs that are marked unavailable
 */

import { getDashboard } from './dashboardCatalog'
import type { ObservabilityEnvId } from './types'
import { TRADE_NS } from './signalRegistry'

export type GrafanaLinkContext = {
  grafanaBaseUrl: string | null | undefined
  dashboardId: string
  env?: ObservabilityEnvId | 'all'
  namespace?: string
  service?: string
  instance?: string
  /** Absolute ms timestamps; defaults to last 1h when omitted. */
  fromMs?: number
  toMs?: number
  /** Alert start — used as `from` when provided. */
  alertStartMs?: number
  /** Declared available UIDs from live probe (optional). Missing → use catalog uid presence. */
  availableUids?: Set<string> | string[]
}

const SAFE_BASE = /^https?:\/\//i

export function normalizeGrafanaBase(raw: string | null | undefined): string | null {
  if (raw == null) return null
  const trimmed = raw.trim()
  if (trimmed === '' || !SAFE_BASE.test(trimmed)) return null
  return trimmed.replace(/\/+$/, '')
}

/**
 * Namespace priority for `var-namespace`:
 * 1. Explicit ctx.namespace (caller override)
 * 2. Catalog defaultNamespace (Ground/IB → data, Agent → platform NS)
 * 3. TRADE_NS[env] when env is a Trade env (Satellite)
 */
function resolveNamespace(
  env: ObservabilityEnvId | 'all' | undefined,
  namespace: string | undefined,
  catalogDefault: string | undefined,
): string | undefined {
  if (namespace != null && namespace.trim() !== '') return namespace.trim()
  if (catalogDefault != null && catalogDefault.trim() !== '') return catalogDefault.trim()
  if (env === 'dev' || env === 'stg' || env === 'prod') return TRADE_NS[env]
  return undefined
}

/**
 * Build a Grafana dashboard URL or null when unavailable / unsafe.
 */
export function buildGrafanaDashboardUrl(ctx: GrafanaLinkContext): string | null {
  const base = normalizeGrafanaBase(ctx.grafanaBaseUrl)
  if (base == null) return null

  const dash = getDashboard(ctx.dashboardId)
  if (dash == null || dash.uid == null || dash.uid.trim() === '') return null

  if (ctx.availableUids != null) {
    const set = ctx.availableUids instanceof Set ? ctx.availableUids : new Set(ctx.availableUids)
    if (!set.has(dash.uid)) return null
  }

  const path = `/d/${encodeURIComponent(dash.uid)}/${encodeURIComponent(dash.slug)}`
  const params = new URLSearchParams()
  params.set('orgId', '1')

  const now = Date.now()
  const from =
    ctx.alertStartMs != null && Number.isFinite(ctx.alertStartMs)
      ? String(ctx.alertStartMs)
      : ctx.fromMs != null
        ? String(ctx.fromMs)
        : String(now - 60 * 60_000)
  const to = ctx.toMs != null ? String(ctx.toMs) : 'now'
  params.set('from', from)
  params.set('to', to)

  const ns = resolveNamespace(ctx.env, ctx.namespace, dash.defaultNamespace)
  if (ns != null) {
    params.set('var-namespace', ns)
  }
  if (ctx.env != null && ctx.env !== 'all' && ctx.env !== 'shared') {
    params.set('var-env', ctx.env)
  }
  if (ctx.service != null && ctx.service.trim() !== '') {
    params.set('var-service', ctx.service.trim())
  }
  if (ctx.instance != null && ctx.instance.trim() !== '') {
    params.set('var-instance', ctx.instance.trim())
  }

  return `${base}${path}?${params.toString()}`
}

/** True when catalog entry has a uid (may still lack live Grafana). */
export function isDashboardCatalogAvailable(dashboardId: string): boolean {
  const dash = getDashboard(dashboardId)
  return dash != null && dash.uid != null && dash.uid.trim() !== ''
}
