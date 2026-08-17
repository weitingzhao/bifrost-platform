import { describe, expect, it } from 'vitest'
import type { RemediationJob } from '@/api/remediationTypes'
import {
  evidencePatchFromAgentLaunchProgress,
  inferAgentLaunchAgentProgress,
} from '@/lib/delivery/agentLaunchAgentProgress'

function job(partial: Partial<RemediationJob> & Pick<RemediationJob, 'phase' | 'status'>): RemediationJob {
  return {
    id: 'job-agent-1',
    created_at: '2026-08-16T00:00:00Z',
    updated_at: '2026-08-16T00:00:00Z',
    events: [],
    ...partial,
  }
}

describe('inferAgentLaunchAgentProgress', () => {
  it('returns empty when idle', () => {
    expect(inferAgentLaunchAgentProgress(undefined, false).detectDone).toBe(false)
  })

  it('marks Approve awaiting while diagnosing', () => {
    const p = inferAgentLaunchAgentProgress(
      job({ phase: 'diagnosing', status: 'running' }),
      true,
    )
    expect(p.detectDone).toBe(true)
    expect(p.approveAwaiting).toBe(true)
    expect(p.focusStep).toBe('approve')
  })

  it('advances to Deploy while remediating', () => {
    const p = inferAgentLaunchAgentProgress(
      job({ phase: 'remediating', status: 'running' }),
      true,
    )
    expect(p.approveDone).toBe(true)
    expect(p.deployOutcome).toBe('pending')
    expect(p.focusStep).toBe('deploy')
  })

  it('advances to Verify when deploy finished and awaiting approval', () => {
    const p = inferAgentLaunchAgentProgress(
      job({
        phase: 'awaiting_approval',
        status: 'running',
        events: [
          {
            id: '1',
            at: '2026-08-16T00:01:00Z',
            type: 'thinking',
            text: 'Host deploy finished. Now recheck get_agent_bridge.',
          },
          {
            id: '2',
            at: '2026-08-16T00:01:01Z',
            type: 'approval_request',
            text: 'Verify stage: confirm runner heartbeat ok',
            meta: { kind: 'manual_steps' },
          },
        ],
      }),
      true,
    )
    expect(p.approveDone).toBe(true)
    expect(p.deployOutcome).toBe('ok')
    expect(p.verifyAwaiting).toBe(true)
    expect(p.focusStep).toBe('verify')
  })

  it('marks all done on phase done', () => {
    const p = inferAgentLaunchAgentProgress(job({ phase: 'done', status: 'done' }), false)
    expect(p.deployOutcome).toBe('ok')
    expect(p.verifyOutcome).toBe('ok')
    expect(p.liveOutcome).toBe('ok')
  })
})

describe('evidencePatchFromAgentLaunchProgress', () => {
  it('only advances missing evidence fields', () => {
    const patch = evidencePatchFromAgentLaunchProgress(
      { deployOutcome: 'ok', lastDeployAt: '2026-08-16T00:00:00Z' },
      {
        detectDone: true,
        approveDone: true,
        approveAwaiting: false,
        deployOutcome: 'ok',
        verifyAwaiting: true,
        verifyOutcome: undefined,
        liveOutcome: undefined,
        focusStep: 'verify',
        failed: false,
      },
    )
    expect(patch?.lastDetectAt).toBeTruthy()
    expect(patch?.lastApproveAt).toBeTruthy()
    expect(patch?.deployOutcome).toBeUndefined()
  })
})
