import assert from 'node:assert/strict'
import { test } from 'node:test'
import { presenceBody, ring, settingsFromMcpConfig, type DoorbellIO } from './doorbell.js'
import type { RuntimeDeps } from './runtimeTools.js'

// Origin: 2026-11-14 doorbell test (agent-tool-comm seat). A message to a
// thread that had only read the platform was refused with HTTP 422 "not a
// registered actor"; a background command that waits on the inbox woke an
// idle Claude Code session about 7 s after a message was sent.

function fake(replies: unknown[], opts: { fellBack?: boolean } = {}) {
  const calls: { get: string[]; upload: { path: string; body: string; kind?: string }[] } = { get: [], upload: [] }
  let t = 0
  const deps: RuntimeDeps = {
    get: async (path) => {
      calls.get.push(path)
      t += 1000
      return replies.shift() ?? { messages: [] }
    },
    getText: async () => ({ media_type: 'text/plain', text: '' }),
    send: async () => ({}),
    upload: async (path, body, _ct, headers) => {
      calls.upload.push({ path, body, kind: headers['X-Artifact-Kind'] })
      return { created: true }
    },
    session: () => 'local_test',
    fellBack: () => opts.fellBack ?? false,
  }
  const lines: string[] = []
  const io: DoorbellIO = { deps, out: (l) => lines.push(l), now: () => t }
  return { io, calls, lines, advance: (ms: number) => { t += ms } }
}

test('a waiting message ends the wait with exit 0 and a summary without the body', async () => {
  const f = fake([{ actor: 'thread:local_test', messages: [{ message_id: 'm_1', from: 'thread:x', kind: 'note', body: 'secret-ish body' }] }])
  assert.equal(await ring({ minutes: 1, register: true }, f.io), 0)
  assert.match(f.lines[0], /1 message\(s\) for thread:local_test/)
  assert.match(f.lines[1], /m_1 from=thread:x kind=note bytes=15/)
  assert.ok(!f.lines.join('\n').includes('secret-ish'), 'the body is never printed')
})

test('the thread registers itself before waiting, with the same body for one thread', async () => {
  const f = fake([{ messages: [{ message_id: 'm_1' }] }])
  await ring({ minutes: 1, register: true }, f.io)
  assert.equal(f.calls.upload.length, 1)
  assert.equal(f.calls.upload[0].body, presenceBody('local_test'))
  assert.equal(f.calls.upload[0].kind, 'report')
  assert.ok(!/\/Users\/|\/home\//.test(f.calls.upload[0].body), 'no local path: the platform refuses one')
  const g = fake([{ messages: [{ message_id: 'm_1' }] }])
  await ring({ minutes: 1, register: false }, g.io)
  assert.equal(g.calls.upload.length, 0)
})

test('no message by the deadline is exit 2', async () => {
  const f = fake([])
  // each empty long poll moves the fake clock by 1 s; a 0.05-minute deadline is 3 s
  assert.equal(await ring({ minutes: 0.05, register: false }, f.io), 2)
  assert.ok(f.calls.get.length >= 1)
  assert.match(f.lines.at(-1) ?? '', /no message by the deadline/)
})

test('without a session id it refuses instead of waiting as the host name', async () => {
  const f = fake([], { fellBack: true })
  await assert.rejects(ring({ minutes: 1, register: true }, f.io), /no session id/)
  assert.equal(f.calls.upload.length + f.calls.get.length, 0)
})

test('only the URL and the token key name are taken from an MCP config', () => {
  const text = JSON.stringify({
    mcpServers: {
      'bifrost-platform': {
        env: {
          PLATFORM_API_URL: '${PLATFORM_URL:-http://10.0.0.1:30876}',
          PLATFORM_TOKEN_ENV_KEY: 'PLATFORM_OPERATOR_TOKEN',
          PLATFORM_OPERATOR_TOKEN: '${PLATFORM_OPERATOR_TOKEN}',
        },
      },
    },
  })
  assert.deepEqual(settingsFromMcpConfig(text, 'bifrost-platform', {}), {
    PLATFORM_API_URL: 'http://10.0.0.1:30876',
    PLATFORM_TOKEN_ENV_KEY: 'PLATFORM_OPERATOR_TOKEN',
  })
  assert.equal(settingsFromMcpConfig(text, 'bifrost-platform', { PLATFORM_URL: 'http://h:1' }).PLATFORM_API_URL, 'http://h:1')
  assert.throws(() => settingsFromMcpConfig(text, 'nope', {}), /no server nope/)
})
