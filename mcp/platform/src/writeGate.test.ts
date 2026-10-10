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
import { OWNER_WAIT_NOTE, planWrite, writesEnabled, writesMode } from './writeGate.js'
import { platformDelete, platformPost, platformPut, resetClientCacheForTests } from './platformClient.js'

const catalogIds = JSON.parse(
  readFileSync(new URL('../../../config/actions-catalog.json', import.meta.url), 'utf8'),
) as { actions: string[] }

describe('write mode', () => {
  it('unset is legacy, off sends nothing, on is explicit', () => {
    assert.equal(writesMode({}), 'legacy')
    assert.equal(writesMode({ MCP_WRITES: '' }), 'legacy')
    assert.equal(writesMode({ MCP_WRITES: 'off' }), 'off')
    assert.equal(writesMode({ MCP_WRITES: 'on' }), 'on')
    assert.equal(writesEnabled({}), false)
    assert.equal(writesEnabled({ MCP_WRITES: 'on' }), true)

    const legacy = planWrite('POST', '/api/v1/gitops/apps/demo/sync', {}, 'legacy')
    assert.equal(legacy.kind, 'direct')

    const blocked = planWrite('POST', '/api/v1/gitops/apps/demo/sync', {}, 'off')
    assert.equal(blocked.kind, 'blocked')
    if (blocked.kind !== 'blocked') return
    assert.equal(blocked.body.error, 'writes not cut over')
    assert.equal(blocked.body.action, 'gitops_sync_app')
  })

  it('maps the four catalog routes that had no mapping', () => {
    const sweep = planWrite('POST', '/api/v1/cluster/postgres/backups/sweep-failed', {}, 'on')
    assert.equal(sweep.kind, 'consult')
    if (sweep.kind === 'consult') assert.equal(sweep.action, 'sweep_failed_backups')

    const sync = planWrite('POST', '/api/v1/cluster/sync-kubeconfig', {}, 'on')
    assert.equal(sync.kind, 'consult')
    if (sync.kind === 'consult') assert.equal(sync.action, 'sync_kubeconfig')

    const schedule = planWrite('PUT', '/api/v1/cluster/data-clone/schedule', { enabled: false }, 'on')
    assert.equal(schedule.kind, 'consult')
    if (schedule.kind === 'consult') {
      assert.equal(schedule.action, 'update_data_clone_schedule')
      assert.deepEqual(schedule.params, { enabled: false })
    }

    const deleted = planWrite('DELETE', '/api/v1/plugins/market-data/api/contracts/ES', undefined, 'on')
    assert.equal(deleted.kind, 'consult')
    if (deleted.kind === 'consult') {
      assert.equal(deleted.action, 'market_data_delete')
      assert.deepEqual(deleted.params, { path: 'contracts/ES' })
    }
  })

  it('refuses an unmapped write when on and does not invent unlisted_write', () => {
    const plan = planWrite('POST', '/api/v1/cluster/namespaces/ensure-bifrost', {}, 'on')
    assert.equal(plan.kind, 'unmapped')
    if (plan.kind !== 'unmapped') return
    assert.equal(plan.body.error, 'unmapped write')
    assert.equal(JSON.stringify(plan.body).includes('unlisted_write'), false)
  })
})

describe('action catalog ratchet', () => {
  it('every mapping is a catalog id and every MCP-exposed catalog action is mapped', () => {
    const catalog = new Set(catalogIds.actions)
    const mapped = new Set(WRITE_SPECS.map((spec) => spec.action))
    for (const spec of WRITE_SPECS) {
      assert.equal(catalog.has(spec.action), true, spec.action)
    }
    const exposed = new Set<string>([...PLATFORM_STDIO_TOOL_NAMES, ...LOCAL_TOOL_NAMES])
    for (const id of catalogIds.actions) {
      if (!exposed.has(id)) continue
      assert.equal(mapped.has(id), true, id)
    }
  })
})

