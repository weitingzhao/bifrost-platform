#!/usr/bin/env node
/**
 * Observability hub — deterministic unit tests (no test framework).
 * Usage: npx tsx scripts/observability-viewmodel-test.ts
 */
import assert from 'node:assert/strict'
import { mapAlert, mapAlerts, verdictAffectingAlerts } from '../src/lib/observability/alertMapping'
import {
  buildGrafanaDashboardUrl,
  isDashboardCatalogAvailable,
  normalizeGrafanaBase,
} from '../src/lib/observability/grafanaUrlBuilder'
import { buildObservabilityViewModel } from '../src/lib/observability/observabilityViewModel'
import { SIGNAL_REGISTRY } from '../src/lib/observability/signalRegistry'
import {
  buildDomainHealth,
  domainVerdictFromSignals,
  maxVerdict,
} from '../src/lib/observability/verdictAggregation'
import type { EvaluatedSignal } from '../src/lib/observability/types'
import { getSignalDef } from '../src/lib/observability/signalRegistry'

let passed = 0
function check(name: string, fn: () => void) {
  try {
    fn()
    passed += 1
    console.log(`ok — ${name}`)
  } catch (e) {
    console.error(`FAIL — ${name}`)
    throw e
  }
}

function sig(
  id: string,
  state: EvaluatedSignal['state'],
  summary = state,
): EvaluatedSignal {
  const def = getSignalDef(id)
  assert.ok(def, `missing signal ${id}`)
  return { def, state, summary, env: def.scope === 'shared' ? 'shared' : 'stg' }
}

check('registry covers seven Apollo domains', () => {
  const domains = new Set(SIGNAL_REGISTRY.map(s => s.domain))
  for (const d of [
    'mission-control',
    'rocket',
    'ground-systems',
    'satellite',
    'subcontractors',
    'engineer',
    'governance',
  ]) {
    assert.ok(domains.has(d as never), d)
  }
})

check('required vs evidence — evidence never forces critical alone', () => {
  const signals = [
    sig('rocket.layer-b', 'healthy'),
    sig('rocket.prometheus-reachable', 'healthy'),
    sig('rocket.scrape-targets', 'healthy'),
    sig('rocket.cluster-cpu', 'critical'), // evidence
    sig('rocket.cluster-memory', 'critical'), // evidence
  ]
  const { verdict } = domainVerdictFromSignals(signals, [])
  assert.equal(verdict, 'healthy')
})

check('unknown required → UNKNOWN (not healthy)', () => {
  const signals = [
    sig('satellite.api-request-rate', 'unknown'),
    sig('satellite.api-latency-p99', 'healthy'),
    sig('satellite.api-error-rate', 'healthy'),
    sig('satellite.bus-health', 'healthy'),
  ]
  const { verdict } = domainVerdictFromSignals(signals, [])
  assert.equal(verdict, 'unknown')
})

check('not observed optional contract → NOT OBSERVED', () => {
  const signals = [sig('mission-control.hub', 'not_observed'), sig('governance.catalog', 'not_observed')]
  // build per-domain
  const mc = buildDomainHealth('mission-control', [signals[0]!], [])
  assert.equal(mc.verdict, 'not_observed')
})

check('expected off is neutral', () => {
  const signals = [
    sig('satellite.api-request-rate', 'expected_off'),
    sig('satellite.api-latency-p99', 'healthy'),
    sig('satellite.api-error-rate', 'healthy'),
    sig('satellite.bus-health', 'healthy'),
  ]
  const { verdict, reason } = domainVerdictFromSignals(signals, [])
  assert.equal(verdict, 'healthy')
  assert.match(reason, /expected off/i)
})

