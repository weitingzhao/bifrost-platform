import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { afterEach, beforeEach, describe, it } from 'node:test'
import { APPROVAL_REQUIRED_ERROR, NO_REQUEST_CREATED_NOTE, platformPost, resetClientCacheForTests } from './platformClient.js'
import { PLATFORM_STDIO_TOOL_NAMES } from './stdioToolNames.js'

// Origin: 2026-11-15. The policy's apply clause was signed that morning
// (rp-20261115-1339). From then on request_action for a covered plan answered
// 400 "call directly", and no tool reached the apply route: the first covered
// plan (pipeline-land.yaml at infra 002bb598) could not be applied by anyone.
// A plan the policy did not cover would have become a card reading
// "mcp:apply_manifest".
describe('apply_manifest is a tool, judged by the server and not by an approvals consult', () => {
  const APPLY = '/api/v1/actuation/manifests/apply'
  const originalFetch = globalThis.fetch
  const envKeys = ['PLATFORM_API_URL', 'PLATFORM_OPERATOR_TOKEN', 'PLATFORM_TOKEN_ENV_KEY', 'CLAUDE_CODE_HOST_SESSION_ID'] as const
  const saved: Record<string, string | undefined> = {}
  let calls: Array<{ url: string; method: string; body?: unknown }>
  let status: number
  let reply: string

  beforeEach(() => {
    for (const key of envKeys) saved[key] = process.env[key]
    process.env.PLATFORM_API_URL = 'http://192.168.10.100:30876'
    process.env.PLATFORM_OPERATOR_TOKEN = 'test-operator-not-a-real-token'
    delete process.env.PLATFORM_TOKEN_ENV_KEY
    process.env.CLAUDE_CODE_HOST_SESSION_ID = 'local_test_session'
    resetClientCacheForTests()
    calls = []
    status = 202
    reply = JSON.stringify({ apply_run: 'apply-002bb598-1', tier: 'C' })
    globalThis.fetch = (async (url: string | URL | Request, init?: RequestInit) => {
      const target = String(url)
      calls.push({ url: target, method: init?.method ?? 'GET', body: init?.body == null ? undefined : JSON.parse(String(init.body)) })
      if (target.endsWith('/api/v1/approvals')) {
        return new Response(JSON.stringify({ id: 'apr-test', status: 'pending', action: 'apply_manifest' }), { status: 201 })
      }
      return new Response(reply, { status })
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

  it('the tool is registered and listed', () => {
    const src = readFileSync(new URL('./index.ts', import.meta.url), 'utf8')
    assert.ok(src.includes("reg(\n  'apply_manifest',"), 'index.ts must register apply_manifest')
    assert.ok(new Set<string>(PLATFORM_STDIO_TOOL_NAMES).has('apply_manifest'))
  })

  it('a covered plan goes to the apply route once; /api/v1/approvals is never called', async () => {
    const applied = (await platformPost(APPLY, { plan_id: 'plan-002bb598-1' })) as { apply_run: string }
    assert.equal(applied.apply_run, 'apply-002bb598-1')
    assert.equal(calls.length, 1)
    assert.ok(calls[0].url.endsWith(APPLY))
    assert.deepEqual(calls[0].body, { plan_id: 'plan-002bb598-1' })
  })

  it('a plan the policy does not cover returns its reasons and creates no request', async () => {
    const reasons = ['Task/bifrost-ci-platform-reuse was last applied by kubectl-client-side-apply, not bifrost-applier']
    status = 403
    reply = JSON.stringify({ error: 'approval required', action: 'apply_manifest', release_policy: reasons })
    const refused = (await platformPost(APPLY, { plan_id: 'plan-002bb598-2' })) as Record<string, unknown>
    assert.deepEqual(refused, {
      started: false,
      error: APPROVAL_REQUIRED_ERROR,
      action: 'apply_manifest',
      release_policy: reasons,
      note: NO_REQUEST_CREATED_NOTE,
    })
    assert.equal(calls.filter((c) => c.url.endsWith('/api/v1/approvals')).length, 0)
  })
})
