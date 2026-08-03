/**
 * Observability hub view model — pure aggregation over live probes.
 * Single source of truth for overall / domain verdicts.
 */

import type { AgentBridgeResponse } from '@/api/agentTypes'
import type { ClusterMetricsResponse, ClusterObservabilityResponse, TelemetryMetricResult } from '@/api/clusterTypes'
import type { SelfHealthResponse } from '@/api/matrixTypes'
import type { RemediationHealthResponse } from '@/api/remediationTypes'
import type { IbGatewayStatusResponse } from '@/api/satelliteBusTypes'
import type { SystemDomainId } from '@/lib/architecture/systemDomainCatalog'
import {
  annotateStandbyAlerts,
  hostMatchesStandbyNode,
  mapAlerts,
  type RawAlertInput,
  type StandbyNodeRef,
} from './alertMapping'
import { GRAFANA_DASHBOARD_CATALOG } from './dashboardCatalog'
import {
  buildGrafanaDashboardUrl,
  buildGrafanaSoloPanelUrl,
  isDashboardCatalogAvailable,
} from './grafanaUrlBuilder'
import {
  METRIC_TO_SIGNAL,
  OBSERVABILITY_DOMAIN_ORDER,
  SIGNAL_REGISTRY,
  SIGNAL_STALE_MS,
  TARGET_DOMAIN_HINTS,
  TRADE_NS,
  getSignalDef,
} from './signalRegistry'
import {
  buildAttentionItems,
  buildDomainHealth,
  buildSystemVerdict,
} from './verdictAggregation'
import type {
  EvaluatedSignal,
  GoldenSignalRow,
  ObservabilityEnvId,
  ObservabilityViewModel,
  ScrapeTargetView,
  SelectedDomainDetail,
  SignalState,
} from './types'

/** Target shape for VM input — mirrors API without importing heavy modules in tests. */
export type TelemetryTargetLike = {
  labels?: Record<string, string>
  scrape_pool?: string
  scrape_url?: string
  health?: string
  last_error?: string
  last_scrape?: string
  last_scrape_duration?: number
}

export type BusHealthInput = {
  health: 'healthy' | 'degraded' | 'unavailable' | 'unknown'
  topReason: string
}

export type ObservabilityViewModelInput = {
  selectedEnv: 'dev' | 'stg' | 'prod'
  selectedDomain: SystemDomainId
  nowMs?: number
  observability?: ClusterObservabilityResponse | null
  metrics?: ClusterMetricsResponse | null
  telemetryMetrics?: TelemetryMetricResult[] | null
  telemetryError?: string | null
  alerts?: RawAlertInput[] | null
  alertsError?: string | null
  targets?: TelemetryTargetLike[] | null
  targetsError?: string | null
  bus?: BusHealthInput | null
  ibGateway?: IbGatewayStatusResponse | null
  remediation?: RemediationHealthResponse | null
  agentBridge?: AgentBridgeResponse | null
  selfHealth?: SelfHealthResponse | null
  /**
   * Elastic standby hosts (`elastic_mode === 'standby'` only — not degraded).
   * Used to neutralize Expected-Off node alerts and TargetDown scrapes.
   */
  standbyNodes?: StandbyNodeRef[]
  /** Declared Grafana UIDs known to exist; omit to trust catalog uid presence. */
  availableGrafanaUids?: string[]
}

function metricById(
  metrics: TelemetryMetricResult[] | null | undefined,
  id: string,
): TelemetryMetricResult | undefined {
  return metrics?.find(m => m.id === id)
}