check('optional-contract not_observed is neutral — domain can be HEALTHY', () => {
  // Ground Systems: network has no runtime contract yet (optionalContract) —
  // it must not pin the domain to UNKNOWN when everything scored is healthy.
  const signals = [
    sig('ground.redis-ib', 'healthy'),
    sig('ground.postgres', 'healthy'),
    sig('ground.network', 'not_observed'),
  ]
  const { verdict, reason } = domainVerdictFromSignals(signals, [])
  assert.equal(verdict, 'healthy')
  assert.match(reason, /not observed \(optional\)/i)

  // Engineer on prod/stg seat: agent-bridge NOT OBSERVED (optionalContract).
  const engineer = [
    sig('engineer.remediation-runner', 'healthy'),
    sig('engineer.agent-bridge', 'not_observed'),
  ]
  assert.equal(domainVerdictFromSignals(engineer, []).verdict, 'healthy')

  // Non-optional not_observed still blocks HEALTHY (partial observation → UNKNOWN).
  const partial = [
    sig('satellite.api-request-rate', 'not_observed'),
    sig('satellite.api-latency-p99', 'healthy'),
    sig('satellite.api-error-rate', 'healthy'),
    sig('satellite.bus-health', 'healthy'),
  ]
  assert.equal(domainVerdictFromSignals(partial, []).verdict, 'unknown')
})

check('shared dependency counted once + affects domains marked', () => {
  const def = getSignalDef('rocket.layer-b')!
  assert.ok(def.affectsDomains?.includes('satellite'))
  assert.ok(def.affectsDomains?.includes('ground-systems'))
  const signals = [sig('rocket.layer-b', 'critical', 'Layer B down')]
  const attentionDomain = buildDomainHealth('rocket', signals, [])
  assert.equal(attentionDomain.verdict, 'critical')
  assert.deepEqual(attentionDomain.sharedDependencyIds, ['rocket.layer-b'])
})

check('alert mapping — unmapped does not affect verdict', () => {
  const unmapped = mapAlert(
    {
      labels: { alertname: 'TotallyUnknownCustomAlert' },
      annotations: { summary: 'noise' },
      state: 'firing',
    },
    0,
  )
  assert.equal(unmapped.mapped, false)
  assert.equal(verdictAffectingAlerts([unmapped]).length, 0)

  const mapped = mapAlert(
    {
      labels: { alertname: 'TargetDown', severity: 'critical', namespace: 'monitoring' },
      annotations: { summary: 'scrape down' },
      state: 'firing',
    },
    1,
  )
  assert.equal(mapped.mapped, true)
  assert.equal(mapped.domain, 'rocket')
  assert.equal(verdictAffectingAlerts([mapped]).length, 1)
})

check('alert mapping — label severity overrides rule severity', () => {
  // TargetDown rule defaults to critical, but the Prometheus label is ground truth.
  const warn = mapAlert(
    {
      labels: { alertname: 'TargetDown', severity: 'warning', namespace: 'monitoring' },
      state: 'firing',
    },
    0,
  )
  assert.equal(warn.severity, 'warning')
  assert.equal(warn.domain, 'rocket')
  assert.equal(warn.mapped, true)

  // No severity label → rule severity is the fallback.
  const fallback = mapAlert(
    {
      labels: { alertname: 'TargetDown', namespace: 'monitoring' },
      state: 'firing',
    },
    1,
  )
  assert.equal(fallback.severity, 'critical')

  // Label severity info neutralizes a critical rule (info never affects verdict).
  const info = mapAlert(
    {
      labels: { alertname: 'TargetDown', severity: 'info', namespace: 'monitoring' },
      state: 'firing',
    },
    2,
  )
  assert.equal(info.severity, 'info')
  assert.equal(info.mapped, false)
  assert.equal(verdictAffectingAlerts([warn, fallback, info]).length, 2)
})

check('alert mapping — satellite namespace + Http5xx', () => {
  const a = mapAlert(
    {
      labels: { alertname: 'Http5xxHigh', namespace: 'bifrost-stg', severity: 'critical' },
      state: 'firing',
      active_at: '2026-07-21T12:00:00Z',
    },
    0,
  )
  assert.equal(a.domain, 'satellite')
  assert.equal(a.env, 'stg')
  assert.equal(a.severity, 'critical')
})

