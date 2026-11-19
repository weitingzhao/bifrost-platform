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
  // The Owner first said show all 15, then the same day: the plant generates, it does not know what the
  // power is used for — the room shows the hardware only.
  it('the CPU unit: Details and Inside confirmed (the room, 2026-11-19); Summary open, every chosen detail placed', () => {
    const cpu = UNITS.find(u => u.id === 'cpu')!
    expect(stagesOf(cpu).slice(0, 3)).toEqual([{ id: 'details', state: 'confirmed' }, { id: 'inside', state: 'confirmed' }, { id: 'summary', state: 'open' }])
    for (const use of ['busy.mode', 'running', 'blocked', 'switches', 'interrupts', 'guest', 'load1', 'load5', 'load15', 'pressure']) expect(cpu.chosen).not.toContain(use)
    expect(new Set(cpu.inside!.map(i => i.detail))).toEqual(new Set(cpu.chosen))
    expect(STAGES.map(s => s.id)).toEqual(['details', 'inside', 'summary', 'outside', 'in-use'])
  })
})

// 2026-11-19, stage 3 for the CPU: the summary is what the console shows; one computation for both.
import { cpuSummaryOf } from '../units/cpuRoom'
describe('the CPU summary', () => {
  const cpu = UNITS.find(u => u.id === 'cpu')!
  it('four items, each from chosen details, each through a dictionary row', () => {
    expect(cpu.summary!.map(i => i.id)).toEqual(['cores', 'busy', 'clock', 'temp'])
    expect(unitRulesBroken(cpu)).toEqual([])
  })
  it('a summary row the dictionary lacks is refused', () => {
    const bad = { ...cpu, summary: [{ id: 'x', label: 'X', from: ['cores'], how: 'count' as const, row: 'no.such.row' }] }
    expect(unitRulesBroken(bad).join()).toMatch(/does not have/)
  })
  it('computed from the room’s readings only: average busy and clock, the hottest chip sensor, null when not read', () => {
    const s = cpuSummaryOf({ cores: [{ busy: 0.2, freq: 2e9, max: 4e9 }, { busy: 0.4, freq: 3e9, max: 4e9 }], temps: [{ name: 'Tctl', c: 60 }, { name: 'Core 0', c: 90, core: 0 }] })
    expect(s.cores).toBe(2)
    expect(s.busy).toBeCloseTo(0.3)
    expect(s.clock).toBe(2.5e9)
    expect(s.temp).toEqual({ c: 60, name: 'Tctl' })
    expect(cpuSummaryOf({ cores: [], temps: [] })).toEqual({ cores: 0, busy: null, clock: null, top: null, temp: null })
  })
})
