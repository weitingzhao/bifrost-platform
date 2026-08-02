/**
 * Observability hub — deterministic unit tests.
 * Ported from scripts/observability-viewmodel-test.ts.
 */
import { describe, expect, it } from 'vitest'
import type { AgentBridgeResponse } from '@/api/agentTypes'
import {
  annotateStandbyAlerts,
  isElasticStandbyAlert,
  mapAlert,
  mapAlerts,
  verdictAffectingAlerts,
} from '@/lib/observability/alertMapping'
import {
  buildGrafanaDashboardUrl,
  isDashboardCatalogAvailable,
  normalizeGrafanaBase,
} from '@/lib/observability/grafanaUrlBuilder'
import { buildObservabilityViewModel } from '@/lib/observability/observabilityViewModel'
import { getSignalDef, SIGNAL_REGISTRY } from '@/lib/observability/signalRegistry'
import type { EvaluatedSignal } from '@/lib/observability/types'
import {
  buildAttentionItems,
  buildDomainHealth,
  buildSystemVerdict,
  domainProbeability,
  domainVerdictFromSignals,
  gapSummaryFromSignals,
  maxVerdict,
  signalToGap,
  sumGapSummaries,
} from '@/lib/observability/verdictAggregation'

function sig(id: string, state: EvaluatedSignal['state'], summary = state): EvaluatedSignal {
  const def = getSignalDef(id)
  expect(def, `missing signal ${id}`).toBeTruthy()
  return { def: def!, state, summary, env: def!.scope === 'shared' ? 'shared' : 'stg' }
}

function agentBridgeOk(): AgentBridgeResponse {
  return {
    generated_at: '2026-07-21T11:59:00Z',
    remediation_runner: { url: 'http://runner', status: 'ok' },
    git_bridge: { status: 'ok' },
    satellite_probe_bridge: { status: 'ok' },
    hermes_mcp: { status: 'ok' },
    nous_hermes: {
      status: 'ok',
      gateway_running: true,
      active_agents: 0,
      active_sessions: 0,
      mcp_tool_count: 0,
    },
    platform_mcp: {
      server_name: 'bifrost-platform',
      server_version: '0.0.0',
      tool_count: 0,
      implemented_count: 0,
      agent_tool_count: 0,
      transport: 'stdio',
      script_path: '',
    },
    nightly_report: { available: false },
  }
}

