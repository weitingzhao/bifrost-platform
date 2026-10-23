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
import type { WritesMode } from './writeGate.js'
import { PLATFORM_STDIO_TOOL_NAMES } from './stdioToolNames.js'

type Call = { kind: 'get' | 'send'; path: string; body?: unknown }

function deps(over: { fellBack?: boolean; mode?: WritesMode } = {}): { d: RuntimeDeps; calls: Call[] } {
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
    mode: () => over.mode ?? 'on',
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

  it('refuses every write without a session or with writes off, sending nothing', async () => {
    for (const over of [{ fellBack: true }, { mode: 'off' as WritesMode }]) {
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
