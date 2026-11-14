import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { mkdirSync, mkdtempSync, readFileSync, symlinkSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { execFileSync, spawnSync } from 'node:child_process'
import { join } from 'node:path'
import { describe, it } from 'node:test'
import {
  PROPOSAL_KEY_RULE,
  RUNTIME_DATA_NOTE,
  RUNTIME_SESSION_REFUSAL,
  STOP_KEY_RULE,
  getArtifact,
  searchArtifacts,
  getMission,
  getPlan,
  getPlanView, registerExternalWork, recordExternalWorkEvent, COMMIT_SHA,
  getTask,
  listTasks,
  proposePlanChange,
  proposePlanImport,
  postPlanNote,
  NOTE_KEY_RULE,
  defaultRequestFiles,
  readRequestFile,
  nodeRequestFs,
  PLAN_IMPORT_MAX_BYTES,
  PLAN_IMPORT_FORMAT,
  RECORDER_IMPORT_FORMAT,
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

type Call = { kind: string; path: string; body?: unknown; contentType?: string; headers?: Record<string, string> }

function deps(over: { fellBack?: boolean; session?: string } = {}): { d: RuntimeDeps; calls: Call[] } {
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
  }
  return { d, calls }
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
    // search_artifacts (W-31 2.3): every filter in the query, escaped; none, the bare route.
    assert.deepEqual(await searchArtifacts({}, d), { ok: true, note: RUNTIME_DATA_NOTE })
    assert.equal(calls.at(-1)?.path, '/api/v1/agentrt/artifacts')
    await searchArtifacts({ kind: ['handover', 'report'], task: 't_x', mission: 'M-1', author: 'thread:s 1', since: '2026-10-21T12:00:00+02:00', limit: 20, before: '1792600000123456.' + 'ab'.repeat(32) }, d)
    assert.equal(calls.at(-1)?.path, '/api/v1/agentrt/artifacts?kind=handover,report&task=t_x&mission=M-1&author=thread%3As%201&since=2026-10-21T12%3A00%3A00%2B02%3A00&before=1792600000123456.' + 'ab'.repeat(32) + '&limit=20')
  })
})