function evaluateMetricSignal(
  signalId: string,
  metrics: TelemetryMetricResult[] | null | undefined,
  telemetryError: string | null | undefined,
  env: ObservabilityEnvId,
): EvaluatedSignal | null {
  const def = getSignalDef(signalId)
  if (def == null) return null

  if (telemetryError != null && /503|not configured/i.test(telemetryError)) {
    return {
      def,
      state: 'not_observed',
      summary: 'Prometheus not configured',
      env,
      evidence: telemetryError,
    }
  }

  const metricIds = Object.entries(METRIC_TO_SIGNAL)
    .filter(([, sid]) => sid === signalId)
    .map(([mid]) => mid)
  if (metricIds.length === 0) return null

  const found = metricIds.map(id => metricById(metrics, id)).filter(Boolean) as TelemetryMetricResult[]
  if (found.length === 0) {
    return {
      def,
      state: 'unknown',
      summary: 'Metric not in overview response',
      env,
    }
  }
  if (found.some(m => m.status === 'error')) {
    // Query error is an observability-layer fault, not evidence of business
    // degradation — report UNKNOWN, never DEGRADED.
    return {
      def,
      state: 'unknown',
      summary: found.find(m => m.status === 'error')?.detail ?? 'Metric query error',
      env,
      evidence: found.map(m => `${m.id}:${m.status}`).join(', '),
    }
  }
  if (found.every(m => m.status === 'empty' || m.points.length === 0)) {
    return {
      def,
      state: 'unknown',
      summary: 'No scrape series yet (targets may still be coming up)',
      env,
    }
  }

  // Error-rate threshold: any service > 5% → degraded; > 20% → critical
  if (signalId === 'satellite.api-error-rate') {
    const err = metricById(metrics, 'api_error_rate')
    const max = Math.max(0, ...(err?.points.map(p => p.value) ?? [0]))
    if (max > 0.2) {
      return { def, state: 'critical', summary: `5xx rate ${(max * 100).toFixed(1)}%`, env }
    }
    if (max > 0.05) {
      return { def, state: 'degraded', summary: `5xx rate ${(max * 100).toFixed(1)}%`, env }
    }
  }

  return {
    def,
    state: 'healthy',
    summary: found.map(m => `${m.title} ok`).join('; '),
    env,
  }
}

function evaluateLayerB(
  obs: ClusterObservabilityResponse | null | undefined,
  env: ObservabilityEnvId,
): EvaluatedSignal[] {
  const layerDef = getSignalDef('rocket.layer-b')!
  const promDef = getSignalDef('rocket.prometheus-reachable')!
  if (obs == null) {
    return [
      { def: layerDef, state: 'unknown', summary: 'Layer B probe missing', env },
      { def: promDef, state: 'unknown', summary: 'Prometheus probe missing', env },
    ]
  }
  let layerState: SignalState
  let layerSummary: string
  switch (obs.layer_b_status) {
    case 'ready':
      layerState = 'healthy'
      layerSummary = 'Layer B ready'
      break
    case 'partial':
      layerState = 'degraded'
      layerSummary = 'Layer B partial'
      break
    default:
      layerState = 'not_observed'
      layerSummary = 'Layer B not installed'
  }
  const promOk =
    obs.prometheus_url != null &&
    obs.prometheus_url !== '' &&
    (obs.layer_b_status === 'ready' || obs.layer_b_status === 'partial')
  return [
    { def: layerDef, state: layerState, summary: layerSummary, env: 'shared', evidence: obs.detail },
    {
      def: promDef,
      state: promOk ? 'healthy' : layerState === 'not_observed' ? 'not_observed' : 'unknown',
      summary: promOk ? 'Prometheus URL configured' : 'Prometheus not reachable/configured',
      env: 'shared',
    },
  ]
}

function evaluateClusterEvidence(
  metrics: ClusterMetricsResponse | null | undefined,
): EvaluatedSignal[] {
  const cpuDef = getSignalDef('rocket.cluster-cpu')!
  const memDef = getSignalDef('rocket.cluster-memory')!
  if (metrics == null) {
    return [
      { def: cpuDef, state: 'unknown', summary: 'metrics-server probe missing', env: 'shared' },
      { def: memDef, state: 'unknown', summary: 'metrics-server probe missing', env: 'shared' },
    ]
  }
  const cpuState: SignalState = metrics.metrics_server_available
    ? metrics.cpu_usage_percent != null && metrics.cpu_usage_percent > 90
      ? 'degraded'
      : 'healthy'
    : 'not_observed'
  const memState: SignalState = metrics.metrics_server_available
    ? metrics.memory_usage_percent != null && metrics.memory_usage_percent > 90
      ? 'degraded'
      : 'healthy'
    : 'not_observed'
  return [
    {
      def: cpuDef,
      state: cpuState,
      summary:
        metrics.cpu_usage_percent != null
          ? `CPU ${metrics.cpu_usage_percent.toFixed(0)}%`
          : metrics.metrics_server_available
            ? 'CPU available'
            : 'metrics-server missing',
      env: 'shared',
    },
    {
      def: memDef,
      state: memState,
      summary:
        metrics.memory_usage_percent != null
          ? `Memory ${metrics.memory_usage_percent.toFixed(0)}%`
          : metrics.metrics_server_available
            ? 'Memory available'
            : 'metrics-server missing',
      env: 'shared',
    },
  ]
}