describe('platformClient write gate', () => {
  const originalFetch = globalThis.fetch
  const envKeys = ['PLATFORM_API_URL', 'PLATFORM_OPERATOR_TOKEN', 'PLATFORM_TOKEN_ENV_KEY', 'MCP_WRITES', 'CLAUDE_CODE_HOST_SESSION_ID'] as const
  const saved: Record<string, string | undefined> = {}
  let calls: Array<{ url: string; method: string; body?: unknown; session?: string }>
  let approvalStatus: number
  let approvalBody: Record<string, unknown>

  beforeEach(() => {
    for (const key of envKeys) saved[key] = process.env[key]
    process.env.PLATFORM_API_URL = 'http://192.168.10.100:30876'
    process.env.PLATFORM_OPERATOR_TOKEN = 'test-operator-not-a-real-token'
    delete process.env.PLATFORM_TOKEN_ENV_KEY
    process.env.MCP_WRITES = 'on'
    process.env.CLAUDE_CODE_HOST_SESSION_ID = 'local_test_session'
    resetClientCacheForTests()
    calls = []
    approvalStatus = 201
    approvalBody = { id: 'apr-test', status: 'pending', action: 'gitops_sync_app' }
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
        return new Response(JSON.stringify(approvalBody), {
          status: approvalStatus,
          headers: { 'Content-Type': 'application/json' },
        })
      }
      return new Response(JSON.stringify({ status: 'executed' }), { status: 200 })
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

  it('unset calls the original route and does not ask for an approval', async () => {
    delete process.env.MCP_WRITES
    const result = (await platformPost('/api/v1/gitops/apps/demo/sync')) as { status: string }
    assert.equal(result.status, 'executed')
    assert.equal(calls.length, 1)
    assert.ok(calls[0].url.endsWith('/api/v1/gitops/apps/demo/sync'))
    assert.equal(calls[0].url.includes('/approvals'), false)
  })

  it('on plus call-directly calls the original route', async () => {
    approvalStatus = 400
    approvalBody = { error: 'call directly', action: 'start_pipeline_run', tier: 'B' }
    const result = (await platformPost('/api/v1/delivery/pipelines/bifrost-deliver-platform/runs', {
      revision: 'abc',
    })) as { status: string }
    assert.equal(result.status, 'executed')
    assert.equal(calls.length, 2)
    assert.ok(calls[0].url.endsWith('/api/v1/approvals'))
    assert.equal((calls[0].body as { action: string }).action, 'start_pipeline_run')
    assert.equal((calls[0].body as { params: { name: string } }).params.name, 'bifrost-deliver-platform')
    assert.ok(calls[1].url.endsWith('/api/v1/delivery/pipelines/bifrost-deliver-platform/runs'))
    assert.equal(calls[1].method, 'POST')
  })

  it('on plus 201 returns the approval id and never calls the original route', async () => {
    const created = (await platformPost('/api/v1/gitops/apps/demo/sync')) as {
      id: string
      note: string
      approve_hint: string
    }
    assert.equal(created.id, 'apr-test')
    assert.equal(created.note, OWNER_WAIT_NOTE)
    assert.match(created.approve_hint, /#approvals\?id=apr-test/)
    assert.equal(calls.length, 1)
    assert.ok(calls[0].url.endsWith('/api/v1/approvals'))
    assert.equal(calls[0].url.includes('/sync'), false)
    assert.equal((calls[0].body as { action: string }).action, 'gitops_sync_app')
    assert.equal(calls[0].session, 'local_test_session')

    calls.length = 0
    approvalBody = { id: 'apr-schedule', status: 'pending', action: 'update_data_clone_schedule' }
    const scheduled = (await platformPut('/api/v1/cluster/data-clone/schedule', { enabled: true })) as { id: string }
    assert.equal(scheduled.id, 'apr-schedule')
    assert.equal(calls.length, 1)
    assert.equal((calls[0].body as { action: string }).action, 'update_data_clone_schedule')
    assert.equal(calls[0].url.includes('/data-clone/schedule'), false)
  })

  it('on plus 403 returns the refusal and does not call the original route', async () => {
    approvalStatus = 403
    approvalBody = { error: 'forbidden', action: 'scale_deployment', tier: 'X' }
    const refused = (await platformPost('/api/v1/cluster/workloads/scale', {
      namespace: 'bifrost-prod',
      kind: 'Deployment',
      name: 'daemon',
      replicas: 1,
    })) as { error: string; tier: string }
    assert.equal(refused.error, 'forbidden')
    assert.equal(refused.tier, 'X')
    assert.equal(calls.length, 1)
    assert.ok(calls[0].url.endsWith('/api/v1/approvals'))
    assert.equal(calls[0].url.includes('/scale'), false)
  })

  it('an unmapped write is refused when on and does not call fetch', async () => {
    const refused = (await platformPost('/api/v1/checklist/signals', { signals: [] })) as {
      error: string
      hint: string
    }
    assert.equal(refused.error, 'unmapped write')
    assert.equal(JSON.stringify(refused).includes('unlisted_write'), false)
    assert.equal(calls.length, 0)
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

  it('delete follows call-directly back to the original route', async () => {
    approvalStatus = 400
    approvalBody = { error: 'call directly', action: 'delete_pipeline_run', tier: 'B' }
    const removed = (await platformDelete('/api/v1/delivery/runs/run-1')) as { status?: string }
    assert.equal(removed.status, 'executed')
    assert.equal(calls.length, 2)
    assert.ok(calls[0].url.endsWith('/api/v1/approvals'))
    assert.equal(calls[1].method, 'DELETE')
    assert.ok(calls[1].url.includes('/delivery/runs/run-1'))
  })

  it('approve_request posts channel chat and is not a full-server tool', async () => {
    const mocked = globalThis.fetch
    globalThis.fetch = (async (url: string | URL | Request, init?: RequestInit) => {
      if ((init?.method ?? 'GET') === 'GET' && String(url).endsWith('/api/v1/approvals/9')) {
        calls.push({ url: String(url), method: 'GET', body: undefined })
        const rec = {
          id: 'apr-9',
          number: 9,
          tier: 'C',
          action: 'gitops_sync_app',
          status: 'pending',
          approval_line: '#9 · tier C · gitops_sync_app',
          params_hash: 'hashhashhash',
        }
        return new Response(JSON.stringify(rec), { status: 200 })
      }
      return mocked(url, init)
    }) as typeof fetch
    await approveRequest('#9', '#9 · tier C · gitops_sync_app')
    assert.equal(calls.length, 2)
    assert.equal(calls[0].method, 'GET')
    assert.ok(calls[1].url.endsWith('/api/v1/approvals/apr-9/approve'))
    assert.deepEqual(calls[1].body, {
      channel: 'chat',
      approval_line: '#9 · tier C · gitops_sync_app',
      params_hash: 'hashhashhash',
    })

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
