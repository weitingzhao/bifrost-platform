import { describe, expect, it } from 'vitest'
import { buildObservabilityViewModel } from '@/lib/observability/observabilityViewModel'
import {
  analyzeObservabilityPack,
  buildObservabilityAgentPack,
  buildObservabilityDiagnosePrefill,
} from '@/lib/observability/observabilityAgentPack'

function healthyLayerBVm(alerts: NonNullable<Parameters<typeof buildObservabilityViewModel>[0]['alerts']>) {
  return buildObservabilityViewModel({
    selectedEnv: 'dev',
    selectedDomain: 'rocket',
    nowMs: Date.parse('2026-08-31T17:00:00Z'),
    alerts,
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
      generated_at: '2026-08-31T16:59:00Z',
    },
    metrics: {
      cluster_id: 'c',
      reachability: 'ok',
      detail: 'ok',
      metrics_server_available: true,
      cpu_usage_percent: 20,
      memory_usage_percent: 30,
      top_pods: [],
      generated_at: '2026-08-31T16:59:00Z',
    },
  })
}

describe('observabilityAgentPack', () => {
  it('lists firing alerts even when system verdict is HEALTHY', () => {
    const vm = healthyLayerBVm([
      {
        labels: { alertname: 'Watchdog', severity: 'info' },
        annotations: { summary: 'Watchdog is firing (expected heartbeat)' },
        state: 'firing',
      },
      {
        labels: { alertname: 'CustomUnmappedAlert', instance: 'node-a' },
        annotations: { summary: 'no catalog rule' },
        state: 'firing',
      },
    ])
    const text = buildObservabilityAgentPack({
      generatedAt: '2026-08-31T17:00:00Z',
      tradeEnv: 'dev',
      namespace: 'bifrost-dev',
      selectedDomain: 'rocket',
      viewModel: vm,
    })
    expect(text).toContain('Copy for Agent')
    expect(text).toContain('## Alerts (Prometheus / Alertmanager)')
    expect(text).toContain('Watchdog')
    expect(text).toContain('CustomUnmappedAlert')
    expect(text).toContain('kind=unmapped')
    expect(text).toContain('kind=info')
    expect(text).toMatch(/info=\d/)
    expect(text).toContain('D10 BLOCKED')
    expect(analyzeObservabilityPack(vm).some(f => /firing alerts/i.test(f.title))).toBe(true)
  })

  it('groups leftover KubeJobFailed by CronJob family and maps to subcontractors', () => {
    const vm = healthyLayerBVm([
      {
        labels: {
          alertname: 'KubeJobFailed',
          severity: 'warning',
          namespace: 'plugin-market-data',
          job_name: 'market-data-stock-eod-29779050',
        },
        annotations: { summary: 'Job failed to complete.' },
        state: 'firing',
      },
      {
        labels: {
          alertname: 'KubeJobFailed',
          severity: 'warning',
          namespace: 'plugin-market-data',
          job_name: 'market-data-stock-eod-29786250',
        },
        annotations: { summary: 'Job failed to complete.' },
        state: 'firing',
      },
      {
        labels: {
          alertname: 'KubeControllerManagerDown',
          severity: 'critical',
          job: 'kube-controller-manager',
        },
        annotations: { summary: 'Target disappeared from Prometheus target discovery.' },
        state: 'firing',
      },
    ])
    const text = buildObservabilityAgentPack({
      generatedAt: '2026-08-31T17:00:00Z',
      tradeEnv: 'dev',
      namespace: 'bifrost-dev',
      selectedDomain: 'rocket',
      viewModel: vm,
    })
    expect(text).toContain('KubeJobFailed ×2')
    expect(text).toContain('job=market-data-stock-eod')
    expect(text).toContain('kind=verdict_affecting')
    expect(text).toContain('kind=expected_neutral')
    expect(text).toContain('KubeControllerManagerDown')
    expect(vm.system.overall).toBe('degraded')
    expect(vm.domains.find(d => d.domain === 'subcontractors')?.verdict).toBe('degraded')
    const rocket = vm.domains.find(d => d.domain === 'rocket')
    expect(rocket?.verdict).not.toBe('critical')
    expect(rocket?.verdict).not.toBe('degraded')
  })

  it('diagnose prefill includes alerts and D10', () => {
    const vm = healthyLayerBVm([
      {
        labels: { alertname: 'Watchdog', severity: 'info' },
        annotations: { summary: 'Watchdog' },
        state: 'firing',
      },
    ])
    const prefill = buildObservabilityDiagnosePrefill({
      generatedAt: '2026-08-31T17:00:00Z',
      tradeEnv: 'dev',
      namespace: 'bifrost-dev',
      selectedDomain: 'rocket',
      viewModel: vm,
    })
    expect(prefill).toContain('Observability — assisted diagnose')
    expect(prefill).toContain('Watchdog')
    expect(prefill).toContain('D10 BLOCKED')
    expect(prefill).toContain('Firing alerts')
  })
})