function mapTargets(targets: TelemetryTargetLike[] | null | undefined): ScrapeTargetView[] {
  if (targets == null) return []
  return targets.map((t, i) => {
    const labels = t.labels ?? {}
    const job = labels.job ?? t.scrape_pool ?? 'unknown'
    const instance = labels.instance ?? labels.pod ?? t.scrape_url ?? `target-${i}`
    const ns = labels.namespace ?? labels.exported_namespace
    const node = labels.node
    const pod = labels.pod
    const metricsPath = labels.metrics_path
    const blob = `${job} ${instance} ${ns ?? ''} ${node ?? ''} ${metricsPath ?? ''} ${t.scrape_pool ?? ''}`
    const hint =
      TARGET_DOMAIN_HINTS.find(h => h.match.test(blob)) ??
      ({ domain: 'rocket' as SystemDomainId, role: 'evidence' as const, envHint: 'shared' as const })
    let env: ObservabilityEnvId = hint.envHint ?? 'shared'
    if (ns === 'bifrost-dev') env = 'dev'
    else if (ns === 'bifrost-stg') env = 'stg'
    else if (ns === 'bifrost-prod') env = 'prod'

    const healthRaw = (t.health ?? 'unknown').toLowerCase()
    const health: ScrapeTargetView['health'] =
      healthRaw === 'up' ? 'up' : healthRaw === 'down' ? 'down' : 'unknown'

    const pathKey = metricsPath != null && metricsPath !== '' ? metricsPath : ''
    return {
      id: `${job}:${instance}:${pathKey || i}`,
      job,
      instance,
      node,
      pod,
      metricsPath,
      namespace: ns,
      health,
      lastScrape: t.last_scrape,
      lastError: t.last_error,
      role: hint.role,
      domain: hint.domain,
      // Target env comes from its own namespace / hint — independent of the
      // selected env (filtering by selection happens at render time).
      env,
    }
  })
}

function scrapeHealthRank(h: ScrapeTargetView['health']): number {
  if (h === 'down') return 0
  if (h === 'unknown') return 1
  return 2
}

/** DOWN first, then job / node / path — makes Rocket kubelet rows scannable. */
export function sortScrapeTargets(targets: ScrapeTargetView[]): ScrapeTargetView[] {
  return [...targets].sort((a, b) => {
    const hr = scrapeHealthRank(a.health) - scrapeHealthRank(b.health)
    if (hr !== 0) return hr
    const j = a.job.localeCompare(b.job)
    if (j !== 0) return j
    const na = a.node ?? a.pod ?? a.instance
    const nb = b.node ?? b.pod ?? b.instance
    const n = na.localeCompare(nb)
    if (n !== 0) return n
    return (a.metricsPath ?? '').localeCompare(b.metricsPath ?? '')
  })
}

