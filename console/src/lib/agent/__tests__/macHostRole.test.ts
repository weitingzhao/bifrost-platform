import { describe, expect, it } from 'vitest'
import type { AgentBridgeResponse } from '@/api/agentTypes'
import {
  bridgeRunners,
  buildMacAgentRoleByHost,
  findRunnerForDeployTarget,
  hostKeyFromEndpoint,
  macAgentRoleLabel,
  resolveMacAgentRole,
  runnerStatusReach,
} from '@/lib/agent/macHostRole'

function stubBridge(partial: Partial<AgentBridgeResponse>): AgentBridgeResponse {
  return {
    generated_at: '2026-07-24T00:00:00Z',
    remediation_runner: { url: 'http://192.168.10.50:8781', role: 'primary', status: 'ok' },
    git_bridge: { status: 'ok' },
    satellite_probe_bridge: { status: 'not_configured' },
    hermes_mcp: { status: 'not_configured' },
    nous_hermes: {
      status: 'not_configured',
      gateway_running: false,
      active_agents: 0,
      active_sessions: 0,
      mcp_tool_count: 0,
    },
    platform_mcp: {
      server_name: 'test',
      server_version: '0',
      tool_count: 0,
      implemented_count: 0,
      agent_tool_count: 0,
      transport: 'stdio',
      script_path: '',
    },
    nightly_report: { available: false },
    ...partial,
  }
}

describe('macHostRole', () => {
  it('parses runner URL and SSH remote host keys', () => {
    expect(hostKeyFromEndpoint('http://192.168.10.50:8781')).toBe('192.168.10.50')
    expect(hostKeyFromEndpoint('vision@192.168.10.52')).toBe('192.168.10.52')
    expect(hostKeyFromEndpoint('')).toBeNull()
  })

  it('maps only bridge runners with roles (no blind LAN tag)', () => {
    const bridge = stubBridge({
      runners: [
        { url: 'http://192.168.10.50:8781', role: 'primary', status: 'ok' },
        { url: 'http://192.168.10.52:8781', role: 'standby', status: 'unavailable' },
      ],
    })
    const map = buildMacAgentRoleByHost(bridge)
    expect(map['192.168.10.50']).toBe('primary')
    expect(map['192.168.10.52']).toBe('standby')
    expect(resolveMacAgentRole('192.168.10.60', map)).toBeUndefined()
    expect(macAgentRoleLabel('standby')).toBe('Standby')
  })

  it('falls back to remediation_runner when runners[] empty', () => {
    const bridge = stubBridge({ runners: [] })
    expect(bridgeRunners(bridge)).toHaveLength(1)
    expect(buildMacAgentRoleByHost(bridge)['192.168.10.50']).toBe('primary')
  })

  it('finds runner for deploy target by role then host', () => {
    const bridge = stubBridge({
      runners: [
        { url: 'http://192.168.10.50:8781', role: 'primary', status: 'ok' },
        { url: 'http://192.168.10.52:8781', role: 'standby', status: 'unavailable' },
      ],
    })
    expect(
      findRunnerForDeployTarget(bridge, { role: 'standby', remote: 'vision@192.168.10.52' })?.status,
    ).toBe('unavailable')
    expect(runnerStatusReach('ok')).toBe('ok')
    expect(runnerStatusReach('unavailable')).toBe('fail')
    expect(runnerStatusReach('not_configured')).toBe('unknown')
  })
})
