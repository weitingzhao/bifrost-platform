#!/usr/bin/env node
/**
 * Agent Capability live view-model — deterministic unit tests (no test framework).
 * Usage: npx tsx scripts/agent-capability-viewmodel-test.ts
 */
import assert from 'node:assert/strict'
import type { AgentBridgeResponse, RemediationJob } from '../src/api/types'
import type { AgentTaskEntry } from '../src/lib/agent/agentTaskCatalog'
import {
  activeJobIdForTask,
  buildAgentCapabilityViewModel,
  nodeMatchesFilter,
} from '../src/lib/agent/agentCapabilityViewModel'

let passed = 0
function check(name: string, fn: () => void) {
  try {
    fn()
    passed += 1
    console.log(`ok — ${name}`)
  } catch (e) {
    console.error(`FAIL — ${name}`)
    throw e
  }
}

function task(partial: Partial<AgentTaskEntry> & Pick<AgentTaskEntry, 'id' | 'scope'>): AgentTaskEntry {
  return {
    label: partial.label ?? partial.id,
    domain: partial.domain ?? 'Platform',
    action: partial.action ?? 'Remediate',
    tier: partial.tier ?? 'manual',
    entryPoint: '—',
    trigger: '—',
    description: '',
    ...partial,
  }
}

function job(
  partial: Partial<RemediationJob> & Pick<RemediationJob, 'id' | 'status' | 'phase' | 'scope'>,
): RemediationJob {
  return {
    created_at: '2026-07-21T12:00:00Z',
    updated_at: '2026-07-21T12:05:00Z',
    ...partial,
  }
}

const bridgeOk: AgentBridgeResponse = {
  generated_at: '2026-07-21T12:00:00Z',
  remediation_runner: { url: 'http://x', status: 'ok' },
  git_bridge: { status: 'ok' },
  satellite_probe_bridge: { status: 'ok' },
  hermes_mcp: { status: 'ok' },
}

const bridgeDown: AgentBridgeResponse = {
  ...bridgeOk,
  remediation_runner: { url: 'http://x', status: 'unavailable' },
}

const catalog = [
  task({ id: 'release', scope: 'release', domain: 'Platform', action: 'Release' }),
  task({ id: 'cluster-auto', scope: 'cluster_issues_full_auto', domain: 'Cluster', aliases: ['cluster'] }),
  task({ id: 'drift-brief', scope: 'drift-brief', domain: 'Drift', action: 'Brief', tier: 'automated' }),
]

check('idle when runtime ok and no jobs', () => {
  const vm = buildAgentCapabilityViewModel({ tasks: catalog, jobs: [], bridge: bridgeOk })
  assert.equal(vm.strip.runtimeReachable, true)
  assert.equal(vm.strip.idle, 3)
  assert.equal(vm.statusByTaskId.release, 'idle')
})

check('running and awaiting from live jobs', () => {
  const vm = buildAgentCapabilityViewModel({
    tasks: catalog,
    jobs: [
      job({
        id: 'j1',
        scope: 'release',
        status: 'running',
        phase: 'remediating',
        updated_at: '2026-07-21T13:00:00Z',
      }),
      job({
        id: 'j2',
        scope: 'cluster_issues_full_auto',
        status: 'running',
        phase: 'awaiting_approval',
        updated_at: '2026-07-21T13:01:00Z',
      }),
    ],
    bridge: bridgeOk,
  })
  assert.equal(vm.statusByTaskId.release, 'running')
  assert.equal(vm.statusByTaskId['cluster-auto'], 'awaiting')
  assert.equal(vm.strip.running, 1)
  assert.equal(vm.strip.awaiting, 1)
  assert.equal(activeJobIdForTask(vm.nodes.find(n => n.task.id === 'release')!), 'j1')
})

check('failed from latest job; alias scope matches', () => {
  const vm = buildAgentCapabilityViewModel({
    tasks: catalog,
    jobs: [
      job({
        id: 'j3',
        scope: 'cluster',
        status: 'failed',
        phase: 'done',
        updated_at: '2026-07-21T14:00:00Z',
      }),
    ],
    bridge: bridgeOk,
  })
  assert.equal(vm.statusByTaskId['cluster-auto'], 'failed')
  assert.equal(vm.strip.failed, 1)
})

check('ready after successful job', () => {
  const vm = buildAgentCapabilityViewModel({
    tasks: catalog,
    jobs: [
      job({
        id: 'j4',
        scope: 'drift-brief',
        status: 'done',
        phase: 'done',
      }),
    ],
    bridge: bridgeOk,
  })
  assert.equal(vm.statusByTaskId['drift-brief'], 'ready')
})

check('degraded when runner unreachable even with catalog', () => {
  const vm = buildAgentCapabilityViewModel({
    tasks: catalog,
    jobs: [],
    bridge: bridgeDown,
  })
  assert.equal(vm.strip.runtimeReachable, false)
  assert.equal(vm.strip.degraded, 3)
  assert.ok(vm.summaryLine.includes('unreachable') || vm.summaryLine.includes('degraded'))
})

check('bridge unavailable wins over health ok', () => {
  const vm = buildAgentCapabilityViewModel({
    tasks: catalog,
    jobs: [],
    bridge: bridgeDown,
    health: { status: 'ok', service: 'remediation' },
  })
  assert.equal(vm.strip.runtimeReachable, false)
  assert.equal(vm.strip.degraded, 3)
})

check('health ok fallback when bridge missing', () => {
  const vm = buildAgentCapabilityViewModel({
    tasks: catalog,
    jobs: [],
    health: { status: 'ok', service: 'remediation' },
  })
  assert.equal(vm.strip.runtimeReachable, true)
})

check('filter attention vs ready', () => {
  assert.equal(nodeMatchesFilter('failed', 'attention'), true)
  assert.equal(nodeMatchesFilter('idle', 'attention'), false)
  assert.equal(nodeMatchesFilter('ready', 'ready'), true)
  assert.equal(nodeMatchesFilter('idle', 'ready'), true)
  assert.equal(nodeMatchesFilter('running', 'ready'), false)
})

check('escalation edges soft-highlight when hot', () => {
  const vm = buildAgentCapabilityViewModel({
    tasks: [
      task({ id: 'release', scope: 'release', action: 'Release' }),
      task({ id: 'release-fix', scope: 'release-fix', action: 'Release Fix', tier: 'escalation' }),
    ],
    jobs: [
      job({
        id: 'jf',
        scope: 'release',
        status: 'failed',
        phase: 'done',
      }),
    ],
    bridge: bridgeOk,
  })
  assert.ok(vm.highlightedEdgeKeys.includes('release→release-fix'))
})

console.log(`\n${passed} checks passed`)
