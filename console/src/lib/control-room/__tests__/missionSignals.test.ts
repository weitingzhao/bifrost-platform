import { describe, expect, it } from 'vitest'
import type { AgentBridgeResponse } from '@/api/agentTypes'
import {
  agentSignal,
  buildMissionSnapshot,
  collectMissionDegradationItems,
} from '@/lib/control-room/missionSignals'
import {
  buildControlRoomBaySignals,
  worstBayScanSignal,
} from '@/lib/control-room/controlRoomBays'

function bridge(partial: {
  gitStatus?: string
  dirty?: number
  runnersOk?: boolean
}): AgentBridgeResponse {
  const runnersOk = partial.runnersOk !== false
  const runner = {
    url: 'http://192.168.10.50:8781',
    role: 'primary' as const,
    status: runnersOk ? 'ok' : 'fail',
    version: '0.1.0',
    active: true,
    cursor_api_key: true,
    service: 'bifrost-remediation-runner',
  }
  return {
    generated_at: '2026-08-29T00:00:00Z',
    remediation_runner: runner,
    runners: [
      runner,
      {
        ...runner,
        url: 'http://192.168.10.52:8781',
        role: 'standby',
        active: false,
      },
    ],
    git_bridge: {
      url: 'http://192.168.10.40:8785',
      status: partial.gitStatus ?? 'ok',
      dirty_repos: partial.dirty ?? 0,
    },
    satellite_probe_bridge: { status: 'ok' },
    hermes_mcp: { status: 'unavailable' },
    nous_hermes: {
      status: 'ok',
      gateway_running: true,
      active_agents: 0,
      active_sessions: 0,
      mcp_tool_count: 0,
    },
    platform_mcp: {
      server_name: 'mcp-server-platform',
      server_version: '0.1.0',
      tool_count: 1,
      implemented_count: 1,
      agent_tool_count: 1,
      transport: 'stdio',
      script_path: '/tmp/x',
    },
    nightly_report: { available: false },
  }
}

describe('agentSignal (seat map — dirty informational)', () => {
  it('keeps ok when Bridge is reachable with dirty repos', () => {
    const state = agentSignal(undefined, bridge({ dirty: 4 }))
    expect(state.signal).toBe('ok')
    expect(state.detail).toContain('Bridge 4 dirty')
    expect(state.detail).toContain('Runners 2/2 (HA)')
  })

  it('fails when Bridge is down even if dirty count is zero', () => {
    const state = agentSignal(undefined, bridge({ gitStatus: 'fail', dirty: 0 }))
    expect(state.signal).toBe('fail')
    expect(state.detail).toContain('Bridge down')
  })

  it('stays ok when Bridge is clean', () => {
    const state = agentSignal(undefined, bridge({ dirty: 0 }))
    expect(state.signal).toBe('ok')
    expect(state.detail).toContain('Bridge clean')
  })
})

describe('buildMissionSnapshot + Control Room bays (dirty alone)', () => {
  it('does not list Agent as degraded when only dirty repos', () => {
    const snap = buildMissionSnapshot({
      bridge: bridge({ dirty: 4 }),
      matrices: [],
    })
    expect(snap.agent.signal).toBe('ok')
    expect(snap.agent.detail).toContain('Bridge 4 dirty')
    expect(collectMissionDegradationItems(snap).filter(i => i.id === 'Agent')).toHaveLength(0)

    // Isolate Agent dirty by seeding a NOMINAL snapshot with dirty agent detail.
    const nominal = {
      infra: { signal: 'ok' as const, value: 'ok', detail: 'ok' },
      release: { signal: 'ok' as const, value: 'ok', detail: 'ok' },
      control: { signal: 'ok' as const, value: 'ok', detail: 'ok' },
      agent: snap.agent,
      tradeDev: { signal: 'ok' as const, value: 'ok', detail: 'ok' },
      tradeStg: { signal: 'ok' as const, value: 'ok', detail: 'ok' },
      tradeProd: { signal: 'ok' as const, value: 'ok', detail: 'ok' },
      rocketOverall: 'ok' as const,
      payloadOverall: 'ok' as const,
      missionOverall: 'ok' as const,
    }
    const bays = buildControlRoomBaySignals({
      snapshot: nominal,
      operateOpenCount: 0,
      pendingBriefCount: 0,
      activeAgentJobCount: 0,
      showHealth: false,
    })
    const mission = bays.find(b => b.id === 'mission')
    const operate = bays.find(b => b.id === 'operate')
    expect(mission?.signal).toBe('ok')
    expect(operate?.signal).toBe('ok')
    expect(worstBayScanSignal(bays)).toBe('ok')
  })

  it('Operate bay can still caution from pending brief without dirty degrading Mission', () => {
    const agent = agentSignal(undefined, bridge({ dirty: 4 }))
    const snap = {
      infra: { signal: 'ok' as const, value: 'ok', detail: 'ok' },
      release: { signal: 'ok' as const, value: 'ok', detail: 'ok' },
      control: { signal: 'ok' as const, value: 'ok', detail: 'ok' },
      agent,
      tradeDev: { signal: 'ok' as const, value: 'ok', detail: 'ok' },
      tradeStg: { signal: 'ok' as const, value: 'ok', detail: 'ok' },
      tradeProd: { signal: 'ok' as const, value: 'ok', detail: 'ok' },
      rocketOverall: 'ok' as const,
      payloadOverall: 'ok' as const,
      missionOverall: 'ok' as const,
    }
    const bays = buildControlRoomBaySignals({
      snapshot: snap,
      pendingBriefCount: 1,
      showHealth: false,
    })
    expect(bays.find(b => b.id === 'mission')?.signal).toBe('ok')
    expect(bays.find(b => b.id === 'operate')?.signal).toBe('degraded')
    expect(worstBayScanSignal(bays)).toBe('degraded')
  })
})
