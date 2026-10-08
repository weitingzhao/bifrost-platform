import { describe, expect, it } from 'vitest'
import type { SelfHealthResponse } from '@/api/matrixTypes'
import { shellStatusSentence, SHELL_STATUS_PATH, probesForViewer } from '@/lib/shell/shellStatusLine'
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

describe('shell status sentence', () => {
  it('reads self-health with no environment query', () => {
    expect(SHELL_STATUS_PATH).toBe('/api/v1/self-health')
    expect(SHELL_STATUS_PATH.includes('env=')).toBe(false)
  })

  it('ignores other environments and reports the viewer seat only', () => {
    const body = health({
      viewer_env: 'prod',
      overall: 'fail',
      probes: [
        probe('stg', 'fail', 'staging api down'),
        probe('prod', 'ok', 'up'),
      ],
    })
    expect(probesForViewer(body).map(item => item.env)).toEqual(['prod'])
    expect(shellStatusSentence({ health: body, loading: false, error: null })).toBe('All clear')
  })

  it('uses the worst viewer-env probe as the sentence', () => {
    const body = health({
      viewer_env: 'prod',
      overall: 'ok',
      probes: [
        probe('prod', 'ok', 'up', 'api'),
        probe('prod', 'degraded', 'argo lagging', 'argo'),
      ],
    })
    expect(shellStatusSentence({ health: body, loading: false, error: null })).toBe(
      'Degraded — argo lagging',
    )
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
