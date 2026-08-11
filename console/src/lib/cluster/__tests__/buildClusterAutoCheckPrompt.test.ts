import { describe, expect, it } from 'vitest'
import { scopeForPlaybookId } from '@/lib/agent/playbookAgentPrompts'
import { buildClusterAutoCheckBundle } from '@/lib/cluster/buildClusterAutoCheckPrompt'
import type { ClusterSummary } from '@/api/clusterTypes'
import type { MissionSnapshot } from '@/lib/control-room/missionSignals'

function okModule(detail = 'ok') {
  return { signal: 'ok' as const, value: 'ok', detail }
}

function degradedModule(detail: string) {
  return { signal: 'degraded' as const, value: '…', detail }
}

function baseSummary(overrides: Partial<ClusterSummary> = {}): ClusterSummary {
  return {
    cluster_id: 'default',
    reachability: 'ok',
    detail: 'cluster API reachable',
    api_server: 'https://192.168.10.73:6443',
    distribution: 'k3s',
    server_version: 'v1.35.5+k3s1',
    nodes_ready: 6,
    nodes_total: 6,
    failing_pods: 0,
    running_pods: 400,
    pending_pods: 0,
    cpu_allocatable: '112',
    memory_allocatable: '239G',
    ...overrides,
  } as ClusterSummary
}

function missionWithControlAgentDegraded(): MissionSnapshot {
  return {
    infra: okModule('nodes ready'),
    release: okModule('release'),
    control: degradedModule('Platform self-health probing'),
    agent: degradedModule('Agent bridge unknown'),
    tradeDev: okModule(),
    tradeStg: okModule(),
    tradeProd: okModule(),
    rocketOverall: 'degraded',
    payloadOverall: 'ok',
    missionOverall: 'degraded',
  } as MissionSnapshot
}

describe('scopeForPlaybookId — Auto-Check / triage Fix mappings', () => {
  it('maps cluster-issues-full-auto and operator-plane-remediate', () => {
    expect(scopeForPlaybookId('cluster-issues-full-auto')).toBe('cluster_issues_full_auto')
    expect(scopeForPlaybookId('operator-plane-remediate')).toBe('operator-plane-remediate')
    expect(scopeForPlaybookId('platform-self-health-recover')).toBe('platform-self-health-recover')
  })
})

describe('buildClusterAutoCheckBundle', () => {
  it('includes ops triage rows when fleet is green but Control/Agent degraded', () => {
    const bundle = buildClusterAutoCheckBundle({
      summary: baseSummary(),
      missionSnapshot: missionWithControlAgentDegraded(),
    })
    expect(bundle.fleetIssues).toHaveLength(0)
    expect(bundle.hasWork).toBe(true)
    expect(bundle.triageRows.length).toBeGreaterThan(0)
    expect(bundle.prompt).toContain('## Ops failure triage')
    expect(bundle.prompt).toMatch(/Control|Agent/)
    expect(bundle.prompt).toMatch(/platform-self-health-recover|operator-plane-remediate/)
  })

  it('marks verification-only when fleet and triage are clear', () => {
    const snap = missionWithControlAgentDegraded()
    snap.control = okModule('self-health ok')
    snap.agent = okModule('runners ok')
    snap.rocketOverall = 'ok'
    snap.missionOverall = 'ok'
    const bundle = buildClusterAutoCheckBundle({
      summary: baseSummary(),
      missionSnapshot: snap,
    })
    expect(bundle.hasWork).toBe(false)
    expect(bundle.prompt).toContain('verification pass')
  })
})
