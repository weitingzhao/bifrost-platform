import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { z } from 'zod'
import {
  ISSUE_DATA_NOTE,
  ISSUE_WRITES,
  REQUEST_OWNER_INPUT,
  TAKE_ISSUE_INPUT,
  addToIssue,
  fileIssue,
  getIssueFull,
  issueWrite,
  listIssues,
  mergeIssues,
  relayUserResponse,
} from './issueTools.js'
import type { RuntimeDeps } from './runtimeTools.js'
import { PLATFORM_STDIO_TOOL_NAMES } from './stdioToolNames.js'

type Call = { kind: 'get' | 'send'; path: string; body?: unknown }

function deps(over: { fellBack?: boolean } = {}): { d: RuntimeDeps; calls: Call[] } {
  const calls: Call[] = []
  const d: RuntimeDeps = {
    get: async (path) => {
      calls.push({ kind: 'get', path })
      return { issues: [] }
    },
    getText: async () => ({ media_type: 'text/plain', text: '' }),
    send: async (_m, path, body) => {
      calls.push({ kind: 'send', path, body })
      return { issue_id: 'BI-3', version: 1 }
    },
    upload: async () => ({}),
    session: () => 'local_s1',
    fellBack: () => over.fellBack ?? false,
  }
  return { d, calls }
}

const TOOLS = [
  'file_issue', 'add_to_issue', 'relay_user_response', 'list_issues', 'get_issue_full', 'draft_brief', 'classify_issue', 'put_design',
  'record_review', 'record_close_check', 'request_owner', 'take_issue', 'reply_to_requester', 'close_issue', 'park_issue', 'resume_issue',
  'reopen_issue', 'merge_issues',
]

describe('the crew issue tools (W-31 BI-1 slice 2)', () => {
  it('posts each write to its route, the issue in the path, as this thread', async () => {
    const { d, calls } = deps()
    await fileIssue({ product: 'screening', kind: 'request', on_behalf_of: 'person:owner', title: 't', idempotency_key: 'k1' }, d)
    await addToIssue({ issue_id: 'BI-3', agent_reading: 'r', idempotency_key: 'k2' }, d)
    await relayUserResponse({ issue_id: 'BI-3', expected_version: 1, in_reply_to: 4, user_words: 'yes', user_verdict: 'confirmed', idempotency_key: 'k3' }, d)
    await mergeIssues({ from: 'BI-4', into: 'BI-3', from_version: 0, into_version: 1, idempotency_key: 'k4' }, d)
    for (const [tool, action] of Object.entries(ISSUE_WRITES)) {
      await issueWrite(tool)({ issue_id: 'BI-3', expected_version: 2, idempotency_key: `k-${tool}` }, d)
      const last = calls[calls.length - 1]
      assert.equal(last.path, `/api/v1/agentrt/issues/BI-3/${action}`, tool)
      assert.deepEqual(last.body, { expected_version: 2, idempotency_key: `k-${tool}` }, tool)
    }
    assert.deepEqual(calls.slice(0, 4).map((c) => c.path), [
      '/api/v1/agentrt/issues',
      '/api/v1/agentrt/issues/BI-3/additions',
      '/api/v1/agentrt/issues/BI-3/responses',
      '/api/v1/agentrt/issues/merge',
    ])
    assert.equal(Object.keys(ISSUE_WRITES).length, 12)
  })

  it('refuses every write without a session, sending nothing', async () => {
    for (const over of [{ fellBack: true }]) {
      const { d, calls } = deps(over)
      const outs = [
        await fileIssue({ idempotency_key: 'k' }, d),
        await addToIssue({ issue_id: 'BI-3', idempotency_key: 'k' }, d),
        await mergeIssues({ idempotency_key: 'k' }, d),
        await issueWrite('request_owner')({ issue_id: 'BI-3', idempotency_key: 'k' }, d),
      ] as Array<{ error?: string }>
      for (const out of outs) assert.ok(out.error)
      assert.equal(calls.length, 0)
    }
  })

  it('reads with the data note: the list by its filters, an issue in full', async () => {
    const { d, calls } = deps()
    const out = (await listIssues({ view: 'active', product: 'screening', limit: 20, waiting_on: undefined }, d)) as { note: string }
    await getIssueFull('BI-7', d)
    assert.equal(out.note, ISSUE_DATA_NOTE)
    assert.deepEqual(calls, [
      { kind: 'get', path: '/api/v1/agentrt/issues?view=active&product=screening&limit=20' },
      { kind: 'get', path: '/api/v1/agentrt/issues/BI-7' },
    ])
  })

  it('request_owner takes no text: a field beside the purpose is dropped, never sent', () => {
    const schema = z.object(REQUEST_OWNER_INPUT)
    const parsed = schema.parse({ issue_id: 'BI-3', expected_version: 2, idempotency_key: 'k', purpose: 'priority', title: 'her words', body: 'more' })
    assert.deepEqual(Object.keys(parsed).sort(), ['expected_version', 'idempotency_key', 'issue_id', 'purpose'])
    assert.equal(schema.safeParse({ issue_id: 'BI-3', expected_version: 2, idempotency_key: 'k', purpose: 'free text' }).success, false)
    assert.equal(schema.safeParse({ issue_id: 'BI-3', expected_version: 2, idempotency_key: 'k', purpose: 'design_decision', decision: 'D1', letters: 5 }).success, false)
    // take_issue's mission names no work id: it is the issue's.
    const take = z.object(TAKE_ISSUE_INPUT)
    const m = take.parse({ issue_id: 'BI-3', expected_version: 2, idempotency_key: 'k', commitment: 'c',
      mission: { title: 't', goal: 'g', success: ['s'], work_id: 'W-1' } })
    assert.equal('work_id' in (m.mission as Record<string, unknown>), false)
  })

  it('every issue tool is registered and listed', () => {
    const index = readFileSync(new URL('./index.ts', import.meta.url), 'utf8')
    const src = readFileSync(new URL('./registerIssues.ts', import.meta.url), 'utf8')
    assert.match(index, /registerIssueTools\(reg\)/)
    const listed = new Set<string>(PLATFORM_STDIO_TOOL_NAMES)
    for (const name of TOOLS) {
      assert.equal(listed.has(name), true, name)
      assert.ok(src.includes(`'${name}'`), name)
    }
  })
})