check('target down degrades rocket scrape signal', () => {
  const vm = buildObservabilityViewModel({
    selectedEnv: 'stg',
    selectedDomain: 'rocket',
    nowMs: Date.parse('2026-07-21T12:00:00Z'),
    observability: {
      cluster_id: 'c',
      namespace: 'monitoring',
      layer_b_status: 'ready',
      layer_b_install_enabled: true,
      reachability: 'ok',
      detail: 'ok',
      components: [],
      grafana_url: 'http://grafana.example:30883',
      prometheus_url: 'http://prom.example',
      generated_at: '2026-07-21T11:59:00Z',
    },
    metrics: {
      cluster_id: 'c',
      reachability: 'ok',
      detail: 'ok',
      metrics_server_available: true,
      cpu_usage_percent: 20,
      memory_usage_percent: 30,
      top_pods: [],
      generated_at: '2026-07-21T11:59:00Z',
    },
    telemetryMetrics: [
      {
        id: 'api_request_rate',
        title: 'API request rate',
        unit: 'req/s',
        status: 'ok',
        points: [{ labels: { service: 'api-monitor' }, value: 1 }],
      },
      {
        id: 'api_latency_p99',
        title: 'API latency P99',
        unit: 's',
        status: 'ok',
        points: [{ labels: { service: 'api-monitor' }, value: 0.05 }],
      },
      {
        id: 'api_error_rate',
        title: 'API 5xx',
        unit: 'ratio',
        status: 'ok',
        points: [{ labels: { service: 'api-monitor' }, value: 0 }],
      },
      {
        id: 'redis_memory_bytes',
        title: 'Redis',
        unit: 'bytes',
        status: 'ok',
        points: [{ labels: { pod: 'redis-ib-0' }, value: 1_000_000 }],
      },
      {
        id: 'pg_connections',
        title: 'PG',
        unit: 'connections',
        status: 'ok',
        points: [{ labels: { pod: 'pg-1' }, value: 10 }],
      },
    ],
    targets: [
      {
        labels: { job: 'trade-api', namespace: 'bifrost-stg', instance: 'a' },
        health: 'down',
        last_error: 'connection refused',
      },
      {
        labels: { job: 'trade-api', namespace: 'bifrost-stg', instance: 'b' },
        health: 'up',
      },
    ],
    bus: { health: 'healthy', topReason: 'All required bus hops healthy' },
    ibGateway: { reachability: 'ok' },
    remediation: { status: 'ok' },
    selfHealth: {
      generated_at: '2026-07-21T11:59:00Z',
      probes: [],
      overall: 'ok',
      viewer_env: 'dev',
    },
    agentBridge: {
      generated_at: '2026-07-21T11:59:00Z',
      remediation_runner: { status: 'ok' },
      git_bridge: { status: 'ok' },
      satellite_probe_bridge: { status: 'ok' },
      hermes_mcp: { status: 'ok' },
      nous_hermes: { status: 'ok' },
    } as never,
  })

  const rocket = vm.domains.find(d => d.domain === 'rocket')!
  assert.ok(rocket.verdict === 'degraded' || rocket.verdict === 'critical')
  const scrape = rocket.signals.find(s => s.def.id === 'rocket.scrape-targets')!
  assert.ok(scrape.state === 'degraded' || scrape.state === 'critical')
})