describe('observability signal registry + verdict aggregation', () => {
  it('registry covers seven Apollo domains', () => {
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
      expect(domains.has(d as never), d).toBe(true)
    }
  })

  it('required vs evidence — evidence never forces critical alone', () => {
    const signals = [
      sig('rocket.layer-b', 'healthy'),
      sig('rocket.prometheus-reachable', 'healthy'),
      sig('rocket.scrape-targets', 'healthy'),
      sig('rocket.cluster-cpu', 'critical'), // evidence
      sig('rocket.cluster-memory', 'critical'), // evidence
    ]
    const { verdict } = domainVerdictFromSignals(signals, [])
    expect(verdict).toBe('healthy')
  })

  it('unknown required → UNKNOWN (not healthy)', () => {
    const signals = [
      sig('satellite.api-request-rate', 'unknown'),
      sig('satellite.api-latency-p99', 'healthy'),
      sig('satellite.api-error-rate', 'healthy'),
      sig('satellite.bus-health', 'healthy'),
    ]
    const { verdict } = domainVerdictFromSignals(signals, [])
    expect(verdict).toBe('unknown')
  })

  it('not observed optional contract → NOT OBSERVED', () => {
    const signals = [sig('mission-control.hub', 'not_observed'), sig('governance.catalog', 'not_observed')]
    const mc = buildDomainHealth('mission-control', [signals[0]!], [])
    expect(mc.verdict).toBe('not_observed')
    expect(mc.probeability).toBe('reference')
  })

  it('marks Mission Control / Governance as reference; runtime domains stay runtime', () => {
    expect(domainProbeability('mission-control', [sig('mission-control.hub', 'not_observed')])).toBe(
      'reference',
    )
    expect(domainProbeability('governance', [sig('governance.catalog', 'not_observed')])).toBe(
      'reference',
    )
    expect(buildDomainHealth('mission-control', [sig('mission-control.hub', 'not_observed')], []).probeability).toBe(
      'reference',
    )
    expect(buildDomainHealth('governance', [sig('governance.catalog', 'not_observed')], []).probeability).toBe(
      'reference',
    )
    // Ground Systems has optional network but also runtime redis/postgres → runtime
    const ground = buildDomainHealth(
      'ground-systems',
      [
        sig('ground.redis-ib', 'healthy'),
        sig('ground.postgres', 'healthy'),
        sig('ground.network', 'not_observed'),
      ],
      [],
    )
    expect(ground.probeability).toBe('runtime')
    expect(
      buildDomainHealth(
        'satellite',
        [
          sig('satellite.api-request-rate', 'unknown'),
          sig('satellite.api-latency-p99', 'healthy'),
          sig('satellite.api-error-rate', 'healthy'),
          sig('satellite.bus-health', 'healthy'),
        ],
        [],
      ).probeability,
    ).toBe('runtime')
  })

  it('reference domains do not inflate runtime gap rollup or domainCounts', () => {
    const satelliteBlind = buildDomainHealth(
      'satellite',
      [
        sig('satellite.api-request-rate', 'not_observed'), // non-optional → blind
        sig('satellite.api-latency-p99', 'healthy'),
        sig('satellite.api-error-rate', 'healthy'),
        sig('satellite.bus-health', 'healthy'),
      ],
      [],
    )
    const mc = buildDomainHealth('mission-control', [sig('mission-control.hub', 'not_observed')], [])
    const gov = buildDomainHealth('governance', [sig('governance.catalog', 'not_observed')], [])
    const rocketOk = buildDomainHealth(
      'rocket',
      [
        sig('rocket.layer-b', 'healthy'),
        sig('rocket.prometheus-reachable', 'healthy'),
        sig('rocket.scrape-targets', 'healthy'),
      ],
      [],
    )

    const rolled = sumGapSummaries([satelliteBlind, mc, gov, rocketOk])
    // MC/Gov by-design excluded from default rollup; satellite blind still counts
    expect(rolled.blind).toBe(1)
    expect(rolled.fail).toBe(0)
    expect(rolled.byDesign).toBe(0)
    expect(rolled.ok).toBe(6) // 3 satellite healthy scored + 3 rocket
    expect(rolled.total).toBe(7)

    const withRef = sumGapSummaries([satelliteBlind, mc, gov, rocketOk], { includeReference: true })
    expect(withRef.byDesign).toBe(2)
    expect(withRef.total).toBe(9)

    const system = buildSystemVerdict([satelliteBlind, mc, gov, rocketOk], [], {
      env: 'stg',
      generatedAt: '2026-08-02T00:00:00Z',
      freshnessMs: 1_000,
    })
    expect(system.referenceDomainCount).toBe(2)
    expect(system.domainCounts.not_observed).toBe(0) // reference excluded
    expect(system.domainCounts.unknown).toBe(1) // satellite partial observation
    expect(system.domainCounts.healthy).toBe(1) // rocket
  })

  it('expected off is neutral', () => {
    const signals = [
      sig('satellite.api-request-rate', 'expected_off'),
      sig('satellite.api-latency-p99', 'healthy'),
      sig('satellite.api-error-rate', 'healthy'),
      sig('satellite.bus-health', 'healthy'),
    ]
    const { verdict, reason } = domainVerdictFromSignals(signals, [])
    expect(verdict).toBe('healthy')
    expect(reason).toMatch(/expected off/i)
  })

  it('optional-contract not_observed is neutral — domain can be HEALTHY', () => {
    // Ground Systems: network has no runtime contract yet (optionalContract) —
    // it must not pin the domain to UNKNOWN when everything scored is healthy.
    const signals = [
      sig('ground.redis-ib', 'healthy'),
      sig('ground.postgres', 'healthy'),
      sig('ground.network', 'not_observed'),
    ]
    const { verdict, reason } = domainVerdictFromSignals(signals, [])
    expect(verdict).toBe('healthy')
    expect(reason).toMatch(/not observed \(optional\)/i)

    // Engineer on prod/stg seat: agent-bridge NOT OBSERVED (optionalContract).
    const engineer = [
      sig('engineer.remediation-runner', 'healthy'),
      sig('engineer.agent-bridge', 'not_observed'),
    ]
    expect(domainVerdictFromSignals(engineer, []).verdict).toBe('healthy')

    // Non-optional not_observed still blocks HEALTHY (partial observation → UNKNOWN).
    const partial = [
      sig('satellite.api-request-rate', 'not_observed'),
      sig('satellite.api-latency-p99', 'healthy'),
      sig('satellite.api-error-rate', 'healthy'),
      sig('satellite.bus-health', 'healthy'),
    ]
    expect(domainVerdictFromSignals(partial, []).verdict).toBe('unknown')
  })

  it('shared dependency counted once + affects domains marked', () => {
    const def = getSignalDef('rocket.layer-b')!
    expect(def.affectsDomains?.includes('satellite')).toBe(true)
    expect(def.affectsDomains?.includes('ground-systems')).toBe(true)
    const signals = [sig('rocket.layer-b', 'critical', 'Layer B down')]
    const attentionDomain = buildDomainHealth('rocket', signals, [])
    expect(attentionDomain.verdict).toBe('critical')
    expect(attentionDomain.sharedDependencyIds).toEqual(['rocket.layer-b'])
  })

  it('maxVerdict ordering', () => {
    expect(maxVerdict('healthy', 'degraded')).toBe('degraded')
    expect(maxVerdict('unknown', 'critical')).toBe('critical')
    // NOT OBSERVED ranks above HEALTHY — never mask missing observation as healthy.
    expect(maxVerdict('not_observed', 'healthy')).toBe('not_observed')
  })

  it('signalToGap — Expected vs Actual mapping', () => {
    expect(signalToGap(sig('satellite.bus-health', 'healthy'))).toBe('ok')
    expect(signalToGap(sig('satellite.api-request-rate', 'expected_off'))).toBe('ok')
    expect(signalToGap(sig('satellite.bus-health', 'critical'))).toBe('fail')
    expect(signalToGap(sig('satellite.bus-health', 'degraded'))).toBe('fail')
    expect(signalToGap(sig('satellite.api-request-rate', 'unknown'))).toBe('blind')
    // Non-optional not_observed → blind (probe missing)
    expect(signalToGap(sig('satellite.api-request-rate', 'not_observed'))).toBe('blind')
    // Optional contract not_observed → by_design
    expect(signalToGap(sig('ground.network', 'not_observed'))).toBe('by_design')
    expect(signalToGap(sig('mission-control.hub', 'not_observed'))).toBe('by_design')
  })

  it('gapSummary counts required signals only', () => {
    const signals = [
      sig('rocket.layer-b', 'healthy'),
      sig('rocket.prometheus-reachable', 'healthy'),
      sig('rocket.scrape-targets', 'degraded'),
      sig('rocket.cluster-cpu', 'critical'), // evidence — ignored
      sig('rocket.cluster-memory', 'unknown'), // evidence — ignored
    ]
    const summary = gapSummaryFromSignals(signals)
    expect(summary).toEqual({ ok: 2, fail: 1, blind: 0, byDesign: 0, total: 3 })

    const mixed = [
      sig('ground.redis-ib', 'healthy'),
      sig('ground.postgres', 'unknown'),
      sig('ground.network', 'not_observed'), // optionalContract
    ]
    const ground = buildDomainHealth('ground-systems', mixed, [])
    expect(ground.gapSummary).toEqual({ ok: 1, fail: 0, blind: 1, byDesign: 1, total: 3 })

    const mc = buildDomainHealth('mission-control', [sig('mission-control.hub', 'not_observed')], [])
    // Default rollup excludes reference domains (MC by-design does not inflate system meta)
    const rolled = sumGapSummaries([ground, mc])
    expect(rolled).toEqual({ ok: 1, fail: 0, blind: 1, byDesign: 1, total: 3 })
    expect(sumGapSummaries([ground, mc], { includeReference: true })).toEqual({
      ok: 1,
      fail: 0,
      blind: 1,
      byDesign: 2,
      total: 4,
    })
  })
})