describe('take_issue mission input', () => {
  const schema = TAKE_ISSUE_INPUT.mission
  const base = { title: 't', goal: 'g', success: ['s'] }
  const ok = (over: Record<string, unknown>) => schema.safeParse({ ...base, ...over }).success

  it('takes a deadline only in a strict subset of what Go\'s time.Time reads (review 200 A1)', () => {
    for (const v of ['2026-10-20T12:00:00Z', '2026-10-20T12:00:00-05:00', '2026-10-20T12:00:00.123456+08:00', '2028-02-29T00:00:00Z']) {
      assert.equal(ok({ deadline_at: v }), true, v)
    }
    for (const v of ['', '2026-10-20', '2026-10-20T12:00:00', '2026-10-20T12:00:00+0500', '2026-10-20 12:00:00Z',
      '2026-02-30T00:00:00Z', '2026-13-01T00:00:00Z', '2026-10-20T24:00:00Z', '2026-10-20T12:60:00Z', '2026-10-20T12:00:60Z',
      '2026-10-20T12:00:00+24:00']) {
      assert.equal(ok({ deadline_at: v }), false, v)
    }
    assert.equal(ok({}), true, 'left out: the default')
  })

  it('takes a budget a Go int holds exactly (review 200 A2)', () => {
    assert.equal(ok({ budget: { max_attempts: 2, max_wall_hours: 1 } }), true)
    assert.equal(ok({ budget: { max_attempts: Number.MAX_SAFE_INTEGER, max_wall_hours: 0 } }), true)
    assert.equal(ok({ budget: { max_attempts: 1e20, max_wall_hours: 1 } }), false)
    assert.equal(ok({ budget: { max_attempts: 1, max_wall_hours: -1 } }), false)
    assert.equal(ok({ budget: { max_attempts: 1.5, max_wall_hours: 1 } }), false)
  })

  it('takes no_merge as one sentence the route takes (TD-383)', () => {
    assert.equal(ok({ no_merge: 'a benchmark replay' }), true)
    assert.equal(ok({ no_merge: '' }), false)
    assert.equal(ok({ no_merge: '  ' }), false)
    assert.equal(ok({ no_merge: '演'.repeat(66) }), true, '198 bytes')
    assert.equal(ok({ no_merge: '演'.repeat(67) }), false, '201 bytes')
  })

  it('takes needs_owner as one sentence the route takes (TD-390)', () => {
    assert.equal(ok({ needs_owner: 'the walk tests his buttons' }), true)
    assert.equal(ok({ needs_owner: '' }), false)
    assert.equal(ok({ needs_owner: '  ' }), false)
    assert.equal(ok({ needs_owner: '演'.repeat(66) }), true, '198 bytes')
    assert.equal(ok({ needs_owner: '演'.repeat(67) }), false, '201 bytes')
  })

  it('needs what the route needs', () => {
    assert.equal(ok({ success: [] }), false)
    assert.equal(ok({ success: [''] }), false)
    assert.equal(ok({ title: '' }), false)
    assert.equal(ok({ repos: [''] }), false)
    assert.equal(ok({ max_action: 'external' }), false)
    assert.equal(ok({ tenant: 'ops' }), false)
  })
})