check('all probes healthy → system overall HEALTHY (P0 regression)', () => {
  const vm = buildObservabilityViewModel({
    selectedEnv: 'stg',
    selectedDomain: 'satellite',
    nowMs: Date.parse('2026-07-21T12:00:00Z'),
    observability: {
      cluster_id: 'c',
      namespace: 'monitoring',
      layer_b_status: 'ready',
      layer_b_install_enabled: true,
      reachability: 'ok',
      detail: 'ok',
      components: [],
      grafana_url: 'http://grafana.example:30883',
      prometheus_url: 'http://prom.example',
      generated_at: '2026-07-21T11:59:30Z',
    },
    metrics: {
      cluster_id: 'c',
      reachability: 'ok',
      detail: 'ok',
      metrics_server_available: true,
      cpu_usage_percent: 20,
      memory_usage_percent: 30,
      top_pods: [],
      generated_at: '2026-07-21T11:59:30Z',
    },
    telemetryMetrics: [
      {
        id: 'api_request_rate',
        title: 'API request rate',
        unit: 'req/s',
        status: 'ok',
        points: [{ labels: { service: 'api-monitor' }, value: 1 }],
      },
      {
        id: 'api_latency_p99',
        title: 'API latency P99',
        unit: 's',
        status: 'ok',
        points: [{ labels: { service: 'api-monitor' }, value: 0.05 }],
      },
      {
        id: 'api_error_rate',
        title: 'API 5xx',
        unit: 'ratio',
        status: 'ok',
        points: [{ labels: { service: 'api-monitor' }, value: 0 }],
      },
      {
        id: 'redis_memory_bytes',
        title: 'Redis',
        unit: 'bytes',
        status: 'ok',
        points: [{ labels: { pod: 'redis-ib-0' }, value: 1_000_000 }],
      },
      {
        id: 'pg_connections',
        title: 'PG',
        unit: 'connections',
        status: 'ok',
        points: [{ labels: { pod: 'pg-1' }, value: 10 }],
      },
    ],
    targets: [
      { labels: { job: 'trade-api', namespace: 'bifrost-stg', instance: 'a' }, health: 'up' },
      { labels: { job: 'redis', namespace: 'data', instance: 'r' }, health: 'up' },
    ],
    bus: { health: 'healthy', topReason: 'All required bus hops healthy' },
    ibGateway: { reachability: 'ok' },
    remediation: { status: 'ok' },
    selfHealth: {
      generated_at: '2026-07-21T11:59:30Z',
      probes: [],
      overall: 'ok',
      viewer_env: 'dev',
    },
    agentBridge: {
      generated_at: '2026-07-21T11:59:30Z',
      remediation_runner: { status: 'ok' },
      git_bridge: { status: 'ok' },
      satellite_probe_bridge: { status: 'ok' },
      hermes_mcp: { status: 'ok' },
      nous_hermes: { status: 'ok' },
    } as never,
  })

  // Optional-contract signals (ground.network etc.) must not pin domains to UNKNOWN.
  const ground = vm.domains.find(d => d.domain === 'ground-systems')!
  assert.equal(ground.verdict, 'healthy')
  assert.equal(vm.system.overall, 'healthy')
  assert.equal(vm.system.stale, false)
  // Pure NOT OBSERVED domains (Mission Control / Governance) stay excluded, not faked.
  const mc = vm.domains.find(d => d.domain === 'mission-control')!
  assert.equal(mc.verdict, 'not_observed')
})

check('stale freshness flagged', () => {
  const vm = buildObservabilityViewModel({
    selectedEnv: 'stg',
    selectedDomain: 'satellite',
    nowMs: Date.parse('2026-07-21T13:00:00Z'),
    observability: {
      cluster_id: 'c',
      namespace: 'monitoring',
      layer_b_status: 'ready',
      layer_b_install_enabled: false,
      reachability: 'ok',
      detail: 'ok',
      components: [],
      generated_at: '2026-07-21T12:00:00Z', // 60m old
    },
  })
  assert.equal(vm.system.stale, true)
  assert.ok((vm.system.freshnessMs ?? 0) >= 60 * 60_000)
})

check('cross-env isolation — selected env on bus / metrics', () => {
  const vm = buildObservabilityViewModel({
    selectedEnv: 'prod',
    selectedDomain: 'satellite',
    nowMs: Date.parse('2026-07-21T12:00:00Z'),
    bus: { health: 'unavailable', topReason: 'Required consumer down in PROD' },
    telemetryMetrics: [],
    telemetryError: null,
  })
  const sat = vm.domains.find(d => d.domain === 'satellite')!
  const bus = sat.signals.find(s => s.def.id === 'satellite.bus-health')!
  assert.equal(bus.env, 'prod')
  assert.equal(bus.state, 'critical')
})