describe('alert mapping', () => {
  it('unmapped does not affect verdict', () => {
    const unmapped = mapAlert(
      {
        labels: { alertname: 'TotallyUnknownCustomAlert' },
        annotations: { summary: 'noise' },
        state: 'firing',
      },
      0,
    )
    expect(unmapped.mapped).toBe(false)
    expect(verdictAffectingAlerts([unmapped])).toHaveLength(0)

    const mapped = mapAlert(
      {
        labels: { alertname: 'TargetDown', severity: 'critical', namespace: 'monitoring' },
        annotations: { summary: 'scrape down' },
        state: 'firing',
      },
      1,
    )
    expect(mapped.mapped).toBe(true)
    expect(mapped.domain).toBe('rocket')
    expect(verdictAffectingAlerts([mapped])).toHaveLength(1)
  })

  it('label severity overrides rule severity', () => {
    // TargetDown rule defaults to critical, but the Prometheus label is ground truth.
    const warn = mapAlert(
      {
        labels: { alertname: 'TargetDown', severity: 'warning', namespace: 'monitoring' },
        state: 'firing',
      },
      0,
    )
    expect(warn.severity).toBe('warning')
    expect(warn.domain).toBe('rocket')
    expect(warn.mapped).toBe(true)

    // No severity label → rule severity is the fallback.
    const fallback = mapAlert(
      {
        labels: { alertname: 'TargetDown', namespace: 'monitoring' },
        state: 'firing',
      },
      1,
    )
    expect(fallback.severity).toBe('critical')

    // Label severity info neutralizes a critical rule (info never affects verdict).
    const info = mapAlert(
      {
        labels: { alertname: 'TargetDown', severity: 'info', namespace: 'monitoring' },
        state: 'firing',
      },
      2,
    )
    expect(info.severity).toBe('info')
    expect(info.mapped).toBe(false)
    expect(verdictAffectingAlerts([warn, fallback, info])).toHaveLength(2)
  })

  it('satellite namespace + Http5xx', () => {
    const a = mapAlert(
      {
        labels: { alertname: 'Http5xxHigh', namespace: 'bifrost-stg', severity: 'critical' },
        state: 'firing',
        active_at: '2026-07-21T12:00:00Z',
      },
      0,
    )
    expect(a.domain).toBe('satellite')
    expect(a.env).toBe('stg')
    expect(a.severity).toBe('critical')
  })

  it('elastic standby NotReady is standbyNeutral and excluded from verdict/attention', () => {
    const standby = [{ name: 'gpu-server', internalIp: '192.168.10.74' }]
    const standbyAlert = mapAlert(
      {
        labels: {
          alertname: 'KubeNodeNotReady',
          severity: 'warning',
          node: 'gpu-server',
        },
        annotations: { summary: 'gpu-server NotReady' },
        state: 'firing',
      },
      0,
    )
    expect(isElasticStandbyAlert(standbyAlert, standby)).toBe(true)
    const annotated = annotateStandbyAlerts([standbyAlert], standby)
    expect(annotated[0].standbyNeutral).toBe(true)
    expect(verdictAffectingAlerts(annotated)).toHaveLength(0)

    const domains = [
      buildDomainHealth('rocket', [sig('rocket.scrape-targets', 'healthy')], annotated),
    ]
    const attention = buildAttentionItems(domains, annotated)
    expect(attention.some(a => a.signalLabel === 'KubeNodeNotReady')).toBe(false)
  })

  it('non-standby NotReady still affects verdict', () => {
    const standby = [{ name: 'gpu-server', internalIp: '192.168.10.74' }]
    const coreAlert = mapAlert(
      {
        labels: {
          alertname: 'KubeNodeNotReady',
          severity: 'warning',
          node: 'ubt-k3s-01',
        },
        annotations: { summary: 'core NotReady' },
        state: 'firing',
      },
      0,
    )
    expect(isElasticStandbyAlert(coreAlert, standby)).toBe(false)
    const annotated = annotateStandbyAlerts([coreAlert], standby)
    expect(annotated[0].standbyNeutral).toBeFalsy()
    expect(verdictAffectingAlerts(annotated)).toHaveLength(1)
  })

  it('standby node-exporter TargetDown is neutralized', () => {
    const standby = [{ name: 'gpu-server', internalIp: '192.168.10.74' }]
    const td = mapAlert(
      {
        labels: {
          alertname: 'TargetDown',
          severity: 'critical',
          job: 'node-exporter',
          instance: '192.168.10.74:9100',
        },
        state: 'firing',
      },
      0,
    )
    expect(isElasticStandbyAlert(td, standby)).toBe(true)
  })

  /**
   * Demand path: when ollama/minio want replicas, platform-api classifies the
   * node as elastic_mode=degraded (not standby). Observability only passes
   * elastic_mode===standby into standbyNodes — so the list is empty and
   * KubeNodeNotReady must remain Attention WARNING.
   */
  it('demand (empty standbyNodes / degraded node) does not neutralize gpu-server alerts', () => {
    const demandAlert = mapAlert(
      {
        labels: {
          alertname: 'KubeNodeNotReady',
          severity: 'warning',
          node: 'gpu-server',
        },
        annotations: { summary: 'gpu-server NotReady — compute needed' },
        state: 'firing',
      },
      0,
    )
    // No standby refs → FE must not suppress (mirrors elastic_mode=degraded).
    expect(isElasticStandbyAlert(demandAlert, [])).toBe(false)
    const annotated = annotateStandbyAlerts([demandAlert], [])
    expect(annotated[0].standbyNeutral).toBeFalsy()
    expect(verdictAffectingAlerts(annotated)).toHaveLength(1)

    const domains = [
      buildDomainHealth('rocket', [sig('rocket.scrape-targets', 'healthy')], annotated),
    ]
    const attention = buildAttentionItems(domains, annotated)
    expect(attention.some(a => a.signalLabel === 'KubeNodeNotReady')).toBe(true)
  })

  it('demand TargetDown on gpu-server still affects rocket when not in standby list', () => {
    const vm = buildObservabilityViewModel({
      selectedEnv: 'stg',
      selectedDomain: 'rocket',
      nowMs: Date.parse('2026-07-21T12:00:00Z'),
      // Demand → API omits node from standbyNodes
      standbyNodes: [],
      targets: [
        {
          labels: { job: 'node-exporter', instance: '192.168.10.74:9100' },
          health: 'down',
          last_error: 'connection refused',
        },
      ],
      alerts: [
        {
          labels: {
            alertname: 'KubeNodeNotReady',
            severity: 'warning',
            node: 'gpu-server',
          },
          annotations: { summary: 'Compute needed but node offline' },
          state: 'firing',
        },
      ],
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
    })
    const scrape = vm.domains
      .find(d => d.id === 'rocket')
      ?.signals.find(s => s.def.id === 'rocket.scrape-targets')
    // Sole target down + not standby → scrape must not stay healthy-by-standby-filter
    expect(scrape?.state === 'healthy').toBe(false)
    expect(vm.attention.some(a => a.signalLabel === 'KubeNodeNotReady')).toBe(true)
  })
})

