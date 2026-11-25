// 2026-11-25: Agents read a frozen copy of seats.yaml, so every session opened after it showed as a raw thread id
// "Not in the seat roster" (the Owner's phone review, 2026-11-24). The map line now serves the roster
// (GET /api/v1/catalog/seats, platform 3f804e06; the engine's read=seats). Rows below are STG's, 2026-11-25 19:37Z.
import { expect, it } from 'vitest'
import { membersOfRoster, seatSource, withRoster, type SeatRoster } from '../seatRoster'

const roster: SeatRoster = {
  commit: 'e4f93c0d0000', at: '2026-11-25T19:28:00Z',
  seats: [
    { seat: 'company-coordinator', thread: 'thread:local_accc', kind: 'owner', harness: 'claude', mailbox: 'registered' },
    { seat: '8-1-duty', thread: 'thread:local_accc', kind: 'temporary', domain: 'coordination' },
    { seat: '0-foundation-decouple', thread: 'thread:local_c00f', kind: 'owner', domain: 'foundation', domain_name: 'Foundation' },
    { seat: 'codex-owner-td287', thread: 'codex-01a204b7', kind: 'temporary', domain: 'compute', harness: 'codex' },
    { seat: 'vacant-seat', kind: 'owner', domain: 'audit' },
    { seat: 'old-visit', thread: 'thread:local_old', kind: 'temporary', domain: 'plan', ended: '2026-11-23' },
  ],
}

it('makes one member per session with every seat it sits, the owner seat first and naming it', () => {
  const members = membersOfRoster(roster)
  expect(members.map(m => m.id)).toEqual(['thread:local_accc', 'thread:local_c00f', 'codex-01a204b7'])
  const coord = members[0]
  expect(coord.label).toBe('company-coordinator')
  expect(coord.memberships.map(m => [m.seat, m.domain, m.role])).toEqual([['company-coordinator', null, 'owner'], ['8-1-duty', 'coordination', 'temporary']])
  // Another tool's session keeps its own id (no thread: prefix), as its letters name it.
  expect(members[2].memberships[0]).toEqual(expect.objectContaining({ domain: 'compute', role: 'temporary' }))
  // Controls: a seat nobody sits, and a temporary seat that ended, are no member.
  expect(members.some(m => m.label === 'vacant-seat')).toBe(false)
  expect(members.some(m => m.id === 'thread:local_old')).toBe(false)
})

it('says where the seats come from, and falls back to the copy only with the reason', () => {
  expect(seatSource(roster, null).note).toContain('read 2026-11-25T19:28:00Z, infra e4f93c0d')
  // The roster holds no people: the Owner stays from the copy.
  expect(seatSource(roster, null).members.map(m => m.id)).toContain('owner')
  const back = seatSource(undefined, new Error('Seat roster: HTTP 404'))
  expect(back.note).toMatch(/copy of the seat roster imported .*the live roster is not read \(Seat roster: HTTP 404\)/)
  expect(back.members.length).toBeGreaterThan(0)
})

it('replaces an engine read’s memberships with the live roster’s, keeping the thread’s own title', () => {
  const engine = [{ id: 'thread:local_c00f', label: 'Foundation decouple (title)', memberships: [] }, { id: 'thread:local_zzz', label: 'thread:local_zzz', memberships: [] }]
  const out = withRoster(engine, membersOfRoster(roster))
  expect(out[0]).toEqual(expect.objectContaining({ label: 'Foundation decouple (title)', memberships: [expect.objectContaining({ domain: 'foundation' })] }))
  // Control: a member the roster does not have stays as the engine gave it; roster members not in the read are added.
  expect(out[1]).toEqual(engine[1])
  expect(out.map(m => m.id)).toEqual(expect.arrayContaining(['thread:local_accc', 'codex-01a204b7']))
})
