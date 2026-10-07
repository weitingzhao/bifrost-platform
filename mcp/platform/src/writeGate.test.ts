import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { afterEach, beforeEach, describe, it } from 'node:test'
import { WRITE_SPECS } from './actionTiers.js'
import { approveRequest } from './registerApprove.js'
import { APPROVE_TOOL_NAMES } from './registerApprove.js'
import { LOCAL_TOOL_NAMES } from './registerLocal.js'
import { PLATFORM_STDIO_TOOL_NAMES } from './stdioToolNames.js'
import { pollRequest } from './pollRequest.js'
import { bifrostSessionId } from './sessionId.js'
import { decideWrite, writesEnabled } from './writeGate.js'
import { platformDelete, platformPost, resetClientCacheForTests } from './platformClient.js'

const CD_ACTIONS = [
  'drain_node',
  'ensure_kube_prometheus_stack',
  'ensure_kubeconfig_secret',
  'ensure_metrics_server',
  'gitops_rollback_app',
  'gitops_sync_app',
  'join_cluster_node',
  'poweroff_compute_node',
  'scale_deployment',
  'sign_tier_b',
  'stack_install_addon',
  'stack_upgrade_addon',
  'start_pipeline_run',
]

describe('write tiers', () => {
  it('C and D actions are the approval set', () => {
    const cd = WRITE_SPECS.filter((spec) => spec.tier !== 'B')
      .map((spec) => spec.action)
      .sort()
    assert.deepEqual(cd, [...CD_ACTIONS].sort())
  })

  it('C/D decisions name an approval and do not keep the direct route', () => {
    const sync = decideWrite('POST', '/api/v1/gitops/apps/demo/sync', {}, true)
    assert.equal(sync.kind, 'approval')
    if (sync.kind !== 'approval') return
    assert.equal(sync.request.action, 'gitops_sync_app')
    assert.deepEqual(sync.request.params, { name: 'demo' })
    assert.equal(sync.request.reason, 'mcp:gitops_sync_app')
    assert.ok(sync.request.rollback.length > 0)

    const drain = decideWrite('POST', '/api/v1/cluster/nodes/n1/drain', { force: false }, true)
    assert.equal(drain.kind, 'approval')
    if (drain.kind !== 'approval') return
    assert.equal(drain.request.action, 'drain_node')
    assert.equal(drain.request.params.name, 'n1')
  })

  it('B tier stays a direct call', () => {
    const restart = decideWrite(
      'POST',
      '/api/v1/cluster/workloads/rollout-restart',
      { namespace: 'ns', kind: 'Deployment', name: 'api' },
      true,
    )
    assert.equal(restart.kind, 'direct')
  })

  it('writes default off', () => {
    assert.equal(writesEnabled({}), false)
    assert.equal(writesEnabled({ MCP_WRITES: 'off' }), false)
    assert.equal(writesEnabled({ MCP_WRITES: 'on' }), true)
    const blocked = decideWrite('POST', '/api/v1/gitops/apps/demo/sync', {}, false)
    assert.equal(blocked.kind, 'blocked')
    if (blocked.kind !== 'blocked') return
    assert.equal(blocked.body.error, 'writes not cut over')
    assert.equal(blocked.body.action, 'gitops_sync_app')
  })
})