describe('buildObservabilityViewModel', () => {
  it('standby node-exporter TargetDown does not degrade rocket scrape signal', () => {
    const vm = buildObservabilityViewModel({
      selectedEnv: 'stg',
      selectedDomain: 'rocket',
      nowMs: Date.parse('2026-07-21T12:00:00Z'),
      standbyNodes: [{ name: 'gpu-server', internalIp: '192.168.10.74' }],
      targets: [
        {
          labels: { job: 'node-exporter', instance: '192.168.10.73:9100' },
          health: 'up',
        },
        {
          labels: { job: 'node-exporter', instance: '192.168.10.74:9100' },
          health: 'down',
          last_error: 'connection refused',
        },
      ],
      alerts: [
        {
          labels: {
            alertname: 'KubeNodeNotReady',
            severity: 'warning',
            node: 'gpu-server',
          },
          annotations: { summary: 'standby NotReady' },
          state: 'firing',
        },
      ],
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
    })
    const scrape = vm.domains
      .find(d => d.domain === 'rocket')!
      .signals.find(s => s.def.id === 'rocket.scrape-targets')!
    expect(scrape.state).toBe('healthy')
    expect(vm.attention.some(a => a.signalLabel === 'KubeNodeNotReady')).toBe(false)
  })

  it('target down degrades rocket scrape signal', () => {
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
      agentBridge: agentBridgeOk(),
    })

    const rocket = vm.domains.find(d => d.domain === 'rocket')!
    expect(rocket.verdict === 'degraded' || rocket.verdict === 'critical').toBe(true)
    const scrape = rocket.signals.find(s => s.def.id === 'rocket.scrape-targets')!
    expect(scrape.state === 'degraded' || scrape.state === 'critical').toBe(true)
  })

  it('all probes healthy → system overall HEALTHY (P0 regression)', () => {
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
      agentBridge: { ...agentBridgeOk(), generated_at: '2026-07-21T11:59:30Z' },
    })

    // Optional-contract signals (ground.network etc.) must not pin domains to UNKNOWN.
    const ground = vm.domains.find(d => d.domain === 'ground-systems')!
    expect(ground.verdict).toBe('healthy')
    expect(vm.system.overall).toBe('healthy')
    expect(vm.system.stale).toBe(false)
    // Pure NOT OBSERVED domains (Mission Control / Governance) stay excluded, not faked.
    const mc = vm.domains.find(d => d.domain === 'mission-control')!
    expect(mc.verdict).toBe('not_observed')
  })

  it('stale freshness flagged', () => {
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
    expect(vm.system.stale).toBe(true)
    expect(vm.system.freshnessMs ?? 0).toBeGreaterThanOrEqual(60 * 60_000)
  })

  it('cross-env isolation — selected env on bus / metrics', () => {
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
    expect(bus.env).toBe('prod')
    expect(bus.state).toBe('critical')
  })

  it('prometheus 503 → NOT OBSERVED / unknown, never healthy fake', () => {
    const vm = buildObservabilityViewModel({
      selectedEnv: 'stg',
      selectedDomain: 'satellite',
      telemetryError: 'telemetry overview: HTTP 503 — not configured',
      targetsError: 'telemetry targets: HTTP 503 — not configured',
    })
    expect(vm.prometheusConfigured).toBe(false)
    const sat = vm.domains.find(d => d.domain === 'satellite')!
    // Without bus, API metrics not_observed → not healthy
    expect(sat.verdict).not.toBe('healthy')
    expect(vm.system.overall).not.toBe('healthy')
  })

  it('metric query error → UNKNOWN (observability fault, not business degraded)', () => {
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
    expect(reqRate.state).toBe('unknown')
    expect(reqRate.state).not.toBe('degraded')
    expect(sat.verdict).toBe('unknown')
  })

  it('ib_gateway_up is evidence-only — verdict stays with ib_gateway API', () => {
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
    expect(ib.state).toBe('healthy')
    expect(ib.evidence ?? '').toMatch(/prometheus ib_gateway_up=1/)

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
    expect(ibDown.state).toBe('critical')
    expect(ibDown.evidence ?? '').toMatch(/prometheus ib_gateway_up=1/)
  })

  it('golden signals only for satellite / ground-systems — rocket empty', () => {
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
    expect(vm.selected.goldenSignals).toHaveLength(0)
  })

  it('mapped alerts in attention; defects never in input', () => {
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
    expect(vm.attention.some(a => a.id.startsWith('alert:'))).toBe(true)
    const ground = vm.domains.find(d => d.domain === 'ground-systems')!
    expect(ground.verdict).toBe('critical')
    // No defects field exists on view model by construction.
    expect('defects' in vm).toBe(false)
  })

  it('dashboard catalog availability flags', () => {
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
    expect(trade.available).toBe(true)
    expect(trade.url).not.toBeNull()
    expect(trade.url).toMatch(/var-namespace=bifrost-stg/)
    const agent = vm.dashboards.find(d => d.id === 'agent-operations')!
    expect(agent.available).toBe(true)
    expect(agent.url).not.toBeNull()
    expect(agent.url).toMatch(/var-namespace=bifrost-platform-stg/)
    expect(agent.title).toBe('Platform Control Plane')
    const dataLayer = vm.dashboards.find(d => d.id === 'data-layer')!
    expect(dataLayer.available).toBe(true)
    expect(dataLayer.url).toMatch(/\/d\/bifrost-data-layer\//)
    expect(dataLayer.url).toMatch(/var-namespace=data/)
    const ib = vm.dashboards.find(d => d.id === 'ib-gateway')!
    expect(ib.available).toBe(true)
    expect(ib.url).toMatch(/\/d\/bifrost-ib-gateway\//)
    expect(ib.url).toMatch(/var-namespace=data/)
  })
})

