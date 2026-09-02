import { describe, expect, it } from 'vitest'
import {
  analyzeControlRoomAgent,
  buildControlRoomAgentPack,
  buildControlRoomDiagnosePrefill,
  type ControlRoomAgentPackSnapshot,
} from '@/lib/control-room/controlRoomAgentPack'
import type { MissionSnapshot } from '@/lib/control-room/missionSignals'

function emptyMission(): MissionSnapshot {
  const spoke = { signal: 'ok' as const, value: 'ok', detail: 'ok' }
  return {
    infra: spoke,
    release: spoke,
    control: spoke,
    agent: spoke,
    tradeDev: spoke,
    tradeStg: spoke,
    tradeProd: spoke,
    rocketOverall: 'ok',
    payloadOverall: 'ok',
    missionOverall: 'ok',
  }
}

function baseSnap(over: Partial<ControlRoomAgentPackSnapshot> = {}): ControlRoomAgentPackSnapshot {
  return {
    generatedAt: '2026-09-02T18:00:00.000Z',
    missionOverall: 'ok',
    bayCountsLabel: '6 clear',
    bays: [
      { id: 'mission', label: 'Mission', signal: 'ok', reason: 'Mission probes nominal' },
      { id: 'operate', label: 'Operate', signal: 'ok', reason: 'Agent loop idle' },
    ],
    attention: [],
    operateOpen: [],
    pendingBriefCount: 0,
    activeAgentJobCount: 0,
    spineFocus: null,
    failingMatrix: [],
    ibGateway: null,
    ibGatewayError: null,
    mission: emptyMission(),
    missionError: null,
    ...over,
  }
}

describe('analyzeControlRoomAgent', () => {
  it('flags operate handoffs and attention as needing attention', () => {
    const analysis = analyzeControlRoomAgent(
      baseSnap({
        attention: [
          {
            id: 'bay-operate',
            bayId: 'operate',
            severity: 'warning',
            summary: 'Operate: 6 handoffs',
          },
        ],
        operateOpen: [
          {
            id: 'hq-1',
            program_id: 'p1',
            title: 'Stale handoff A',
            status: 'open',
            created_at: '2026-09-01T00:00:00Z',
            operate_lane: 'troubleshoot',
            risk_level: 'medium',
          },
        ],
        pendingBriefCount: 2,
      }),
    )
    expect(analysis.needsAttention).toBe(true)
    expect(analysis.primaryCause).toMatch(/Operate|handoff|6 handoffs/i)
    expect(analysis.findings.some(f => f.id === 'operate-handoffs')).toBe(true)
    expect(analysis.findings.some(f => f.id === 'pending-briefs')).toBe(true)
  })

  it('flags IB Gateway when deployment is 0/1', () => {
    const analysis = analyzeControlRoomAgent(
      baseSnap({
        ibGateway: {
          summary: 'FAIL — Snapshot not refreshing',
          deployment: {
            namespace: 'data',
            name: 'ib-gateway',
            ready: '0/1',
            mode: 'live',
            reachability: 'fail',
          },
          slots: [],
        },
      }),
    )
    expect(analysis.findings.some(f => f.id === 'ib-gateway')).toBe(true)
    expect(analysis.overall === 'fail' || analysis.overall === 'degraded').toBe(true)
  })
})

describe('buildControlRoomAgentPack', () => {
  it('includes Attention / Operate / D10 / Copy for Agent source line', () => {
    const text = buildControlRoomAgentPack(
      baseSnap({
        attention: [
          {
            id: 'bay-operate',
            bayId: 'operate',
            severity: 'warning',
            summary: 'Operate: 6 handoffs',
          },
        ],
        operateOpen: [
          {
            id: 'hq-1',
            program_id: 'p1',
            title: 'Fix matrix probe',
            status: 'open',
            created_at: '2026-09-01T00:00:00Z',
            description: 'nginx degraded on stg',
            acceptance_criteria: ['matrix stg nginx ok'],
          },
        ],
        spineFocus: 'Stabilize Control Room Attention',
      }),
    )
    expect(text).toContain('Copy for Agent')
    expect(text).toContain('D10 BLOCKED')
    expect(text).toContain('Operate: 6 handoffs')
    expect(text).toContain('Fix matrix probe')
    expect(text).toContain('spine_focus: Stabilize Control Room Attention')
    expect(text).toContain('## Suggested investigation order')
  })

  it('diagnose prefill matches pack text', () => {
    const snap = baseSnap()
    expect(buildControlRoomDiagnosePrefill(snap)).toBe(buildControlRoomAgentPack(snap))
  })
})
