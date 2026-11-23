import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { holdReleaseWindow, releaseReleaseWindow, getReleaseWindow, WINDOW_PATH, type WindowDeps } from './windowTools.js'

// Origin: 2026-11-14 card #557 was approved while the window was held and
// failed; agents blocked by the window had no way to be told when it freed
// (locks and queue design v1). Agents could hold the window only through
// release.sh, not through a platform tool (agent-parity, 11-14).

function deps(over: Partial<WindowDeps> = {}) {
  const calls: { kind: string; path: string; body?: unknown }[] = []
  const d: WindowDeps = {
    get: async (path) => { calls.push({ kind: 'get', path }); return { open: false, queue: [] } },
    put: async (path, body) => { calls.push({ kind: 'put', path, body }); return { position: 1 } },
    del: async (path, body) => { calls.push({ kind: 'del', path, body }); return {} },
    session: () => 'local_w',
    fellBack: () => false,
    ...over,
  }
  return { d, calls }
}

describe('release window tools', () => {
  it('hold queues by default, as this session, with its thread for the your_turn notice', async () => {
    const { d, calls } = deps()
    await holdReleaseWindow({ what: 'bifrost-platform', resume: 'deliver platform 04175d0e to STG' }, d)
    assert.deepEqual(calls, [{
      kind: 'put',
      path: WINDOW_PATH,
      body: { who: 'local_w', what: 'bifrost-platform', queue: true, thread: 'thread:local_w', resume: 'deliver platform 04175d0e to STG' },
    }])
  })

  it('queue:false asks for the window now and sends no thread', async () => {
    const { d, calls } = deps()
    await holdReleaseWindow({ what: 'bifrost-platform', queue: false, ttl_minutes: 10, resume: 'ignored' }, d)
    assert.deepEqual(calls[0].body, { who: 'local_w', what: 'bifrost-platform', ttl_minutes: 10 })
  })

  // 2026-11-23: cockpit's release got 400 "missing param: who" because who
  // went in the query and the server's action gate reads it from the body.
  it('release names this session in the body; read goes to the window route', async () => {
    const { d, calls } = deps()
    await releaseReleaseWindow(d)
    await getReleaseWindow(d)
    assert.deepEqual(calls, [{ kind: 'del', path: WINDOW_PATH, body: { who: 'local_w' } }, { kind: 'get', path: WINDOW_PATH }])
  })

  it('without a session id nothing is held or released', async () => {
    const { d, calls } = deps({ fellBack: () => true })
    assert.ok(((await holdReleaseWindow({ what: 'bifrost-platform' }, d)) as { error?: string }).error)
    assert.ok(((await releaseReleaseWindow(d)) as { error?: string }).error)
    assert.equal(calls.length, 0)
  })
})
