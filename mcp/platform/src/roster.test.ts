import assert from 'node:assert/strict'
import { test } from 'node:test'
import { loadRoster, parseRoster, resolveRoster, type Git } from './roster.js'

const roster = `version: 1
seats:
  - {id: company-coordinator, charter: company-coordinator.md, domain_id: null, kind: owner, harness: claude, session: local_accccb4e-410c-4b5f-9f1b-b54c6915dc9a}
  - {id: 6-compute,       charter: 6-compute.md, domain_id: compute, kind: owner, harness: codex,  session: null, liaison: local_7dd39a39-1e0e-4878-b829-2e3f44d5589e, note: "a note, with a comma"}
  - {id: 9-crew, name: crew,          charter: 9-crew.md, kind: owner, harness: claude, session: local_2c3fa7a5-f04c-41a0-8528-ce1b5a07eb86}
  - {id: owner, role: lawgiver, harness: human}
`

// Origin: 2026-11-21, Station's hand-over: a change of session must not mean a change of
// address. roster:<id or name> is the thread that sits the seat now; a Codex seat's mail goes
// to its Claude-side liaison; a seat with no session, or none by that name, is refused.
test('roster:<seat> resolves to the thread that sits it', () => {
  const seats = parseRoster(roster)
  assert.deepEqual(resolveRoster('roster:company-coordinator', seats), { to: 'thread:local_accccb4e-410c-4b5f-9f1b-b54c6915dc9a' })
  assert.deepEqual(resolveRoster('roster:crew', seats), { to: 'thread:local_2c3fa7a5-f04c-41a0-8528-ce1b5a07eb86' })
  assert.deepEqual(resolveRoster('roster:6-compute', seats), { to: 'thread:local_7dd39a39-1e0e-4878-b829-2e3f44d5589e' })
  assert.ok('error' in resolveRoster('roster:owner', seats))
  assert.ok('error' in resolveRoster('roster:12-audit', seats))
})

// 2026-11-24: the shared infra checkout was 131 commits behind main and
// roster:audit resolved three times to the session the seat had left. The
// roster is read from infra's origin/main after a fetch; the working tree is
// only the fallback, and the answer says which was read.
test('the roster is read from infra main, the working tree only when git fails', () => {
  const env = { BIFROST_WORKSPACE: '/ws' } as NodeJS.ProcessEnv
  const old = '  - {id: 12-audit, name: audit, session: local_old}\n'
  const now = '  - {id: 12-audit, name: audit, session: local_new}\n'
  const calls: string[][] = []
  const git: Git = (cwd, args) => {
    calls.push([cwd, ...args])
    if (args[0] === 'show') return now
    if (args[0] === 'rev-parse') return 'abc1234\n'
    return ''
  }
  const fromMain = loadRoster(env, () => old, git)
  assert.deepEqual(resolveRoster('roster:audit', fromMain.seats), { to: 'thread:local_new' })
  assert.equal(fromMain.source, 'bifrost-trade-infra origin/main@abc1234')
  assert.deepEqual(calls[0], ['/ws/bifrost-trade-infra', 'fetch', '-q', 'origin', 'main'])
  const offline = loadRoster(env, () => old, () => { throw new Error('offline') })
  assert.deepEqual(resolveRoster('roster:audit', offline.seats), { to: 'thread:local_old' })
  assert.match(offline.source, /working tree .*may be stale/)
})