describe('grafana URL builder', () => {
  it('safe + contextual', () => {
    expect(normalizeGrafanaBase('javascript:alert(1)')).toBeNull()
    expect(normalizeGrafanaBase('http://g.example/')).toBe('http://g.example')
    expect(isDashboardCatalogAvailable('agent-operations')).toBe(true)
    expect(isDashboardCatalogAvailable('data-layer')).toBe(true)
    expect(isDashboardCatalogAvailable('ib-gateway')).toBe(true)
    expect(isDashboardCatalogAvailable('satellite-trade-overview')).toBe(true)

    const url = buildGrafanaDashboardUrl({
      grafanaBaseUrl: 'http://grafana.example:30883/',
      dashboardId: 'satellite-trade-overview',
      env: 'stg',
      service: 'api-monitor',
      alertStartMs: 1_700_000_000_000,
    })
    expect(url).toBeTruthy()
    expect(url!).toMatch(/^http:\/\/grafana\.example:30883\/d\/bifrost-trade-overview\//)
    expect(url!).toMatch(/var-namespace=bifrost-stg/)
    expect(url!).toMatch(/var-env=stg/)
    expect(url!).toMatch(/var-service=api-monitor/)
    expect(url!).toMatch(/from=1700000000000/)

    const agentUrl = buildGrafanaDashboardUrl({
      grafanaBaseUrl: 'http://grafana.example',
      dashboardId: 'agent-operations',
    })
    expect(agentUrl).toMatch(/\/d\/bifrost-agent-operations\//)
    expect(agentUrl).toMatch(/var-namespace=bifrost-platform-stg/)

    const dataUrl = buildGrafanaDashboardUrl({
      grafanaBaseUrl: 'http://grafana.example',
      dashboardId: 'data-layer',
      env: 'dev',
    })
    expect(dataUrl).toMatch(/var-namespace=data/)
    // Must NOT inject Trade NS when catalog defaultNamespace is set
    expect(dataUrl).not.toMatch(/var-namespace=bifrost-dev/)

    const ibUrl = buildGrafanaDashboardUrl({
      grafanaBaseUrl: 'http://grafana.example',
      dashboardId: 'ib-gateway',
      env: 'prod',
    })
    expect(ibUrl).toMatch(/var-namespace=data/)
    expect(ibUrl).not.toMatch(/var-namespace=bifrost-prod/)
  })
})
