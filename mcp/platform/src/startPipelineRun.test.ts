import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { afterEach, beforeEach, describe, it } from 'node:test'
import {
  APPROVAL_REQUIRED_ERROR,
  NO_REQUEST_CREATED_NOTE,
  platformPost,
  resetClientCacheForTests,
} from './platformClient.js'
import { OWNER_WAIT_NOTE, planWrite } from './writeGate.js'

describe('start_pipeline_run who', () => {
  it('tool schema exposes who in index.ts', () => {
    const src = readFileSync(new URL('./index.ts', import.meta.url), 'utf8')
    const block = src.slice(src.indexOf("'start_pipeline_run'"))
    assert.ok(block.includes('who: z.string()'), 'start_pipeline_run must accept who')
    assert.ok(block.includes('params: z.record(z.string(), z.string())'), 'start_pipeline_run must accept params')
  })

  it('write gate forwards who in approval params', () => {
    const plan = planWrite(
      'POST',
      '/api/v1/delivery/pipelines/bifrost-deliver-platform/runs',
      { revision: 'main', who: 'ada@host' },
    )
    assert.equal(plan.kind, 'consult')
    if (plan.kind !== 'consult') return
    assert.equal(plan.action, 'start_pipeline_run')
    assert.equal(plan.params.name, 'bifrost-deliver-platform')
    assert.equal(plan.params.who, 'ada@host')
  })
})

// The server guard judges a run on its own route. The approvals consult
// created a request in the caller's name whenever the signed release policy
// did not cover a tier C run, with reason "mcp:start_pipeline_run" and no
// word of why the policy refused.
describe('start_pipeline_run is judged by the server, not by an approvals consult', () => {
  const RUNS = '/api/v1/delivery/pipelines/bifrost-deliver-platform-prod/runs'
  const originalFetch = globalThis.fetch
  const envKeys = ['PLATFORM_API_URL', 'PLATFORM_OPERATOR_TOKEN', 'PLATFORM_TOKEN_ENV_KEY', 'CLAUDE_CODE_HOST_SESSION_ID'] as const
  const saved: Record<string, string | undefined> = {}
  let calls: Array<{ url: string; method: string; body?: unknown }>
  let runStatus: number
  let runBody: string

  beforeEach(() => {
    for (const key of envKeys) saved[key] = process.env[key]
    process.env.PLATFORM_API_URL = 'http://192.168.10.100:30876'
    process.env.PLATFORM_OPERATOR_TOKEN = 'test-operator-not-a-real-token'
    delete process.env.PLATFORM_TOKEN_ENV_KEY
    process.env.CLAUDE_CODE_HOST_SESSION_ID = 'local_test_session'
    resetClientCacheForTests()
    calls = []
    runStatus = 200
    runBody = JSON.stringify({ ok: true, action: 'delivery.pipeline.run', run: { name: 'bifrost-deliver-platform-prod-1' } })
    globalThis.fetch = (async (url: string | URL | Request, init?: RequestInit) => {
      const target = String(url)
      calls.push({
        url: target,
        method: init?.method ?? 'GET',
        body: init?.body == null ? undefined : JSON.parse(String(init.body)),
      })
      if (target.endsWith('/api/v1/approvals')) {
        return new Response(JSON.stringify({ id: 'apr-test', status: 'pending', action: 'gitops_sync_app' }), { status: 201 })
      }
      if (target.endsWith('/runs')) return new Response(runBody, { status: runStatus })
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

  const approvalCalls = () => calls.filter((c) => c.url.endsWith('/api/v1/approvals'))

  it('a started run is returned as before, and /api/v1/approvals is never called', async () => {
    const body = { revision: 'a'.repeat(40), who: 'ada@host' }
    const started = (await platformPost(RUNS, body)) as { ok: boolean; run: { name: string } }
    assert.equal(started.ok, true)
    assert.equal(started.run.name, 'bifrost-deliver-platform-prod-1')
    assert.equal(approvalCalls().length, 0)
    assert.equal(calls.length, 1)
    assert.ok(calls[0].url.endsWith(RUNS))
    assert.equal(calls[0].method, 'POST')
    assert.deepEqual(calls[0].body, body)
  })

  it('403 approval required returns the release policy reasons as they came and creates no request', async () => {
    const reasons = [
      'ci: no Succeeded bifrost-ci-platform run for 0123456789abcdef0123456789abcdef01234567',
      'stg: bifrost-platform-stg runs another commit',
    ]
    runStatus = 403
    runBody = JSON.stringify({ error: 'approval required', action: 'start_pipeline_run', release_policy: reasons })
    const refused = (await platformPost(RUNS, { revision: 'a'.repeat(40) })) as Record<string, unknown>
    assert.deepEqual(refused, {
      started: false,
      error: APPROVAL_REQUIRED_ERROR,
      action: 'start_pipeline_run',
      release_policy: reasons,
      note: NO_REQUEST_CREATED_NOTE,
    })
    assert.equal(approvalCalls().length, 0)
    assert.equal(calls.length, 1)
  })

  it('a refusal without release_policy invents no reasons', async () => {
    runStatus = 403
    runBody = JSON.stringify({ error: 'approval required', action: 'start_pipeline_run' })
    const refused = (await platformPost(RUNS, { revision: 'main' })) as Record<string, unknown>
    assert.equal(refused.started, false)
    assert.equal(refused.error, APPROVAL_REQUIRED_ERROR)
    assert.equal('release_policy' in refused, false)
    assert.equal(refused.note, NO_REQUEST_CREATED_NOTE)
    assert.equal(approvalCalls().length, 0)
  })

  it('any other failure is thrown as before, a plain 403 included', async () => {
    for (const [status, text] of [
      [403, JSON.stringify({ error: 'forbidden' })],
      [403, 'not json'],
      [502, JSON.stringify({ ok: false, message: 'release window is held by someone else' })],
    ] as const) {
      runStatus = status
      runBody = text
      await assert.rejects(
        () => platformPost(RUNS, { revision: 'main' }),
        (err: Error) => err.message === `POST ${RUNS}: HTTP ${status} ${text}`,
      )
    }
    assert.equal(approvalCalls().length, 0)
  })

  it('another mapped write still consults the approvals API first', async () => {
    const created = (await platformPost('/api/v1/gitops/apps/demo/sync')) as { id: string; note: string }
    assert.equal(created.id, 'apr-test')
    assert.equal(created.note, OWNER_WAIT_NOTE)
    assert.equal(calls.length, 1)
    assert.ok(calls[0].url.endsWith('/api/v1/approvals'))
    assert.equal((calls[0].body as { action: string; reason: string }).action, 'gitops_sync_app')
  })

  it('the tool description says what a tier C run needs and that no request is created', () => {
    const src = readFileSync(new URL('./index.ts', import.meta.url), 'utf8')
    const from = src.indexOf("'start_pipeline_run'")
    const block = src.slice(from, src.indexOf("'check_release_policy'", from))
    assert.match(block, /never creates an approval request/)
    assert.match(block, /signed release policy/)
    assert.match(block, /release_policy/)
  })
})
