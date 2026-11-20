import { describe, expect, it } from 'vitest'
import { DICTIONARY, rowsDrawn } from '../../frame/library/dictionary'
import { keyOf, partsOf } from '../../frame/library/key'
import { deviceSprite, formOfPlace, levelsOf } from '../../frame/devices'
import { hardwareDistrict, type HardwareReads } from '../hardware'
import fixture from './hardware.fixture.json'

// 2026-11-19, the Owner: the metaphor standard must be usable in the page — open a power plant and see
// what each thing on it means, like a chart's legend — before a second facility is designed, so the two
// can be compared row by row. The fixture is the real read of that day (hardware.test.ts).
const d = hardwareDistrict(fixture as unknown as HardwareReads)
const dev = (id: string) => d.children.find(b => b.id === id)!

describe('a facility’s key', () => {
  it('lists the dictionary rows its picture looks up, in the table’s order; what it draws now is never more', () => {
    for (const k of d.children) {
      const ids = partsOf(k), rows = keyOf(k).rows.map(r => r.id)
      expect(rows).toEqual(DICTIONARY.map(m => m.id).filter(id => ids.includes(id)))
      const now = rowsDrawn(() => deviceSprite(formOfPlace(k), 4, 4, '#cccccc', 'day', levelsOf(k), k.id, k.face)).ids
      for (const id of now) expect(rows).toContain(id)
    }
  })
  // The first try listed only what the standby GPU box drew that moment: its smoke and both tank levels
  // went missing from the key. A key lists the parts a facility has, in any state.
  it('a facility on standby keeps its parts in the key, each saying it is dark', () => {
    const rows = keyOf(dev('gpu-server')).rows
    expect(rows.map(r => r.id)).toEqual(expect.arrayContaining(['cpu.smoke', 'mem.used', 'disk.used']))
    expect(rows.find(r => r.id === 'mem.used')!.now).toBe('standby: dark')
  })
  it('a running power plant shows all of its surfaces, each with what it shows now', () => {
    const k = dev('mini-pc-a'), { form, rows } = keyOf(k)
    expect(form).toBe('plant')
    expect(rows.map(r => r.id)).toEqual(['cpu.busy', 'cpu.smoke', 'cores', 'mem.used', 'mem.total', 'disk.used', 'disk.total', 'pods', 'identity', 'purpose', 'state'])
    for (const r of rows) expect(r.now).not.toBe('')
    expect(rows.find(r => r.id === 'cores')!.now).toMatch(/^\d+ cores$/)
    expect(rows.find(r => r.id === 'mem.used')!.source).toBeTruthy()
  })
  it('a machine nothing reads says so rather than a number', () => {
    const rows = keyOf(dev('win11-host')).rows
    expect(rows.map(r => r.id)).toEqual(['state'])
    expect(rows[0].now).toBe('unread')
  })
})
