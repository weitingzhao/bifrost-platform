import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { presenceBody, presenceSkip, registerPresence } from './presence.js'
import type { RuntimeDeps } from './runtimeTools.js'

// Origin: 2026-11-14 evening, two "not a registered actor" 422s on PROD: the
// agent-tool-comm doorbell test, and seat 9.1 messaging another seat (it fell
// back to a file). A thread is registered on its first write only.

function deps(over: Partial<RuntimeDeps> = {}) {
  const uploads: { path: string; body: string; kind?: string }[] = []
  const d: RuntimeDeps = {
    get: async () => ({}),
    getText: async () => ({ media_type: 'text/plain', text: '' }),
    send: async () => ({}),
    upload: async (path, body, _ct, headers) => { uploads.push({ path, body, kind: headers['X-Artifact-Kind'] }); return { created: true } },
    session: () => 'local_p',
    fellBack: () => false,
    ...over,
  }
  return { d, uploads }
}

describe('presence at server start', () => {
  it('only the full server registers, only against a real platform, and it can be turned off', () => {
    const prod = { PLATFORM_API_URL: 'http://192.168.10.100:30876' }
    assert.equal(presenceSkip(prod, ''), undefined)
    assert.equal(presenceSkip(prod, 'kubernetes'), 'focus kubernetes')
    assert.equal(presenceSkip({ PLATFORM_API_URL: 'http://127.0.0.1:41234' }, ''), 'loopback platform')
    assert.equal(presenceSkip({}, ''), 'loopback platform', 'the default URL is the local dev platform')
    assert.equal(presenceSkip({ ...prod, BIFROST_MCP_NO_PRESENCE: '1' }, ''), 'turned off')
  })

  it('uploads the same small body for one thread, without a local path', async () => {
    const { d, uploads } = deps()
    assert.equal(await registerPresence(d), 'registered')
    await registerPresence(d)
    assert.equal(uploads.length, 2)
    assert.equal(uploads[0].path, uploads[1].path, 'same content address: the platform stores it once')
    assert.equal(uploads[0].body, presenceBody('local_p'))
    assert.equal(uploads[0].kind, 'report')
    assert.ok(!/\/Users\/|\/home\//.test(uploads[0].body))
  })

  it('never throws and does not register without a session', async () => {
    const { d, uploads } = deps({ fellBack: () => true })
    assert.equal(await registerPresence(d), 'no session id')
    assert.equal(uploads.length, 0)
    const { d: down } = deps({ upload: async () => { throw new Error('ECONNREFUSED') } })
    assert.match(await registerPresence(down), /failed: ECONNREFUSED/)
    const { d: refused } = deps({ upload: async () => ({ error: 'HTTP 403' }) })
    assert.match(await registerPresence(refused), /refused/)
  })
})
