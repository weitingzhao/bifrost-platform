import { describe, expect, it } from 'vitest'
import {
  formatOpsIssueAgentClipboard,
  isOpsIssueLane,
  opsIssueNavTarget,
  opsOpenIssueCount,
  resolveActiveSessionBoardMode,
} from '@/lib/briefing/activeSessionLaneMode'
import type { QueueItem, WorkLane } from '@/lib/briefing/workLanes'

const troubleshoot: WorkLane = {
  id: 'troubleshoot',
  track: 'operate',
  componentLine: 'operations',
  trackType: 'maintain',
  label: 'Troubleshooting',
  shortLabel: 'Debug',
  description: 'Failing probes',
  agentMode: 'Ops',
  workIntent: 'debug',
}

const programLane: WorkLane = {
  id: 'trade-iv-radar',
  track: 'build',
  componentLine: 'satellite',
  trackType: 'build',
  label: 'IV Radar',
  shortLabel: 'IV',
  description: 'Program',
  agentMode: 'Ops',
  workIntent: 'feature',
}

describe('activeSessionLaneMode', () => {
  it('classifies troubleshoot as ops-issue lane', () => {
    expect(isOpsIssueLane(troubleshoot)).toBe(true)
    expect(isOpsIssueLane(programLane)).toBe(false)
  })

  it('uses program-phases only when linked programs exist', () => {
    expect(resolveActiveSessionBoardMode(troubleshoot, 0)).toBe('ops-issue-queue')
    expect(resolveActiveSessionBoardMode(programLane, 2)).toBe('program-phases')
    expect(resolveActiveSessionBoardMode(programLane, 0)).toBe('ops-issue-queue')
  })

  it('routes cluster / matrix issue rows to the right desk', () => {
    expect(
      opsIssueNavTarget({ id: 'cluster-pods', label: '5 failing pod(s)', status: 'issue' }),
    ).toBe('cluster')
    expect(
      opsIssueNavTarget({ id: 'matrix-nginx', label: 'Prod probe failing', status: 'issue' }),
    ).toBe('control-room')
    expect(
      opsIssueNavTarget({ id: 'all-clear', label: 'No active issues', status: 'done' }),
    ).toBeNull()
  })

  it('counts open ops issues', () => {
    const queue: QueueItem[] = [
      { id: 'cluster-pods', label: 'pods', status: 'issue' },
      { id: 'all-clear', label: 'clear', status: 'done' },
    ]
    expect(opsOpenIssueCount(queue)).toBe(1)
  })

  it('builds a clipboard pack for Agent', () => {
    const text = formatOpsIssueAgentClipboard(
      { id: 'cluster-pods', label: '5 failing pod(s)', status: 'issue', note: 'CrashLoop' },
      troubleshoot,
    )
    expect(text).toContain('cluster-pods')
    expect(text).toContain('CrashLoop')
    expect(text).toContain('D10')
  })
})
