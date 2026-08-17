import { describe, expect, it } from 'vitest'
import type { RemediationJob } from '@/api/remediationTypes'
import {
  evidencePatchFromAgentProgress,
  inferPluginLaunchAgentProgress,
} from '@/lib/delivery/pluginLaunchAgentProgress'

function job(partial: Partial<RemediationJob> & Pick<RemediationJob, 'phase' | 'status'>): RemediationJob {
  return {
    id: 'job-1',
    created_at: '2026-08-16T00:00:00Z',
    updated_at: '2026-08-16T00:00:00Z',
    events: [],
    ...partial,
  }
}

describe('inferPluginLaunchAgentProgress', () => {
  it('returns empty when idle', () => {
    expect(inferPluginLaunchAgentProgress(undefined, false).detectDone).toBe(false)
  })

  it('marks Approve awaiting while diagnosing', () => {
    const p = inferPluginLaunchAgentProgress(
      job({ phase: 'diagnosing', status: 'running' }),
      true,
    )
    expect(p.detectDone).toBe(true)
    expect(p.approveAwaiting).toBe(true)
    expect(p.focusStep).toBe('approve')
  })

  it('advances to Verify when install finished and awaiting approval', () => {
    const p = inferPluginLaunchAgentProgress(
      job({
        phase: 'awaiting_approval',
        status: 'running',
        events: [
          {
            id: '1',
            at: '2026-08-16T00:01:00Z',
            type: 'thinking',
            text: 'Installation finished. Now running make verify-ib-gateway-program.',
          },
          {
            id: '2',
            at: '2026-08-16T00:01:01Z',
            type: 'approval_request',
            text: 'Verify 阶段: 执行 make verify-ib-gateway-program 完成 program 验收',
            meta: { kind: 'manual_steps' },
          },
        ],
      }),
      true,
    )
    expect(p.approveDone).toBe(true)
    expect(p.installOutcome).toBe('ok')
    expect(p.verifyAwaiting).toBe(true)
    expect(p.focusStep).toBe('verify')
  })

  it('marks Install in progress while remediating without install-done hint', () => {
    const p = inferPluginLaunchAgentProgress(
      job({ phase: 'remediating', status: 'running' }),
      true,
    )
    expect(p.approveDone).toBe(true)
    expect(p.installOutcome).toBe('pending')
    expect(p.focusStep).toBe('install')
  })

  it('marks all done on phase done', () => {
    const p = inferPluginLaunchAgentProgress(job({ phase: 'done', status: 'done' }), false)
    // job present even if not inFlight
    expect(p.detectDone).toBe(true)
    expect(p.installOutcome).toBe('ok')
    expect(p.verifyOutcome).toBe('ok')
    expect(p.liveOutcome).toBe('ok')
  })
})

describe('evidencePatchFromAgentProgress', () => {
  it('only advances missing evidence fields', () => {
    const patch = evidencePatchFromAgentProgress(
      { installOutcome: 'ok', lastInstallAt: '2026-08-16T00:00:00Z' },
      {
        detectDone: true,
        approveDone: true,
        approveAwaiting: false,
        installOutcome: 'ok',
        verifyAwaiting: true,
        verifyOutcome: undefined,
        liveOutcome: undefined,
        focusStep: 'verify',
        failed: false,
      },
    )
    expect(patch?.lastDetectAt).toBeTruthy()
    expect(patch?.lastApproveAt).toBeTruthy()
    expect(patch?.installOutcome).toBeUndefined()
  })
})