describe('agent runtime writes', () => {
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

  it('propose_plan_change passes drop_step and set_marks through as they are', async () => {
    const { d, calls } = deps()
    const changes = [
      { op: 'drop_step' as const, node: 'R2:2.1-6' },
      { op: 'set_marks' as const, node: 'R2:2.1-5', owner_step: true, effect_step: false },
    ]
    await proposePlanChange({ root: 'R2', reason: 'r', changes, idempotency_key: 'c-2' }, d)
    assert.deepEqual(calls[0].body, { idempotency_key: 'c-2', root: 'R2', reason: 'r', changes })
  })

  it('post_plan_note posts to the row with the caller\'s key, and nothing without one, a session or writes on', async () => {
    const { d, calls } = deps()
    const missing = await postPlanNote({ plan_node_id: 'ARCH:state-db', text: 'both environments', idempotency_key: '' }, d)
    assert.deepEqual(missing, { error: 'idempotency_key is required', hint: NOTE_KEY_RULE })
    assert.equal(calls.length, 0)
    await postPlanNote({ plan_node_id: 'ARCH:state-db', text: 'both environments', idempotency_key: 'n-1' }, d)
    // The row's ':' as the platform stores it, the rest escaped.
    await postPlanNote({ plan_node_id: 'ARCH:a/b', text: 'x', idempotency_key: 'n-2' }, d)
    assert.deepEqual(calls, [
      { kind: 'send', path: '/api/v1/agentrt/plan/ARCH:state-db/notes', body: { idempotency_key: 'n-1', text: 'both environments' } },
      { kind: 'send', path: '/api/v1/agentrt/plan/ARCH:a%2Fb/notes', body: { idempotency_key: 'n-2', text: 'x' } },
    ])
    for (const over of [{ fellBack: true }]) {
      const { d: d2, calls: c2 } = deps(over)
      const out = (await postPlanNote({ plan_node_id: 'ARCH:state-db', text: 'x', idempotency_key: 'n-3' }, d2)) as { error?: string }
      assert.ok(out.error)
      assert.equal(c2.length, 0)
    }
  })

  it('the import formats are the platform\'s (planimport.Format, recorderimport.Format)', () => {
    const go = (file: string) => readFileSync(new URL(`../../../api/internal/agentrt/${file}`, import.meta.url), 'utf8')
    assert.match(go('planimport/request.go'), new RegExp(`const Format = "${PLAN_IMPORT_FORMAT.replace(/[./]/g, '\\$&')}"`))
    assert.match(go('recorderimport/recorder.go'), new RegExp(`const Format = "${RECORDER_IMPORT_FORMAT.replace(/[./]/g, '\\$&')}"`))
  })

  it('propose_plan_import reads a request only from its folder and sends it with the caller\'s key', async () => {
    const base = mkdtempSync(join(tmpdir(), 'plan-import-'))
    const dir = join(base, 'plan-import')
    mkdirSync(dir)
    const request = { format: PLAN_IMPORT_FORMAT, root: { id: 'R2' }, nodes: [] }
    writeFileSync(join(dir, 'r2.json'), JSON.stringify(request))
    // A recorder log's request goes to the same route (W-31 2.3).
    const recorder = { format: RECORDER_IMPORT_FORMAT, source: { work_id: 'td-336' }, lines: [] }
    writeFileSync(join(dir, 'recorder-td-336.json'), JSON.stringify(recorder))
    writeFileSync(join(dir, 'formatless.json'), JSON.stringify({ root: { id: 'R2' } }))
    writeFileSync(join(dir, 'other.json'), JSON.stringify({ format: 'something else' }))
    writeFileSync(join(dir, 'bad.json'), '{not json')
    writeFileSync(join(base, 'outside.json'), JSON.stringify(request))
    symlinkSync(join(base, 'outside.json'), join(dir, 'link.json'))
    const saved = process.env.AGENTRT_PLAN_IMPORT_DIR
    process.env.AGENTRT_PLAN_IMPORT_DIR = dir
    try {
      const files = defaultRequestFiles()
      const { d, calls } = deps()
      const refusals: Array<[string, RegExp]> = [
        [join(base, 'outside.json'), /the only folder request files are read from/],
        [join(dir, 'link.json'), /cannot read .*link\.json/],
        [join(dir, 'other.json'), /is not an import request \(agentrt\.plan_import\/v1 or agentrt\.recorder_import\/v1\)/],
        [join(dir, 'formatless.json'), /is not an import request/],
        [join(dir, 'bad.json'), /is not JSON/],
        [join(dir, 'missing.json'), /cannot read/],
        ['r2.json', /an absolute path/],
        [join(dir, 'r2.txt'), /an absolute path to a \.json file/],
      ]
      for (const [file, want] of refusals) {
        const out = (await proposePlanImport({ request_file: file, reason: 'r', idempotency_key: 'i-1' }, d, files)) as { error: string }
        assert.match(out.error, want, file)
      }
      assert.equal(calls.length, 0, 'nothing is sent for a refused file')
      const missing = await proposePlanImport({ request_file: join(dir, 'r2.json'), reason: 'r', idempotency_key: '' }, d, files)
      assert.deepEqual(missing, { error: 'idempotency_key is required', hint: PROPOSAL_KEY_RULE })
      await proposePlanImport({ request_file: join(dir, 'r2.json'), reason: 'round 2', idempotency_key: 'i-1' }, d, files)
      assert.equal(calls[0].path, '/api/v1/agentrt/plan/imports')
      assert.deepEqual(calls[0].body, { idempotency_key: 'i-1', reason: 'round 2', request })
      // The whole body is bounded, its key and reason with it, in UTF-8 bytes (review 491 B2).
      const before = calls.length
      const long = (await proposePlanImport({ request_file: join(dir, 'r2.json'), reason: 'x'.repeat(1 << 20), idempotency_key: 'i-big' }, d, files)) as { error: string }
      assert.match(long.error, /POST \/plan\/imports takes at most 1048576/)
      const wide = (await proposePlanImport({ request_file: join(dir, 'r2.json'), reason: '已'.repeat(350_000), idempotency_key: 'i-wide' }, d, files)) as { error: string }
      assert.match(wide.error, /bytes; POST \/plan\/imports/, '350 000 characters of three bytes each are over 1 MiB')
      assert.equal(calls.length, before, 'nothing is sent for a body over the bound')
      await proposePlanImport({ request_file: join(dir, 'recorder-td-336.json'), reason: 'retire the recorder', idempotency_key: 'i-3' }, d, files)
      assert.equal(calls[1].path, '/api/v1/agentrt/plan/imports')
      assert.deepEqual(calls[1].body, { idempotency_key: 'i-3', reason: 'retire the recorder', request: recorder })
      const off = deps({ fellBack: true })
      await proposePlanImport({ request_file: join(dir, 'r2.json'), reason: 'r', idempotency_key: 'i-2' }, off.d, files)
      assert.equal(off.calls.length, 0, 'no session sends nothing')
    } finally {
      if (saved === undefined) delete process.env.AGENTRT_PLAN_IMPORT_DIR
      else process.env.AGENTRT_PLAN_IMPORT_DIR = saved
    }
  })

  it('reads a request from the descriptor it checked: a file swapped, linked out or grown is refused (review 313 A1)', () => {
    const base = mkdtempSync(join(tmpdir(), 'plan-import-race-'))
    const dir = join(base, 'plan-import')
    mkdirSync(dir)
    writeFileSync(join(dir, 'r2.json'), '{"format":"agentrt.plan_import/v1"}')
    writeFileSync(join(base, 'outside.json'), '{"format":"agentrt.plan_import/v1","secret":1}')
    const real = nodeRequestFs
    assert.deepEqual(readRequestFile(join(dir, 'r2.json'), dir, real), { text: '{"format":"agentrt.plan_import/v1"}' })
    // The path is swapped for a link out after it was opened: the path now
    // names another file than the one opened.
    const swapped = { ...real, stat: (p: string) => (p.endsWith('r2.json') ? real.stat(join(base, 'outside.json')) : real.stat(p)) }
    assert.match((readRequestFile(join(dir, 'r2.json'), dir, swapped) as { error: string }).error, /the only folder/)
    // The folder's path resolves elsewhere once the file is open.
    const moved = { ...real, realpath: (p: string) => (p.endsWith('r2.json') ? join(base, 'outside.json') : real.realpath(p)) }
    assert.match((readRequestFile(join(dir, 'r2.json'), dir, moved) as { error: string }).error, /the only folder/)
    // The file grows while it is read.
    let calls = 0
    const growing = { ...real, read: (_fd: number, buf: Buffer) => (calls++ < 2 ? buf.fill(32).length : 0) }
    assert.match((readRequestFile(join(dir, 'r2.json'), dir, growing) as { error: string }).error, /grew past/)
    // A FIFO with no writer is refused at once, not waited on (review 314 B1),
    // read in a child process with a deadline: a read that waits fails the
    // test instead of hanging it (review 315 B1).
    execFileSync('mkfifo', [join(dir, 'pipe.json')])
    const module = JSON.stringify(new URL('./runtimeTools.ts', import.meta.url).href)
    const child = spawnSync(process.execPath, ['--import', 'tsx', '--input-type=module', '-e',
      `import { readRequestFile } from ${module}; process.stdout.write(JSON.stringify(readRequestFile(${JSON.stringify(join(dir, 'pipe.json'))}, ${JSON.stringify(dir)})))`],
    { timeout: 15000, encoding: 'utf8' })
    assert.equal(child.error, undefined, `the read of a FIFO did not return within 15 s: ${child.error?.message}`)
    assert.match((JSON.parse(child.stdout) as { error: string }).error, /not a regular file/)
    // A descriptor that does not open (a link in the last part) is refused.
    const refuses = { ...real, openNoFollow: () => { throw new Error('ELOOP: too many symbolic links') } }
    assert.match((readRequestFile(join(dir, 'r2.json'), dir, refuses) as { error: string }).error, /cannot read .*ELOOP/)
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
  ] as const) {
    it(`sends no write under ${name}`, async () => {
      const { d, calls } = deps(over)
      for (const got of [
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

describe('agent runtime tools are served', () => {
  it('every runtime tool is registered and listed', () => {
    const index = readFileSync(new URL('./index.ts', import.meta.url), 'utf8')
    const messages = readFileSync(new URL('./registerMessages.ts', import.meta.url), 'utf8')
    const imports = readFileSync(new URL('./registerPlanImport.ts', import.meta.url), 'utf8')
    // the inbox tools are registered from registerMessages.ts, the plan
    // import from registerPlanImport.ts, each called with the scoped reg
    assert.match(index, /registerMessageTools\(reg\)/)
    assert.match(index, /registerPlanImportTools\(reg\)/)
    const served = index + messages + imports
    const listed = new Set<string>(PLATFORM_STDIO_TOOL_NAMES)
    for (const name of ['whoami', 'get_plan', 'get_mission', 'list_tasks', 'get_task', 'propose_plan_change', 'propose_plan_import', 'post_plan_note', 'put_artifact', 'search_artifacts', 'get_artifact', 'request_stop', 'send_message', 'wait_for_messages', 'ack_messages', 'get_attempt_actions']) {
      assert.equal(listed.has(name), true, name)
      assert.match(served, new RegExp(`reg\\(\\s*'${name}'`), name)
    }
  })
})

describe('messages and actions (W-31 2.3)', () => {
  it('send_message posts as this thread and refuses without a session', async () => {
    const { d, calls } = deps()
    await sendMessage({ to: 'seat:chief_of_staff', kind: 'ask', body: 'how is it going?' }, d)
    assert.deepEqual(calls, [{ kind: 'send', path: '/api/v1/agentrt/messages', body: { to: 'seat:chief_of_staff', kind: 'ask', body: 'how is it going?' } }])
    for (const over of [{ fellBack: true }]) {
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
    const { d: off, calls: none } = deps({ fellBack: true })
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

describe('plan view and external work', () => {
  it('reads a root snapshot and forwards bounded writes', async () => {
    const { d, calls } = deps()
    await getPlanView({ root: 'R4' }, d)
    const registration = { idempotency_key: 'run', root: 'R4', plan_node_id: 'R4:4.5', source_run_key: 'run', artifact_id: 'sha256:' + 'a'.repeat(64) }
    await registerExternalWork(registration, d)
    await recordExternalWorkEvent({ external_work_id: 'ew_1', idempotency_key: 'hb', seq: 2, type: 'heartbeat' }, d)
    assert.equal(calls[0].path, '/api/v1/agentrt/plan-view?root=R4')
    assert.deepEqual(calls[1].body, registration)
    assert.equal(calls[2].path, '/api/v1/agentrt/external-work/ew_1/events')
    assert.deepEqual(calls[2].body, { idempotency_key: 'hb', seq: 2, type: 'heartbeat' })
  })
  it('an event without seq sends none, and without a key gets one it reports', async () => {
    const { d, calls } = deps()
    const sha = '0123456789abcdef0123456789abcdef01234567'
    // seq left out: the body has no seq field at all, so the platform assigns the next.
    const kept = await recordExternalWorkEvent({ external_work_id: 'ew_1', idempotency_key: 'hb-7', type: 'heartbeat', commit_shas: [sha] }, d)
    assert.deepEqual(calls[0].body, { idempotency_key: 'hb-7', type: 'heartbeat', commit_shas: [sha] })
    assert.equal('seq' in (calls[0].body as object), false)
    // The caller's own key: the answer is the platform's, nothing added.
    assert.deepEqual(kept, { sent: true })

    // No key: one is generated, sent, and returned so a retry can reuse it.
    const made = (await recordExternalWorkEvent({ external_work_id: 'ew_1', type: 'heartbeat' }, d, () => 'generated-key')) as Record<string, unknown>
    assert.deepEqual(calls[1].body, { idempotency_key: 'generated-key', type: 'heartbeat' })
    assert.deepEqual(made, { sent: true, idempotency_key: 'generated-key' })

    // The default generator is a fresh UUID each call.
    await recordExternalWorkEvent({ external_work_id: 'ew_1', type: 'heartbeat' }, d)
    await recordExternalWorkEvent({ external_work_id: 'ew_1', idempotency_key: '', type: 'heartbeat' }, d)
    const keys = [calls[2], calls[3]].map((c) => (c.body as { idempotency_key: string }).idempotency_key)
    for (const key of keys) assert.match(key, /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/)
    assert.notEqual(keys[0], keys[1])

    // An explicit seq and key go out exactly as before.
    await recordExternalWorkEvent({ external_work_id: 'ew_1', idempotency_key: 'sub', seq: 9, type: 'submitted', commit_shas: [sha], exit_code: 0 }, d)
    assert.deepEqual(calls[4].body, { idempotency_key: 'sub', seq: 9, type: 'submitted', commit_shas: [sha], exit_code: 0 })
  })
  it('names a commit by its full lowercase SHA', () => {
    assert.equal(COMMIT_SHA.test('0123456789abcdef0123456789abcdef01234567'), true)
    assert.equal(COMMIT_SHA.test('a'.repeat(64)), true)
    for (const bad of ['0123456', 'A'.repeat(40), 'g'.repeat(40), 'a'.repeat(41), 'main']) assert.equal(COMMIT_SHA.test(bad), false, bad)
  })
  it('the served schema makes seq and the key optional and describes the registration artifact', () => {
    const index = readFileSync(new URL('./index.ts', import.meta.url), 'utf8')
    const event = index.slice(index.indexOf("server.registerTool('record_external_work_event'"))
    assert.match(event, /idempotency_key: z\.string\(\)\.min\(1\)\.optional\(\)/)
    assert.match(event, /seq: z\.number\(\)\.int\(\)\.min\(1\)\.optional\(\)/)
    assert.match(event, /commit_shas: z\.array\(z\.string\(\)\.regex\(COMMIT_SHA/)
    // The example in register_external_work's description is one the
    // platform's parser takes (store.parseExternalDescription): the nine
    // fields and no other, v 1, the kind, non-empty texts, two estimates.
    const registration = index.slice(index.indexOf("server.registerTool('register_external_work'"), index.indexOf("server.registerTool('record_external_work_event'"))
    const example = registration.match(/\{"v": 1,[^}]*\}/)
    assert.ok(example, 'register_external_work describes its artifact with an example')
    const doc = JSON.parse(example[0]) as Record<string, unknown>
    assert.deepEqual(Object.keys(doc).sort(), ['acceptance_refs', 'branch', 'estimate_minutes', 'executor', 'kind', 'provider', 'repo', 'task', 'v'])
    assert.equal(doc.v, 1)
    assert.equal(doc.kind, 'agentrt.external_work/v1')
    for (const key of ['task', 'executor', 'provider', 'repo', 'branch']) assert.ok(typeof doc[key] === 'string' && (doc[key] as string).trim() !== '', key)
    const refs = doc.acceptance_refs as string[]
    assert.ok(Array.isArray(refs) && refs.length > 0 && refs.every((r) => typeof r === 'string' && r.trim() !== ''))
    const estimate = doc.estimate_minutes as number[]
    assert.ok(Array.isArray(estimate) && estimate.length === 2 && Number.isInteger(estimate[0]) && Number.isInteger(estimate[1]))
    assert.ok(estimate[0] >= 0 && estimate[1] >= estimate[0])
  })
  it('refuses writes when no real session exists', async () => {
    for (const over of [{ fellBack: true }]) {
      const { d, calls } = deps(over)
      await registerExternalWork({ idempotency_key: 'k', root: 'R4', plan_node_id: 'n', source_run_key: 'r', artifact_id: 'x' }, d)
      await recordExternalWorkEvent({ external_work_id: 'x', idempotency_key: 'k', seq: 1, type: 'started' }, d)
      assert.equal(calls.length, 0)
    }
  })
  it('passes adoption CAS through the existing proposal path', async () => {
    const { d, calls } = deps()
    const changes = [{ op: 'adopt_plan_view' as const, artifact: 'sha256:' + 'a'.repeat(64), expected_version: 0 }]
    await proposePlanChange({ root: 'R4', reason: 'adopt', changes, idempotency_key: 'adopt' }, d)
    assert.deepEqual((calls[0].body as { changes: unknown }).changes, changes)
    for (const name of ['get_plan_view', 'register_external_work', 'record_external_work_event'] as const) assert(PLATFORM_STDIO_TOOL_NAMES.includes(name))
  })
})
