import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { describe, it } from 'node:test'
import { z } from 'zod'
import {
  PROPOSAL_KEY_RULE,
  PROPOSE_MISSION_INPUT,
  RUNTIME_DATA_NOTE,
  RUNTIME_SESSION_REFUSAL,
  RUNTIME_WRITES_OFF,
  STOP_KEY_RULE,
  getArtifact,
  getMission,
  getPlan,
  getTask,
  listTasks,
  proposeMission,
  proposePlanChange,
  proposeTasks,
  putArtifact,
  requestStop,
  whoami,
  sendMessage,
  waitForMessages,
  ackMessages,
  getAttemptActions,
  MESSAGE_DATA_NOTE,
  type RuntimeDeps,
} from './runtimeTools.js'
import { PLATFORM_STDIO_TOOL_NAMES } from './stdioToolNames.js'
import type { WritesMode } from './writeGate.js'

type Call = { kind: string; path: string; body?: unknown; contentType?: string; headers?: Record<string, string> }

function deps(over: { fellBack?: boolean; mode?: WritesMode; session?: string } = {}): { d: RuntimeDeps; calls: Call[] } {
  const calls: Call[] = []
  const d: RuntimeDeps = {
    get: async (path) => {
      calls.push({ kind: 'get', path })
      return { ok: true }
    },
    getText: async (path) => {
      calls.push({ kind: 'getText', path })
      return { media_type: 'text/markdown', text: '# spec\n' }
    },
    send: async (_method, path, body) => {
      calls.push({ kind: 'send', path, body })
      return { sent: true }
    },
    upload: async (path, body, contentType, headers) => {
      calls.push({ kind: 'upload', path, body, contentType, headers })
      return { created: true }
    },
    session: () => over.session ?? 'local_s1',
    fellBack: () => over.fellBack ?? false,
    mode: () => over.mode ?? 'on',
  }
  return { d, calls }
}

const task = {
  ref: 'a',
  plan_node_id: 'R2:1-1',
  role: 'implement',
  task_class: 'go',
  title: 'a',
  spec_artifact_id: 'sha256:' + 'ab'.repeat(32),
  effect_class: 'branch',
}

describe('agent runtime reads', () => {
  it('reads the routes it names and marks agent text as data', async () => {
    const { d, calls } = deps()
    await whoami(d)
    assert.deepEqual(await getPlan({}, d), { ok: true, note: RUNTIME_DATA_NOTE })
    await getPlan({ root: 'R2' }, d)
    await getPlan({ node: 'R2:1-1', root: 'R2' }, d)
    await getMission('M-1', d)
    await listTasks({}, d)
    await listTasks({ mission: 'M-1', state: 'ready' }, d)
    await getTask('t_x', d)
    assert.deepEqual(
      calls.map((c) => c.path),
      [
        '/api/v1/agentrt/whoami',
        '/api/v1/agentrt/plan',
        '/api/v1/agentrt/plan?root=R2',
        '/api/v1/agentrt/plan/R2:1-1',
        '/api/v1/agentrt/missions/M-1',
        '/api/v1/agentrt/tasks',
        '/api/v1/agentrt/tasks?mission=M-1&state=ready',
        '/api/v1/agentrt/tasks/t_x',
      ],
    )
    const art = await getArtifact('sha256:' + 'cd'.repeat(32), d)
    assert.deepEqual(art, { artifact_id: 'sha256:' + 'cd'.repeat(32), media_type: 'text/markdown', content: '# spec\n', note: RUNTIME_DATA_NOTE })
    // The id as the platform stores it, ':' unescaped (10-15: %3A named no record).
    assert.equal(calls.at(-1)?.path, '/api/v1/agentrt/artifacts/sha256:' + 'cd'.repeat(32))
  })
})