describe('platformClient write gate', () => {
  const originalFetch = globalThis.fetch
  const envKeys = ['PLATFORM_API_URL', 'PLATFORM_OPERATOR_TOKEN', 'PLATFORM_TOKEN_ENV_KEY', 'MCP_WRITES', 'CLAUDE_CODE_HOST_SESSION_ID'] as const
  const saved: Record<string, string | undefined> = {}
  let calls: Array<{ url: string; method: string; body?: unknown; session?: string }>

  beforeEach(() => {
    for (const key of envKeys) saved[key] = process.env[key]
    process.env.PLATFORM_API_URL = 'http://192.168.10.100:30876'
    process.env.PLATFORM_OPERATOR_TOKEN = 'test-operator-not-a-real-token'
    delete process.env.PLATFORM_TOKEN_ENV_KEY
    process.env.MCP_WRITES = 'on'
    process.env.CLAUDE_CODE_HOST_SESSION_ID = 'local_test_session'
    resetClientCacheForTests()
    calls = []
    globalThis.fetch = (async (url: string | URL | Request, init?: RequestInit) => {
      const headers = new Headers(init?.headers)
      const raw = init?.body == null ? undefined : JSON.parse(String(init.body))
      calls.push({
        url: String(url),
        method: init?.method ?? 'GET',
        body: raw,
        session: headers.get('X-Bifrost-Session') ?? undefined,
      })
      const target = String(url)
      if (target.endsWith('/api/v1/approvals')) {
        const action = raw && typeof raw === 'object' ? (raw as { action?: string }).action : ''
        return new Response(JSON.stringify({ id: 'apr-test', status: 'pending', action }), {
          status: 201,
          headers: { 'Content-Type': 'application/json' },
        })
      }
      if (target.includes('/rollout-restart') || target.endsWith('/approve') || (init?.method ?? 'GET') === 'DELETE') {
        return new Response(JSON.stringify({ status: 'executed' }), { status: 200 })
      }
      return new Response('unexpected direct call', { status: 500 })
    }) as typeof fetch
  })

  afterEach(() => {
    globalThis.fetch = originalFetch
    for (const key of envKeys) {
      if (saved[key] === undefined) delete process.env[key]
      else process.env[key] = saved[key]
    }
    resetClientCacheForTests()
  })

  it('C/D write tools return an approval id and do not call the direct route', async () => {
    const created = (await platformPost('/api/v1/gitops/apps/demo/sync')) as { id: string; status: string }
    assert.equal(created.id, 'apr-test')
    assert.equal(created.status, 'pending')
    assert.equal(calls.length, 1)
    assert.ok(calls[0].url.endsWith('/api/v1/approvals'))
    assert.equal(calls[0].url.includes('/sync'), false)
    assert.equal((calls[0].body as { action: string }).action, 'gitops_sync_app')
    assert.equal(calls[0].session, 'local_test_session')

    calls.length = 0
    const drained = (await platformPost('/api/v1/cluster/nodes/n1/poweroff')) as { id: string }
    assert.equal(drained.id, 'apr-test')
    assert.equal(calls.length, 1)
    assert.equal((calls[0].body as { action: string }).action, 'poweroff_compute_node')
    assert.equal(calls[0].url.includes('/poweroff'), false)

    calls.length = 0
    const removed = (await platformDelete('/api/v1/delivery/runs/run-1')) as { status?: string }
    assert.equal(removed.status, 'executed')
    assert.equal(calls.length, 1)
    assert.ok(calls[0].url.includes('/delivery/runs/run-1'))
    assert.equal(calls[0].method, 'DELETE')
  })

  it('does not call fetch while writes are off', async () => {
    process.env.MCP_WRITES = 'off'
    const blocked = (await platformPost('/api/v1/delivery/pipelines/bifrost-deliver-platform-prod/runs', {
      revision: 'abc',
    })) as { error: string; action: string }
    assert.equal(blocked.error, 'writes not cut over')
    assert.equal(blocked.action, 'start_pipeline_run')
    assert.equal(calls.length, 0)
  })

  it('B tier calls the route itself when writes are on', async () => {
    const result = (await platformPost('/api/v1/cluster/workloads/rollout-restart', {
      namespace: 'bifrost-platform-prod',
      kind: 'Deployment',
      name: 'platform-api',
    })) as { status: string }
    assert.equal(result.status, 'executed')
    assert.equal(calls.length, 1)
    assert.ok(calls[0].url.endsWith('/api/v1/cluster/workloads/rollout-restart'))
    assert.equal(calls[0].url.includes('/approvals'), false)
  })

  it('approve_request posts channel chat and is not a full-server tool', async () => {
    await approveRequest('apr-9')
    assert.equal(calls.length, 1)
    assert.ok(calls[0].url.endsWith('/api/v1/approvals/apr-9/approve'))
    assert.deepEqual(calls[0].body, { channel: 'chat' })

    const platform = new Set<string>(PLATFORM_STDIO_TOOL_NAMES)
    const local = new Set<string>(LOCAL_TOOL_NAMES)
    for (const name of APPROVE_TOOL_NAMES) {
      assert.equal(platform.has(name), false, name)
      assert.equal(local.has(name), false, name)
    }
    const index = readFileSync(new URL('./index.ts', import.meta.url), 'utf8')
    const localSrc = readFileSync(new URL('./registerLocal.ts', import.meta.url), 'utf8')
    const approveSrc = readFileSync(new URL('./registerApprove.ts', import.meta.url), 'utf8')
    assert.equal(index.includes("'approve_request'"), false)
    assert.equal(index.includes("'reject_request'"), false)
    assert.equal(localSrc.includes('approve_request'), false)
    for (const name of APPROVE_TOOL_NAMES) {
      assert.equal(approveSrc.includes(`'${name}'`), true)
    }
    assert.match(approveSrc, /channel:\s*'chat'/)
    assert.equal(localSrc.includes('platformSend'), true)
  })
})

describe('session and poll', () => {
  it('prefers the Claude host session, then Cursor, then the hostname', () => {
    assert.equal(
      bifrostSessionId({ CLAUDE_CODE_HOST_SESSION_ID: 'local_abc', CURSOR_CONVERSATION_ID: 'cursor' }, () => 'host'),
      'local_abc',
    )
    assert.equal(bifrostSessionId({ CURSOR_CONVERSATION_ID: 'cursor-1' }, () => 'host'), 'cursor-1')
    assert.equal(bifrostSessionId({}, () => 'mac-pro'), 'mac-pro')
  })

  it('stops on executed, failed, rejected, expired, or timeout', async () => {
    const statuses = ['pending', 'executed']
    const done = await pollRequest({
      id: 'apr-1',
      timeoutMs: 10_000,
      intervalMs: 1,
      now: () => 0,
      sleep: async () => {},
      get: async () => ({ id: 'apr-1', status: statuses.shift() }),
    })
    assert.equal(done.status, 'executed')

    let ticks = 0
    const timed = await pollRequest({
      id: 'apr-2',
      timeoutMs: 5,
      intervalMs: 10,
      now: () => ticks++ * 5,
      sleep: async () => {},
      get: async () => ({ id: 'apr-2', status: 'pending' }),
    })
    assert.equal(timed.wait, 'timeout')
    assert.equal(timed.status, 'pending')
  })
})