check('prometheus 503 → NOT OBSERVED / unknown, never healthy fake', () => {
  const vm = buildObservabilityViewModel({
    selectedEnv: 'stg',
    selectedDomain: 'satellite',
    telemetryError: 'telemetry overview: HTTP 503 — not configured',
    targetsError: 'telemetry targets: HTTP 503 — not configured',
  })
  assert.equal(vm.prometheusConfigured, false)
  const sat = vm.domains.find(d => d.domain === 'satellite')!
  // Without bus, API metrics not_observed → not healthy
  assert.notEqual(sat.verdict, 'healthy')
  assert.notEqual(vm.system.overall, 'healthy')
})

check('metric query error → UNKNOWN (observability fault, not business degraded)', () => {
  const vm = buildObservabilityViewModel({
    selectedEnv: 'stg',
    selectedDomain: 'satellite',
    nowMs: Date.parse('2026-07-21T12:00:00Z'),
    telemetryMetrics: [
      {
        id: 'api_request_rate',
        title: 'API request rate',
        unit: 'req/s',
        status: 'error',
        detail: 'prometheus query failed: 502',
        points: [],
      },
    ],
  })
  const sat = vm.domains.find(d => d.domain === 'satellite')!
  const reqRate = sat.signals.find(s => s.def.id === 'satellite.api-request-rate')!
  assert.equal(reqRate.state, 'unknown')
  assert.notEqual(reqRate.state, 'degraded')
  assert.equal(sat.verdict, 'unknown')
})

check('ib_gateway_up is evidence-only — verdict stays with ib_gateway API', () => {
  const vm = buildObservabilityViewModel({
    selectedEnv: 'stg',
    selectedDomain: 'subcontractors',
    nowMs: Date.parse('2026-07-21T12:00:00Z'),
    ibGateway: { reachability: 'ok' },
    telemetryMetrics: [
      {
        id: 'ib_gateway_up',
        title: 'IB Gateway scrape up',
        unit: 'up',
        status: 'ok',
        points: [{ labels: { job: 'ib-gateway' }, value: 1 }],
      },
    ],
  })
  const sub = vm.domains.find(d => d.domain === 'subcontractors')!
  const ib = sub.signals.find(s => s.def.id === 'subcontractors.ib-gateway')!
  assert.equal(ib.state, 'healthy')
  assert.match(ib.evidence ?? '', /prometheus ib_gateway_up=1/)

  // API says down → verdict critical even if Prometheus up=1 (single verdict source).
  const vmDown = buildObservabilityViewModel({
    selectedEnv: 'stg',
    selectedDomain: 'subcontractors',
    nowMs: Date.parse('2026-07-21T12:00:00Z'),
    ibGateway: { reachability: 'down' },
    telemetryMetrics: [
      {
        id: 'ib_gateway_up',
        title: 'IB Gateway scrape up',
        unit: 'up',
        status: 'ok',
        points: [{ labels: { job: 'ib-gateway' }, value: 1 }],
      },
    ],
  })
  const ibDown = vmDown.domains
    .find(d => d.domain === 'subcontractors')!
    .signals.find(s => s.def.id === 'subcontractors.ib-gateway')!
  assert.equal(ibDown.state, 'critical')
  assert.match(ibDown.evidence ?? '', /prometheus ib_gateway_up=1/)
})

check('golden signals only for satellite / ground-systems — rocket empty', () => {
  const vm = buildObservabilityViewModel({
    selectedEnv: 'stg',
    selectedDomain: 'rocket',
    nowMs: Date.parse('2026-07-21T12:00:00Z'),
    telemetryMetrics: [
      {
        id: 'api_request_rate',
        title: 'API request rate',
        unit: 'req/s',
        status: 'ok',
        points: [{ labels: { service: 'api-monitor' }, value: 1 }],
      },
    ],
  })
  assert.equal(vm.selected.goldenSignals.length, 0)
})