/** Short path label: /metrics → metrics, /metrics/cadvisor → cadvisor. */
export function shortMetricsPath(path?: string): string | undefined {
  if (path == null || path === '') return undefined
  if (path === '/metrics') return 'metrics'
  const nested = path.match(/\/metrics\/(.+)$/)
  if (nested?.[1]) return nested[1]
  return path.replace(/^\//, '')
}

function isStandbyScrapeTarget(t: ScrapeTargetView, standbyNodes: StandbyNodeRef[]): boolean {
  if (standbyNodes.length === 0) return false
  const job = t.job.toLowerCase()
  const looksLikeNode =
    /node-exporter|node_exporter|kubelet/i.test(job) || /:\d{2,5}$/.test(t.instance)
  if (!looksLikeNode) return false
  return hostMatchesStandbyNode(t.instance, standbyNodes)
}

function evaluateScrapeTargets(
  targets: ScrapeTargetView[],
  targetsError: string | null | undefined,
  standbyNodes: StandbyNodeRef[] = [],
): EvaluatedSignal {
  const def = getSignalDef('rocket.scrape-targets')!
  if (targetsError != null && /503|not configured/i.test(targetsError)) {
    return {
      def,
      state: 'not_observed',
      summary: 'Targets API unavailable — Prometheus not configured',
      env: 'shared',
      evidence: targetsError,
    }
  }
  if (targetsError != null) {
    return { def, state: 'unknown', summary: targetsError, env: 'shared' }
  }
  const requiredish = targets.filter(t => t.role === 'required')
  const pool = requiredish.length > 0 ? requiredish : targets
  if (pool.length === 0) {
    return { def, state: 'unknown', summary: 'No active scrape targets returned', env: 'shared' }
  }
  // Elastic standby node scrapes are EXPECTED OFF — exclude from DOWN rollup.
  const scored = pool.filter(t => !(t.health === 'down' && isStandbyScrapeTarget(t, standbyNodes)))
  const standbyDown = pool.length - scored.length
  const down = scored.filter(t => t.health === 'down')
  if (down.length > 0) {
    return {
      def,
      state: down.length === scored.length ? 'critical' : 'degraded',
      summary: `${down.length}/${scored.length} targets DOWN`,
      env: 'shared',
      linkedIds: down.map(t => t.id),
    }
  }
  if (scored.length === 0) {
    return {
      def,
      state: 'expected_off',
      summary:
        standbyDown > 0
          ? `${standbyDown} standby node scrape(s) expected off`
          : 'No scored scrape targets',
      env: 'shared',
    }
  }
  if (scored.every(t => t.health === 'unknown')) {
    return { def, state: 'unknown', summary: 'Target health unknown', env: 'shared' }
  }
  const up = scored.filter(t => t.health === 'up').length
  const standbyNote = standbyDown > 0 ? ` · ${standbyDown} standby expected off` : ''
  return {
    def,
    state: 'healthy',
    summary: `${up}/${scored.length} targets UP${standbyNote}`,
    env: 'shared',
  }
}

function evaluateOptionalNone(signalId: string): EvaluatedSignal {
  const def = getSignalDef(signalId)!
  return {
    def,
    state: 'not_observed',
    summary: def.description ?? 'No reliable runtime contract',
    env: 'shared',
  }
}

function evaluateBus(bus: BusHealthInput | null | undefined, env: ObservabilityEnvId): EvaluatedSignal {
  const def = getSignalDef('satellite.bus-health')!
  if (bus == null) {
    return { def, state: 'unknown', summary: 'Bus probe not provided', env }
  }
  const state: SignalState =
    bus.health === 'healthy'
      ? 'healthy'
      : bus.health === 'degraded'
        ? 'degraded'
        : bus.health === 'unavailable'
          ? 'critical'
          : 'unknown'
  return { def, state, summary: bus.topReason, env }
}

function ibGatewayApiVerdict(
  def: NonNullable<ReturnType<typeof getSignalDef>>,
  ib: IbGatewayStatusResponse | null | undefined,
): EvaluatedSignal {
  if (ib == null) {
    return { def, state: 'unknown', summary: 'IB Gateway status missing', env: 'shared' }
  }
  // Best-effort across known shapes — do not invent health.
  const reach =
    (ib as { reachability?: string }).reachability ??
    (ib as { status?: string }).status ??
    (ib as { overall?: string }).overall
  if (reach == null) {
    return { def, state: 'unknown', summary: 'IB Gateway status shape unrecognized', env: 'shared' }
  }
  const r = String(reach).toLowerCase()
  if (r === 'ok' || r === 'healthy' || r === 'connected' || r === 'ready') {
    return { def, state: 'healthy', summary: `IB Gateway ${reach}`, env: 'shared' }
  }
  if (r === 'degraded' || r === 'partial') {
    return { def, state: 'degraded', summary: `IB Gateway ${reach}`, env: 'shared' }
  }
  if (r === 'fail' || r === 'down' || r === 'unavailable' || r === 'disconnected') {
    return { def, state: 'critical', summary: `IB Gateway ${reach}`, env: 'shared' }
  }
  return { def, state: 'unknown', summary: `IB Gateway ${reach}`, env: 'shared' }
}

function evaluateIbGateway(
  ib: IbGatewayStatusResponse | null | undefined,
  metrics: TelemetryMetricResult[] | null | undefined,
): EvaluatedSignal {
  const def = getSignalDef('subcontractors.ib-gateway')!
  const result = ibGatewayApiVerdict(def, ib)
  // Prometheus `ib_gateway_up` is evidence-only: the ib_gateway API probe above
  // stays the single verdict source — never a second judge.
  const up = metricById(metrics, 'ib_gateway_up')
  if (up != null && up.status === 'ok' && up.points.length > 0) {
    const promNote = `prometheus ib_gateway_up=${up.points.map(p => p.value).join(',')}`
    return {
      ...result,
      evidence: result.evidence != null ? `${result.evidence}; ${promNote}` : promNote,
    }
  }
  return result
}

function evaluateRemediation(
  rem: RemediationHealthResponse | null | undefined,
): EvaluatedSignal {
  const def = getSignalDef('engineer.remediation-runner')!
  if (rem == null) {
    return { def, state: 'unknown', summary: 'Remediation health missing', env: 'shared' }
  }
  const status =
    (rem as { status?: string }).status ??
    (rem as { reachability?: string }).reachability ??
    (rem as { healthy?: boolean }).healthy
  if (typeof status === 'boolean') {
    return {
      def,
      state: status ? 'healthy' : 'degraded',
      summary: status ? 'Remediation runner healthy' : 'Remediation runner unhealthy',
      env: 'shared',
    }
  }
  if (status == null) {
    return { def, state: 'unknown', summary: 'Remediation status unrecognized', env: 'shared' }
  }
  const r = String(status).toLowerCase()
  if (r === 'ok' || r === 'healthy' || r === 'ready') {
    return { def, state: 'healthy', summary: `Remediation ${status}`, env: 'shared' }
  }
  if (r === 'degraded' || r === 'busy') {
    return { def, state: 'degraded', summary: `Remediation ${status}`, env: 'shared' }
  }
  if (r === 'fail' || r === 'down' || r === 'unavailable') {
    return { def, state: 'critical', summary: `Remediation ${status}`, env: 'shared' }
  }
  return { def, state: 'unknown', summary: `Remediation ${status}`, env: 'shared' }
}

function evaluateAgentBridge(
  bridge: AgentBridgeResponse | null | undefined,
  self: SelfHealthResponse | null | undefined,
): EvaluatedSignal {
  const def = getSignalDef('engineer.agent-bridge')!
  const viewer = (self?.viewer_env ?? '').toLowerCase()
  if (viewer === 'prod' || viewer === 'stg') {
    return {
      def,
      state: 'not_observed',
      summary: 'Agent bridge not observed on this viewer seat (Mac-adjacent only)',
      env: 'shared',
    }
  }
  if (bridge == null) {
    return { def, state: 'unknown', summary: 'Agent bridge probe missing', env: 'shared' }
  }
  const status = bridge.satellite_probe_bridge?.status
  if (status == null) {
    return { def, state: 'not_observed', summary: 'No satellite probe bridge field', env: 'shared' }
  }
  if (status === 'ok') {
    return { def, state: 'healthy', summary: 'Probe bridge ok', env: 'shared' }
  }
  if (status === 'degraded') {
    return { def, state: 'degraded', summary: 'Probe bridge degraded', env: 'shared' }
  }
  return { def, state: 'unknown', summary: `Probe bridge ${status}`, env: 'shared' }
}

function goldenFromMetrics(metrics: TelemetryMetricResult[] | null | undefined): GoldenSignalRow[] {
  const ids = [
    'api_request_rate',
    'api_latency_p99',
    'api_error_rate',
    'redis_memory_bytes',
    'pg_connections',
  ]
  return ids.map(id => {
    const m = metricById(metrics, id)
    if (m == null) {
      return { id, label: id, status: 'unknown' as const, valueLabel: '—' }
    }
    if (m.status === 'error') {
      return { id, label: m.title, unit: m.unit, status: 'error', valueLabel: 'error', detail: m.detail }
    }
    if (m.status === 'empty' || m.points.length === 0) {
      return { id, label: m.title, unit: m.unit, status: 'empty', valueLabel: '—' }
    }
    const v = m.points[0]!.value
    let valueLabel = String(v)
    if (m.unit === 'ratio') valueLabel = `${(v * 100).toFixed(2)}%`
    else if (m.unit === 'bytes') valueLabel = `${(v / 1_048_576).toFixed(1)} MiB`
    else if (m.unit === 's') valueLabel = `${(v * 1000).toFixed(0)} ms`
    else if (m.unit === 'req/s') valueLabel = v.toFixed(3)
    return { id, label: m.title, unit: m.unit, status: 'ok', valueLabel }
  })
}

function buildSelectedDetail(
  domain: SystemDomainId,
  signals: EvaluatedSignal[],
  alerts: ReturnType<typeof mapAlerts>,
  targets: ScrapeTargetView[],
  input: ObservabilityViewModelInput,
  grafanaBase: string | null,
): SelectedDomainDetail {
  const domainSignals = signals.filter(s => s.def.domain === domain)
  const dependencyPath = domainSignals
    .filter(s => s.def.role === 'required')
    .map(s => ({
      id: s.def.id,
      label: s.def.label,
      scope: s.def.scope,
      state: s.state,
      summary: s.summary,
    }))

  const detailRoutes = [
    ...new Set(
      domainSignals
        .map(s => s.def.detailRoute)
        .filter((r): r is string => r != null && r !== ''),
    ),
  ]

  const dashIds = [
    ...new Set(
      [
        ...domainSignals.map(s => s.def.grafanaDashboardId),
        ...GRAFANA_DASHBOARD_CATALOG.filter(d => d.domain === domain).map(d => d.id),
      ].filter((id): id is string => id != null),
    ),
  ]

  const grafanaLinks = dashIds.map(id => {
    const available = isDashboardCatalogAvailable(id)
    // Namespace: Agent seat override; else catalog defaultNamespace /
    // TRADE_NS[env] inside the builder — never force Trade NS here.
    const url = available
      ? buildGrafanaDashboardUrl({
          grafanaBaseUrl: grafanaBase,
          dashboardId: id,
          env: input.selectedEnv,
          namespace: resolveDashboardNamespaceOverride(id, input.selectedEnv),
          availableUids: input.availableGrafanaUids,
        })
      : null
    const title = GRAFANA_DASHBOARD_CATALOG.find(d => d.id === id)?.title ?? id
    return { label: title, url, available: available && url != null }
  })

  // Primary board for the domain that declares soloPanel (Bifrost boards first).
  const primarySoloDash =
    GRAFANA_DASHBOARD_CATALOG.find(d => d.domain === domain && d.soloPanel != null && d.uid != null) ??
    null
  let soloEmbed: SelectedDomainDetail['soloEmbed'] = null
  if (primarySoloDash != null && primarySoloDash.soloPanel != null) {
    const soloUrl = buildGrafanaSoloPanelUrl({
      grafanaBaseUrl: grafanaBase,
      dashboardId: primarySoloDash.id,
      env: input.selectedEnv,
      namespace: resolveDashboardNamespaceOverride(primarySoloDash.id, input.selectedEnv),
      availableUids: input.availableGrafanaUids,
      theme: 'dark',
    })
    if (soloUrl != null) {
      soloEmbed = {
        url: soloUrl,
        title: primarySoloDash.soloPanel.title,
        height: primarySoloDash.soloPanel.height ?? 180,
      }
    }
  }

  return {
    domain,
    dependencyPath,
    // Golden signals only exist for satellite / ground-systems (matches the UI,
    // which renders the golden section for those two domains only). Rocket
    // CPU/memory evidence lives in the cluster_metrics signals instead.
    goldenSignals:
      domain === 'satellite' || domain === 'ground-systems'
        ? goldenFromMetrics(input.telemetryMetrics)
        : [],
    alerts: alerts.filter(a => a.domain === domain),
    scrapeTargets: sortScrapeTargets(targets.filter(t => t.domain === domain)),
    detailLinks: detailRoutes.map(route => ({
      label: routeLabel(route),
      route,
    })),
    grafanaLinks,
    soloEmbed,
  }
}

function routeLabel(route: string): string {
  switch (route) {
    case 'cluster':
      return 'Rocket → Cluster'
    case 'satellite-telemetry':
      return 'Satellite → Runtime'
    case 'satellite-bus':
      return 'Satellite → Bus Status'
    case 'satellite-api':
      return 'Satellite → API & Auth Probes'
    case 'plugin-gallery':
      return 'Subcontractors → Plugin Gallery'
    case 'agent-desk':
      return 'Engineer → Agent Desk'
    case 'operator-plane':
      return 'Engineer → Operator Plane'
    case 'network':
      return 'Ground → Network'
    case 'control-room':
      return 'Mission Control → Control Room'
    case 'blueprint':
      return 'Governance → Blueprint'
    case 'observability':
      return 'Mission Control → Observability'
    default:
      return route
  }
}

/**
 * Agent Control Plane NS follows seat: prod → bifrost-platform-prod,
 * otherwise catalog default (bifrost-platform-stg). Other boards leave
 * namespace unset so the builder uses catalog defaultNamespace / TRADE_NS.
 */
function resolveDashboardNamespaceOverride(
  dashboardId: string,
  selectedEnv: 'dev' | 'stg' | 'prod',
): string | undefined {
  if (dashboardId !== 'agent-operations') return undefined
  if (selectedEnv === 'prod') return 'bifrost-platform-prod'
  return undefined
}

/**
 * Build the Observability hub view model. Pure / deterministic.
 */
export function buildObservabilityViewModel(
  input: ObservabilityViewModelInput,
): ObservabilityViewModel {
  const nowMs = input.nowMs ?? Date.now()
  const env = input.selectedEnv
  const grafanaBase =
    input.observability?.grafana_url != null && input.observability.grafana_url !== ''
      ? input.observability.grafana_url
      : null

  const standbyNodes = input.standbyNodes ?? []
  const downScrapeTargets = (input.targets ?? [])
    .filter(t => (t.health ?? '').toLowerCase() === 'down')
    .map(t => ({
      job: t.labels?.job,
      instance: t.labels?.instance,
      health: t.health,
    }))
  const mappedAlerts = annotateStandbyAlerts(
    mapAlerts(input.alerts ?? []),
    standbyNodes,
    downScrapeTargets,
  )
  const targets = mapTargets(input.targets)

  const signals: EvaluatedSignal[] = []

  // Rocket
  signals.push(...evaluateLayerB(input.observability, 'shared'))
  signals.push(evaluateScrapeTargets(targets, input.targetsError, standbyNodes))
  signals.push(...evaluateClusterEvidence(input.metrics))

  // Ground
  for (const id of ['ground.redis-ib', 'ground.postgres'] as const) {
    const s = evaluateMetricSignal(id, input.telemetryMetrics, input.telemetryError, 'shared')
    if (s != null) signals.push(s)
  }
  signals.push(evaluateOptionalNone('ground.network'))

  // Satellite
  for (const id of [
    'satellite.api-request-rate',
    'satellite.api-latency-p99',
    'satellite.api-error-rate',
  ] as const) {
    const s = evaluateMetricSignal(id, input.telemetryMetrics, input.telemetryError, env)
    if (s != null) signals.push(s)
  }
  signals.push(evaluateBus(input.bus, env))
  // K8s workloads — evidence only, always unknown unless explicitly supplied later
  {
    const def = getSignalDef('satellite.k8s-workloads')!
    signals.push({
      def,
      state: 'unknown',
      summary: 'Workload readiness is evidence-only (see Satellite Bus Evidence)',
      env,
    })
  }

  // Subcontractors
  signals.push(evaluateIbGateway(input.ibGateway, input.telemetryMetrics))

  // Engineer
  signals.push(evaluateRemediation(input.remediation))
  signals.push(evaluateAgentBridge(input.agentBridge, input.selfHealth))

  // Mission Control / Governance — NOT OBSERVED
  signals.push(evaluateOptionalNone('mission-control.hub'))
  signals.push(evaluateOptionalNone('governance.catalog'))

  // Ensure every registered signal appears (fill gaps as unknown/not_observed)
  for (const def of SIGNAL_REGISTRY) {
    if (signals.some(s => s.def.id === def.id)) continue
    signals.push({
      def,
      state: def.optionalContract ? 'not_observed' : 'unknown',
      summary: 'Not evaluated',
      env: def.scope === 'shared' ? 'shared' : env,
    })
  }

  const domains = OBSERVABILITY_DOMAIN_ORDER.map(domain =>
    buildDomainHealth(
      domain,
      signals.filter(s => s.def.domain === domain),
      mappedAlerts,
    ),
  )

  const generatedAt = new Date(nowMs).toISOString()
  // Freshness only considers observability/metrics generated_at; when all
  // candidates are missing, freshnessMs stays null and stale is intentionally
  // conservative (true) — never claim fresh without a timestamp.
  const freshnessCandidates = [
    input.observability?.generated_at,
    input.metrics?.generated_at,
  ]
    .filter((x): x is string => x != null)
    .map(x => Date.parse(x))
    .filter(n => !Number.isNaN(n))
  const freshest = freshnessCandidates.length > 0 ? Math.max(...freshnessCandidates) : null
  const freshnessMs = freshest != null ? nowMs - freshest : null

  const system = buildSystemVerdict(domains, mappedAlerts, {
    env,
    generatedAt,
    freshnessMs,
  })

  const attention = buildAttentionItems(domains, mappedAlerts, {
    grafanaUrlFor: ({ domain, env: e, activeAt }) => {
      const dash =
        GRAFANA_DASHBOARD_CATALOG.find(d => d.domain === domain)?.id ?? 'platform-overview'
      const alertStart = activeAt != null ? Date.parse(activeAt) : undefined
      const seatEnv = e === 'dev' || e === 'stg' || e === 'prod' ? e : input.selectedEnv
      return buildGrafanaDashboardUrl({
        grafanaBaseUrl: grafanaBase,
        dashboardId: dash,
        env: e,
        namespace: resolveDashboardNamespaceOverride(dash, seatEnv),
        alertStartMs: alertStart != null && !Number.isNaN(alertStart) ? alertStart : undefined,
        availableUids: input.availableGrafanaUids,
      })
    },
  })

  const selected = buildSelectedDetail(
    input.selectedDomain,
    signals,
    mappedAlerts,
    targets,
    input,
    grafanaBase,
  )

  const dashboards = GRAFANA_DASHBOARD_CATALOG.map(d => {
    const catalogOk = d.uid != null
    // Builder resolves var-namespace via catalog defaultNamespace → TRADE_NS;
    // Agent gets an explicit prod seat override.
    const url = catalogOk
      ? buildGrafanaDashboardUrl({
          grafanaBaseUrl: grafanaBase,
          dashboardId: d.id,
          env: input.selectedEnv,
          namespace: resolveDashboardNamespaceOverride(d.id, input.selectedEnv),
          availableUids: input.availableGrafanaUids,
        })
      : null
    return {
      ...d,
      available: catalogOk && url != null,
      url,
    }
  })

  const prometheusConfigured =
    input.telemetryError == null || !/503|not configured/i.test(input.telemetryError)

  return {
    system,
    domains,
    attention,
    selected,
    dashboards,
    layerBStatus: input.observability?.layer_b_status ?? 'unknown',
    prometheusConfigured,
    grafanaBaseUrl: grafanaBase,
  }
}

export { SIGNAL_STALE_MS, TRADE_NS }
