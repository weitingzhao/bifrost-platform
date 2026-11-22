import assert from 'node:assert/strict'
import { test } from 'node:test'
import { parseRoster, resolveRoster } from './roster.js'

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
