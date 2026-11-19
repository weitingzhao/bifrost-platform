import { describe, expect, it } from 'vitest'
import { STAGES, stagesOf, unitRulesBroken, type UnitSpec } from '../units/process'
import { UNITS } from '../../content/units/cpu'

// 2026-11-19, the Owner: build from the inside out — the detail first, in full, then each level a
// summary of the one before — and set the process up first, with a page to confirm each stage on.
const base: UnitSpec = { id: 't', name: 'T', what: '', details: [{ id: 'a', label: 'A', meaning: '', query: 'up', unit: 'count', per: 'machine' }], confirmed: {} }

describe('the unit process', () => {
  it('every unit spec holds', () => {
    for (const u of UNITS) expect(unitRulesBroken(u)).toEqual([])
  })
  it('one stage is open at a time: the first not confirmed; later ones wait', () => {
    expect(stagesOf(base).map(s => s.state)).toEqual(['open', 'waiting', 'waiting', 'waiting', 'waiting'])
    const one = { ...base, chosen: ['a'], confirmed: { details: { on: '2026-11-19', words: 'ok' } } }
    expect(stagesOf(one).map(s => s.state)).toEqual(['confirmed', 'open', 'waiting', 'waiting', 'waiting'])
  })
  it('a stage cannot be confirmed over an earlier one', () => {
    expect(unitRulesBroken({ ...base, confirmed: { inside: { on: '2026-11-19', words: 'ok' } } }).join()).toMatch(/before an earlier stage/)
  })
  it('the room places every chosen detail, and nothing that is not chosen', () => {
    const u = { ...base, chosen: ['a'], inside: [] }
    expect(unitRulesBroken(u).join()).toMatch(/no place in the room/)
  })
  it('a summary is made only from details the room shows: nothing outside that is not inside', () => {
    const u = { ...base, chosen: ['a'], summary: [{ id: 's', label: 'S', from: ['b'], how: 'average' as const, row: 'cpu.busy' }] }
    expect(unitRulesBroken(u).join()).toMatch(/does not show/)
  })
  it('the CPU unit: Details confirmed with all 15 shown (the Owner, 2026-11-19); Inside open, every detail placed', () => {
    const cpu = UNITS.find(u => u.id === 'cpu')!
    expect(stagesOf(cpu).slice(0, 2)).toEqual([{ id: 'details', state: 'confirmed' }, { id: 'inside', state: 'open' }])
    expect(cpu.chosen).toHaveLength(cpu.details.length)
    expect(new Set(cpu.inside!.map(i => i.detail))).toEqual(new Set(cpu.chosen))
    expect(STAGES.map(s => s.id)).toEqual(['details', 'inside', 'summary', 'outside', 'in-use'])
  })
})