describe('agent runtime writes', () => {
  it('propose_mission sends the mission with the caller\'s key, and none without one', async () => {
    const { d, calls } = deps()
    const mission = { title: 'Fix x', goal: 'x works', success: ['go test passes'], repos: ['bifrost-platform'], max_action: 'branch' as const }
    const missing = await proposeMission({ ...mission, idempotency_key: '' }, d)
    assert.deepEqual(missing, { error: 'idempotency_key is required', hint: PROPOSAL_KEY_RULE })
    assert.equal(calls.length, 0)
    await proposeMission({ ...mission, idempotency_key: 'm-1' }, d)
    await proposeMission({ ...mission, budget: { max_attempts: 2, max_wall_hours: 1 }, idempotency_key: 'm-2' }, d)
    assert.equal(calls[0].path, '/api/v1/agentrt/missions')
    assert.deepEqual(calls[0].body, { ...mission, idempotency_key: 'm-1' })
    assert.deepEqual(calls[1].body, { ...mission, budget: { max_attempts: 2, max_wall_hours: 1 }, idempotency_key: 'm-2' })
  })

  it('propose_tasks sends the batch with the caller\'s key, and none without one', async () => {
    const { d, calls } = deps()
    const missing = await proposeTasks({ mission_id: 'M-1', tasks: [task], idempotency_key: '' }, d)
    assert.deepEqual(missing, { error: 'idempotency_key is required', hint: PROPOSAL_KEY_RULE })
    assert.equal(calls.length, 0)
    await proposeTasks({ mission_id: 'M-1', tasks: [task], idempotency_key: 'p-1' }, d)
    await proposeTasks({ mission_id: 'M-1', tasks: [task], idempotency_key: 'p-2' }, d)
    assert.equal(calls[0].path, '/api/v1/agentrt/missions/M-1/tasks')
    assert.deepEqual(calls[0].body, { idempotency_key: 'p-1', tasks: [task] })
    assert.equal((calls[1].body as { idempotency_key: string }).idempotency_key, 'p-2', 'the same tasks again are another proposal')
  })

  it('propose_plan_change sends the changes with the caller\'s key, and none without one', async () => {
    const { d, calls } = deps()
    const changes = [{ op: 'add_node' as const, parent: 'R2:2.1', kind: 'step' as const, step_kind: 'agent', title: 'next' }]
    const missing = await proposePlanChange({ root: 'R2', reason: 'r', changes, idempotency_key: '' }, d)
    assert.deepEqual(missing, { error: 'idempotency_key is required', hint: PROPOSAL_KEY_RULE })
    assert.equal(calls.length, 0)
    await proposePlanChange({ root: 'R2', reason: 'r', changes, idempotency_key: 'c-1' }, d)
    assert.equal(calls[0].path, '/api/v1/agentrt/plan/proposals')
    assert.deepEqual(calls[0].body, { idempotency_key: 'c-1', root: 'R2', reason: 'r', changes })
  })

  it('put_artifact addresses the content by its sha256 and names its kind', async () => {
    const { d, calls } = deps()
    await putArtifact({ kind: 'task_spec', content: '# implement x\n' }, d)
    const sum = createHash('sha256').update('# implement x\n', 'utf8').digest('hex')
    assert.equal(calls[0].path, `/api/v1/agentrt/artifacts/sha256:${sum}`)
    assert.equal(calls[0].body, '# implement x\n')
    assert.equal(calls[0].contentType, 'text/markdown')
    assert.deepEqual(calls[0].headers, { 'X-Artifact-Kind': 'task_spec' })
    await putArtifact({ kind: 'brief', content: '{}', media_type: 'application/json' }, d)
    assert.equal(calls[1].contentType, 'application/json')
  })

  it('request_stop sends the control with the caller\'s key, and none without one', async () => {
    const { d, calls } = deps()
    const missing = await requestStop({ scope: 'global', kind: 'pause', reason: 'look first', idempotency_key: '' }, d)
    assert.deepEqual(missing, { error: 'idempotency_key is required', hint: STOP_KEY_RULE })
    assert.equal(calls.length, 0)
    await requestStop({ scope: 'task', scope_ref: 't_x', kind: 'stop', reason: 'wrong', idempotency_key: 'stop-1' }, d)
    await requestStop({ scope: 'task', scope_ref: 't_x', kind: 'stop', reason: 'wrong', idempotency_key: 'stop-2' }, d)
    assert.equal(calls[0].path, '/api/v1/agentrt/controls')
    assert.deepEqual(calls[0].body, { scope: 'task', scope_ref: 't_x', kind: 'stop', reason: 'wrong', idempotency_key: 'stop-1' })
    assert.equal((calls[1].body as Record<string, string>).idempotency_key, 'stop-2', 'the same stop again is another stop')
  })

  for (const [name, over, hint] of [
    ['a fallen-back session', { fellBack: true }, RUNTIME_SESSION_REFUSAL],
    ['MCP_WRITES=off', { mode: 'off' as WritesMode }, RUNTIME_WRITES_OFF],
  ] as const) {
    it(`sends no write under ${name}`, async () => {
      const { d, calls } = deps(over)
      for (const got of [
        await proposeMission({ title: 't', goal: 'g', success: ['s'], idempotency_key: 'k' }, d),
        await proposeTasks({ mission_id: 'M-1', tasks: [task], idempotency_key: 'k' }, d),
        await proposePlanChange({ root: 'R2', reason: 'r', changes: [{ op: 'retitle', node: 'R2', title: 't' }], idempotency_key: 'k' }, d),
        await putArtifact({ kind: 'task_spec', content: 'x' }, d),
        await requestStop({ scope: 'global', kind: 'stop', reason: 'x', idempotency_key: 'k' }, d),
      ]) {
        assert.equal((got as { hint: string }).hint, hint)
      }
      assert.equal(calls.length, 0)
    })
  }
})

