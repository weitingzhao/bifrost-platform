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
  /** Grafana theme for solo embeds; default dark. */
  theme?: 'dark' | 'light'
}

export type GrafanaSoloPanelContext = GrafanaLinkContext & {
  /** Override catalog soloPanel.panelId when needed. */
  panelId?: number
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

function resolveDashboardEntry(ctx: GrafanaLinkContext) {
  const base = normalizeGrafanaBase(ctx.grafanaBaseUrl)
  if (base == null) return null

  const dash = getDashboard(ctx.dashboardId)
  if (dash == null || dash.uid == null || dash.uid.trim() === '') return null

  if (ctx.availableUids != null) {
    const set = ctx.availableUids instanceof Set ? ctx.availableUids : new Set(ctx.availableUids)
    if (!set.has(dash.uid)) return null
  }

  return { base, dash }
}

function appendSharedGrafanaParams(
  params: URLSearchParams,
  ctx: GrafanaLinkContext,
  dash: NonNullable<ReturnType<typeof getDashboard>>,
): void {
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

  if (!dash.suppressNamespace) {
    const ns = resolveNamespace(ctx.env, ctx.namespace, dash.defaultNamespace)
    if (ns != null) {
      params.set('var-namespace', ns)
    }
  }
  // Shared boards (Ground/IB) and Rocket stock (suppressEnv) ignore seat env.
  if (
    !dash.suppressEnv &&
    ctx.env != null &&
    ctx.env !== 'all' &&
    ctx.env !== 'shared' &&
    dash.env !== 'shared'
  ) {
    params.set('var-env', ctx.env)
  }
  if (ctx.service != null && ctx.service.trim() !== '') {
    params.set('var-service', ctx.service.trim())
  }
  if (ctx.instance != null && ctx.instance.trim() !== '') {
    params.set('var-instance', ctx.instance.trim())
  }
}

/**
 * Build a Grafana dashboard URL or null when unavailable / unsafe.
 */
export function buildGrafanaDashboardUrl(ctx: GrafanaLinkContext): string | null {
  const resolved = resolveDashboardEntry(ctx)
  if (resolved == null) return null

  const { base, dash } = resolved
  const path = `/d/${encodeURIComponent(dash.uid!)}/${encodeURIComponent(dash.slug)}`
  const params = new URLSearchParams()
  appendSharedGrafanaParams(params, ctx, dash)
  return `${base}${path}?${params.toString()}`
}

/**
 * Build a Grafana solo-panel embed URL (`/d-solo/...`) or null when unavailable.
 * Requires catalog `soloPanel` (or ctx.panelId) and a valid uid.
 */
export function buildGrafanaSoloPanelUrl(ctx: GrafanaSoloPanelContext): string | null {
  const resolved = resolveDashboardEntry(ctx)
  if (resolved == null) return null

  const { base, dash } = resolved
  const panelId = ctx.panelId ?? dash.soloPanel?.panelId
  if (panelId == null || !Number.isFinite(panelId) || panelId <= 0) return null

  const path = `/d-solo/${encodeURIComponent(dash.uid!)}/${encodeURIComponent(dash.slug)}`
  const params = new URLSearchParams()
  appendSharedGrafanaParams(params, ctx, dash)
  params.set('panelId', String(panelId))
  params.set('theme', ctx.theme === 'light' ? 'light' : 'dark')
  params.set('refresh', '30s')
  return `${base}${path}?${params.toString()}`
}

/** True when catalog entry has a uid (may still lack live Grafana). */
export function isDashboardCatalogAvailable(dashboardId: string): boolean {
  const dash = getDashboard(dashboardId)
  return dash != null && dash.uid != null && dash.uid.trim() !== ''
}
