import { describe, expect, it } from 'vitest'
import type { ChecklistSignalsResponse } from '@/api/checklist'
import type { TelemetryAlertsResponse } from '@/api/clusterTypes'
import type { SelfHealthResponse } from '@/api/matrixTypes'
import {
  SHELL_STATUS_PATH,
  probesForViewer,
  systemVerdict,
  type VerdictInput,
} from '@/lib/shell/shellStatusLine'
import { showDevSessions } from '@/lib/shell/localConsole'

function health(partial: Partial<SelfHealthResponse> & Pick<SelfHealthResponse, 'probes' | 'viewer_env'>): SelfHealthResponse {
  return {
    generated_at: '2026-10-08T00:00:00Z',
    overall: 'fail',
    ...partial,
  }
}

function probe(env: string, status: 'ok' | 'degraded' | 'fail', detail: string, id = 'api') {
  return { id, category: 'control', env, status, detail, latency_ms: 1 }
}

/** Shapes follow GET /checklist/signals and GET /telemetry/alerts as PROD returned them on 2026-10-10. */
function checklist(signals: Array<[string, string]>): ChecklistSignalsResponse {
  return {
    updated_at: '2026-10-10T07:21:54Z',
    last_run_id: 'prober-20261010T072154Z',
    source: 'checklist-prober',
    signals: signals.map(([item_id, signal]) => ({ item_id, signal, detail: '', env: 'prod' })),
    quiet_success_streak: 0,
  }
}

function alerts(rows: Array<[string, string, string]>): TelemetryAlertsResponse {
  return {
    prometheus_url: 'http://kube-prometheus-stack-prometheus.monitoring.svc.cluster.local:9090',
    generated_at: '2026-10-10T07:25:00Z',
    alerts: rows.map(([alertname, severity, state]) => ({
      labels: { alertname, severity, namespace: 'monitoring' },
      annotations: {},
      state,
    })),
  }
}

const okHealth = health({ viewer_env: 'prod', overall: 'ok', probes: [probe('prod', 'ok', 'HTTP 200')] })

function input(partial: Partial<VerdictInput>): VerdictInput {
  return {
    health: { data: okHealth, loading: false, error: null },
    checklist: { data: checklist([['redis', 'ok']]), loading: false, error: null },
    alerts: { data: alerts([]), loading: false, error: null },
    ...partial,
  }
}

describe('system verdict', () => {
  it('reads self-health with no environment query', () => {
    expect(SHELL_STATUS_PATH).toBe('/api/v1/self-health')
    expect(SHELL_STATUS_PATH.includes('env=')).toBe(false)
  })

  it('scopes self-health to the viewer seat', () => {
    const body = health({
      viewer_env: 'prod',
      probes: [probe('stg', 'fail', 'staging api down'), probe('prod', 'ok', 'up')],
    })
    expect(probesForViewer(body).map(item => item.env)).toEqual(['prod'])
    expect(systemVerdict(input({ health: { data: body, loading: false, error: null } })).sentence).toBe(
      'All clear',
    )
  })

  it('says Degraded, not All clear, when warning alerts fire (the 2026-10-10 PROD state)', () => {
    const verdict = systemVerdict(
      input({
        checklist: {
          data: checklist([
            ['redis', 'ok'],
            ['ib-feed', 'unknown'],
            ['hermes-tooling', 'unknown'],
          ]),
          loading: false,
          error: null,
        },
        alerts: {
          data: alerts([
            ['KubePodNotReady', 'warning', 'firing'],
            ['TargetDown', 'warning', 'firing'],
            ['BifrostMarketDataWarningLingering', 'warning', 'pending'],
            ['BifrostElasticStandbyMarker', 'info', 'firing'],
            ['Watchdog', 'none', 'firing'],
          ]),
          loading: false,
          error: null,
        },
      }),
    )
    expect(verdict.tone).toBe('degraded')
    expect(verdict.sentence).toBe('Degraded — 2 warning alerts')
  })

  it('says Failing on a critical alert or a failed checklist item', () => {
    const critical = systemVerdict(
      input({ alerts: { data: alerts([['PostgresDown', 'critical', 'firing']]), loading: false, error: null } }),
    )
    expect(critical.tone).toBe('failing')
    expect(critical.sentence).toBe('Failing — 1 critical alert')

    const failed = systemVerdict(
      input({
        checklist: { data: checklist([['redis', 'fail'], ['postgres', 'degraded']]), loading: false, error: null },
        alerts: { data: alerts([['KubePodNotReady', 'warning', 'firing']]), loading: false, error: null },
      }),
    )
    expect(failed.tone).toBe('failing')
    expect(failed.sentence.startsWith('Failing — ')).toBe(true)
    expect(failed.causes.map(cause => cause.level)).toEqual(['fail', 'degraded', 'degraded'])
  })

  it('counts a degraded or failing self-health probe on the seat', () => {
    const body = health({ viewer_env: 'prod', probes: [probe('prod', 'degraded', 'argo lagging', 'argo')] })
    expect(systemVerdict(input({ health: { data: body, loading: false, error: null } })).sentence).toBe(
      'Degraded — argo',
    )
  })

  it('shortens long cause lists', () => {
    const verdict = systemVerdict(
      input({
        checklist: {
          data: checklist([['redis', 'fail'], ['postgres', 'fail'], ['nodes-ready', 'fail']]),
          loading: false,
          error: null,
        },
      }),
    )
    expect(verdict.sentence.endsWith(' · +1 more')).toBe(true)
  })

  it('never says All clear while a source is unreadable', () => {
    const verdict = systemVerdict(
      input({ alerts: { data: undefined, loading: false, error: 'telemetry alerts: HTTP 502' } }),
    )
    expect(verdict.tone).toBe('unknown')
    expect(verdict.sentence).toBe('Unknown — alerts unreadable')
  })

  it('still reports what it can read when another source fails', () => {
    const verdict = systemVerdict(
      input({
        checklist: { data: undefined, loading: false, error: 'HTTP 500' },
        alerts: { data: alerts([['KubeJobFailed', 'warning', 'firing']]), loading: false, error: null },
      }),
    )
    expect(verdict.tone).toBe('degraded')
    expect(verdict.unreadable).toEqual(['checklist'])
  })

  it('waits for the first read', () => {
    const verdict = systemVerdict(input({ checklist: { data: undefined, loading: true, error: null } }))
    expect(verdict.tone).toBe('loading')
    expect(verdict.sentence).toBe('Checking health…')
  })
})

describe('showDevSessions', () => {
  it('hides the entry on PROD and STG production builds', () => {
    expect(showDevSessions({ viewerEnv: 'prod', viewerEnvLoading: false, devBuild: false })).toBe(false)
    expect(showDevSessions({ viewerEnv: 'stg', viewerEnvLoading: false, devBuild: false })).toBe(false)
    expect(showDevSessions({ viewerEnv: 'prod', viewerEnvLoading: true, devBuild: false })).toBe(false)
  })

  it('shows the entry for a local viewer or a dev build', () => {
    expect(showDevSessions({ viewerEnv: 'dev', viewerEnvLoading: false, devBuild: false })).toBe(true)
    expect(showDevSessions({ viewerEnv: 'dev-local', viewerEnvLoading: false, devBuild: false })).toBe(true)
    expect(showDevSessions({ viewerEnv: 'prod', viewerEnvLoading: false, devBuild: true })).toBe(true)
  })
})