describe('propose_mission input', () => {
  const schema = z.object(PROPOSE_MISSION_INPUT)
  const base = { title: 't', goal: 'g', success: ['s'], idempotency_key: 'k' }
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

  it('needs what the route needs', () => {
    assert.equal(ok({ success: [] }), false)
    assert.equal(ok({ success: [''] }), false)
    assert.equal(ok({ title: '' }), false)
    assert.equal(ok({ idempotency_key: '' }), false)
    assert.equal(ok({ repos: [''] }), false)
    assert.equal(ok({ max_action: 'external' }), false)
    assert.equal(ok({ tenant: 'ops' }), false)
  })
})

describe('agent runtime tools are served', () => {
  it('every runtime tool is registered and listed', () => {
    const index = readFileSync(new URL('./index.ts', import.meta.url), 'utf8')
    const messages = readFileSync(new URL('./registerMessages.ts', import.meta.url), 'utf8')
    // the inbox tools are registered from registerMessages.ts, called with the scoped reg
    assert.match(index, /registerMessageTools\(reg\)/)
    const served = index + messages
    const listed = new Set<string>(PLATFORM_STDIO_TOOL_NAMES)
    for (const name of ['whoami', 'get_plan', 'get_mission', 'list_tasks', 'get_task', 'propose_mission', 'propose_tasks', 'propose_plan_change', 'put_artifact', 'get_artifact', 'request_stop', 'send_message', 'wait_for_messages', 'ack_messages', 'get_attempt_actions']) {
      assert.equal(listed.has(name), true, name)
      assert.match(served, new RegExp(`reg\\(\\s*'${name}'`), name)
    }
    // The schema the tests above check is the one served.
    assert.match(index, /reg\(\s*'propose_mission',\s*`[^`]*`,\s*PROPOSE_MISSION_INPUT,/)
  })
})

describe('messages and actions (W-31 2.3)', () => {
  it('send_message posts as this thread and refuses without a session or with writes off', async () => {
    const { d, calls } = deps()
    await sendMessage({ to: 'seat:chief_of_staff', kind: 'ask', body: 'how is it going?' }, d)
    assert.deepEqual(calls, [{ kind: 'send', path: '/api/v1/agentrt/messages', body: { to: 'seat:chief_of_staff', kind: 'ask', body: 'how is it going?' } }])
    for (const over of [{ fellBack: true }, { mode: 'off' as WritesMode }]) {
      const { d: d2, calls: c2 } = deps(over)
      const out = (await sendMessage({ to: 'owner', kind: 'note', body: 'x' }, d2)) as { error?: string }
      assert.ok(out.error)
      assert.equal(c2.length, 0)
    }
  })

  it('wait_for_messages polls at most 25 seconds at a time until one arrives or time is up', async () => {
    let t = 0
    const answers = [{ messages: [] }, { messages: [] }, { actor: 'thread:local_s1', messages: [{ message_id: 'm_1', body: 'hi' }] }]
    const paths: string[] = []
    const d: RuntimeDeps = {
      ...deps().d,
      get: async (path) => {
        paths.push(path)
        t += 25_000
        return answers.shift()
      },
    }
    const out = (await waitForMessages({ timeout_seconds: 120 }, d, () => t)) as { messages: unknown[]; note: string }
    assert.equal(out.messages.length, 1)
    assert.equal(out.note, MESSAGE_DATA_NOTE)
    assert.deepEqual(paths, [
      '/api/v1/agentrt/messages?wait=25&limit=100',
      '/api/v1/agentrt/messages?wait=25&limit=100',
      '/api/v1/agentrt/messages?wait=25&limit=100',
    ])
    let t2 = 0
    const empty: RuntimeDeps = { ...deps().d, get: async () => { t2 += 25_000; return { messages: [] } } }
    const none = (await waitForMessages({ timeout_seconds: 30 }, empty, () => t2)) as { messages: unknown[] }
    assert.equal(none.messages.length, 0)
    const { d: once, calls } = deps()
    await waitForMessages({ timeout_seconds: 0, limit: 5 }, once, () => 0)
    assert.deepEqual(calls, [{ kind: 'get', path: '/api/v1/agentrt/messages?wait=0&limit=5' }])
  })

  it('ack_messages sends the ids; get_attempt_actions reads the stream, after a cursor or only the newest', async () => {
    const { d, calls } = deps()
    await ackMessages({ message_ids: ['m_1', 'm_2'] }, d)
    await getAttemptActions({ attempt_id: 'a_01M56J5QPAAQQP53KV4MQYCW5Q', after: '1760-0' }, d)
    await getAttemptActions({ attempt_id: 'a_01M56J5QPAAQQP53KV4MQYCW5Q', last: true }, d)
    assert.deepEqual(calls, [
      { kind: 'send', path: '/api/v1/agentrt/messages/ack', body: { message_ids: ['m_1', 'm_2'] } },
      { kind: 'get', path: '/api/v1/agentrt/attempts/a_01M56J5QPAAQQP53KV4MQYCW5Q/actions?after=1760-0' },
      { kind: 'get', path: '/api/v1/agentrt/attempts/a_01M56J5QPAAQQP53KV4MQYCW5Q/actions?last=1' },
    ])
    const { d: off, calls: none } = deps({ mode: 'off' })
    assert.ok(((await ackMessages({ message_ids: ['m_1'] }, off)) as { error?: string }).error)
    assert.equal(none.length, 0)
  })
})

describe('wait_for_messages and a server that answers empty at once (review 264 B4)', () => {
  it('polls at most about once a second', async () => {
    let t = 0
    let calls = 0
    const d: RuntimeDeps = { ...deps().d, get: async () => { calls++; return { messages: [] } } }
    const out = (await waitForMessages({ timeout_seconds: 10 }, d, () => t, async (ms) => { t += ms })) as { messages: unknown[] }
    assert.equal(out.messages.length, 0)
    assert.ok(calls <= 11, `${calls} polls in 10 seconds`)
  })
})
