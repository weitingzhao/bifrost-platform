import { test } from 'node:test'
import assert from 'node:assert/strict'
import { existsSync, mkdtempSync, readFileSync, utimesSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { acceptanceBody, blocksSince, machineName, shipBlocks, shipBlocksLocked } from './foundationPost.js'
import type { RuntimeDeps } from './runtimeTools.js'

// Origin: 决定 2026-11-21, Foundation's state goes on the bus; the guard's
// refusals were only in one Mac's local log. Each whole line after the offset is
// one block, posted once: the offset moves past whole lines only, so a line the
// guard is still writing waits for the next run (the control: a half line).
test('blocks since the offset are the whole lines after it', () => {
  const log = '2026-11-21T00:00:00.000Z\towner\ts1\n2026-11-21T00:01:00.000Z\tgitea-primary\ts2\n'
  const first = blocksSince(log, 0, 'mac')
  assert.deepEqual(first.blocks.map((b) => b.rule), ['owner', 'gitea-primary'])
  assert.equal(first.next, Buffer.byteLength(log))
  assert.deepEqual(blocksSince(log, first.next, 'mac').blocks, [])
  const half = blocksSince(log + '2026-11-21T00:02:00.000Z\tD10', first.next, 'mac')
  assert.deepEqual(half.blocks, [])
  assert.equal(half.next, first.next)
})

test('a machine name the platform accepts', () => {
  assert.equal(machineName('Vision Mac.local'), 'Vision-Mac')
})

// Origin: same day; the acceptance script's --json output is the route's body,
// items in item order.
test('the acceptance output becomes the route body', () => {
  const b = acceptanceBody({ ruler: 3, score: '1/10', head: 'a'.repeat(40), read_at: 't',
    items: [{ item: 2, status: 'fail' }, { item: 1, status: 'pass' }] })
  assert.deepEqual(b.items, ['pass', 'fail'])
  assert.equal(b.platform_commit, 'a'.repeat(40))
})

// Origin: 2026-11-21, the communication line's review: a refusal moved the
// offset on and the block was lost. Now a failure that is not a 400 stops at
// its line (posted on the next run); a 400 skips the line (the control).
test('a failure stops at its line, a 400 skips it', async () => {
  const dir = mkdtempSync(join(tmpdir(), 'fpost-'))
  const log = join(dir, 'blocks.log')
  writeFileSync(log, 't\towner\ts1\nt\tgitea-primary\ts2\nt\tD10\ts3\n')
  const posted: string[] = []
  const deps = (fail: (rule: string) => string | undefined) =>
    ({ send: async (_m: string, _p: string, b: { rule: string }) => {
      const why = fail(b.rule)
      if (why) throw new Error(`POST /x: HTTP ${why}`)
      posted.push(b.rule)
      return {}
    } }) as unknown as RuntimeDeps
  assert.equal(await shipBlocks(log, deps((r) => (r === 'gitea-primary' ? '503 busy' : undefined)), () => {}), 1)
  assert.deepEqual(posted, ['owner'])
  assert.equal(readFileSync(`${log}.shipped`, 'utf8'), String('t\towner\ts1\n'.length))
  assert.equal(await shipBlocks(log, deps((r) => (r === 'gitea-primary' ? '400 bad' : undefined)), () => {}), 1)
  assert.deepEqual(posted, ['owner', 'D10'])
})

// Origin: 2026-11-21, every session's doorbell now ships the machine's block
// log; two at once must not post a line twice. A fresh lock means another
// process is shipping (-1, nothing posted); a lock older than two minutes is
// taken as left behind and the run goes ahead (the control).
test('a fresh lock skips the run, a stale one is taken', async () => {
  const dir = mkdtempSync(join(tmpdir(), 'fpost-lock-'))
  const log = join(dir, 'blocks.log')
  writeFileSync(log, 't\towner\ts1\n')
  const posted: string[] = []
  const deps = { send: async (_m: string, _p: string, b: { rule: string }) => { posted.push(b.rule); return {} } } as unknown as RuntimeDeps
  writeFileSync(`${log}.lock`, '')
  assert.equal(await shipBlocksLocked(log, deps, () => {}), -1)
  assert.deepEqual(posted, [])
  const old = new Date(Date.now() - 3 * 60_000)
  utimesSync(`${log}.lock`, old, old)
  assert.equal(await shipBlocksLocked(log, deps, () => {}), 1)
  assert.deepEqual(posted, ['owner'])
  assert.equal(existsSync(`${log}.lock`), false)
})