check('maxVerdict ordering', () => {
  assert.equal(maxVerdict('healthy', 'degraded'), 'degraded')
  assert.equal(maxVerdict('unknown', 'critical'), 'critical')
  // NOT OBSERVED ranks above HEALTHY — never mask missing observation as healthy.
  assert.equal(maxVerdict('not_observed', 'healthy'), 'not_observed')
})

check('grafana URL builder — safe + contextual', () => {
  assert.equal(normalizeGrafanaBase('javascript:alert(1)'), null)
  assert.equal(normalizeGrafanaBase('http://g.example/'), 'http://g.example')
  assert.equal(isDashboardCatalogAvailable('agent-operations'), false)
  assert.equal(isDashboardCatalogAvailable('satellite-trade-overview'), true)

  const url = buildGrafanaDashboardUrl({
    grafanaBaseUrl: 'http://grafana.example:30883/',
    dashboardId: 'satellite-trade-overview',
    env: 'stg',
    service: 'api-monitor',
    alertStartMs: 1_700_000_000_000,
  })
  assert.ok(url)
  assert.match(url!, /^http:\/\/grafana\.example:30883\/d\/bifrost-trade-overview\//)
  assert.match(url!, /var-namespace=bifrost-stg/)
  assert.match(url!, /var-env=stg/)
  assert.match(url!, /var-service=api-monitor/)
  assert.match(url!, /from=1700000000000/)

  const unavailable = buildGrafanaDashboardUrl({
    grafanaBaseUrl: 'http://grafana.example',
    dashboardId: 'agent-operations',
  })
  assert.equal(unavailable, null)
})

check('mapped alerts in attention; defects never in input', () => {
  const alerts = mapAlerts([
    {
      labels: { alertname: 'RedisDown', severity: 'critical', namespace: 'data' },
      annotations: { summary: 'redis-ib down' },
      state: 'firing',
      active_at: '2026-07-21T11:00:00Z',
    },
  ])
  const vm = buildObservabilityViewModel({
    selectedEnv: 'stg',
    selectedDomain: 'ground-systems',
    nowMs: Date.parse('2026-07-21T12:00:00Z'),
    alerts,
    observability: {
      cluster_id: 'c',
      namespace: 'monitoring',
      layer_b_status: 'ready',
      layer_b_install_enabled: true,
      reachability: 'ok',
      detail: 'ok',
      components: [],
      grafana_url: 'http://grafana.example',
      generated_at: '2026-07-21T11:59:30Z',
    },
    telemetryMetrics: [
      {
        id: 'redis_memory_bytes',
        title: 'Redis',
        status: 'ok',
        points: [{ labels: {}, value: 1 }],
      },
      {
        id: 'pg_connections',
        title: 'PG',
        status: 'ok',
        points: [{ labels: {}, value: 1 }],
      },
    ],
  })
  assert.ok(vm.attention.some(a => a.id.startsWith('alert:')))
  const ground = vm.domains.find(d => d.domain === 'ground-systems')!
  assert.equal(ground.verdict, 'critical')
  // No defects field exists on view model by construction.
  assert.equal('defects' in vm, false)
})

check('dashboard catalog availability flags', () => {
  const vm = buildObservabilityViewModel({
    selectedEnv: 'stg',
    selectedDomain: 'rocket',
    observability: {
      cluster_id: 'c',
      namespace: 'monitoring',
      layer_b_status: 'ready',
      layer_b_install_enabled: true,
      reachability: 'ok',
      detail: 'ok',
      components: [],
      grafana_url: 'http://grafana.example',
      generated_at: '2026-07-21T12:00:00Z',
    },
    nowMs: Date.parse('2026-07-21T12:00:30Z'),
  })
  const trade = vm.dashboards.find(d => d.id === 'satellite-trade-overview')!
  assert.equal(trade.available, true)
  assert.ok(trade.url != null)
  const agent = vm.dashboards.find(d => d.id === 'agent-operations')!
  assert.equal(agent.available, false)
  assert.equal(agent.url, null)
})

console.log(`\n${passed} checks passed`)
