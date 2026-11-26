import { describe, expect, it } from 'vitest'
import type { Place } from '@bifrost/ui/city/places'
import { rulesBroken } from '@bifrost/ui/city/rules'

// 2026-11-19 (ISSUE-9): the ground and its rules are the frame's; a domain that uses this look must
// keep them and cannot change them. Before this, only the Console content's own test held them, so a
// second domain could have been drawn broken.
const p = (id: string, kind: Place['kind'], shape: string, children: Place[] = [], fn?: string): Place =>
  ({ id, kind, shape: shape as Place['shape'], label: id, question: '', dashboard: null, children, ...(fn ? { function: fn } : {}) })
const good = () => p('town', 'town', '8x8', [p('a', 'district', '8x8', [p('b', 'building', '4x8', [p('r', 'room', '4x4')])], 'fa')])

describe('the frame holds its rules on any content', () => {
  it('good content passes', () => {
    expect(rulesBroken({ tree: good(), functions: [{ id: 'fa' }] })).toEqual([])
  })
  it('the Console content passes', async () => {
    const { consoleContent } = await import('../../content/console')
    expect(rulesBroken(consoleContent())).toEqual([])
  })
  it('a shape not the frame’s is refused', () => {
    const t = good(); t.children[0].shape = '3x3' as Place['shape']
    expect(rulesBroken({ tree: t }).join()).toMatch(/3x3 is not one of/)
  })
  // 2026-11-20 (ISSUE-9): the unit is 4×4; nothing is smaller.
  it('any thing under one unit is refused', () => {
    const u = good(); u.children[0].children[0].children[0].shape = '2x2'
    expect(rulesBroken({ tree: u }).join()).toMatch(/smallest thing is 4×4/)
  })
  it('more than the ground holds is refused', () => {
    const t = good(); t.children = Array.from({ length: 5 }, (_, i) => p(`d${i}`, 'district', '8x8'))
    expect(rulesBroken({ tree: t }).join()).toMatch(/takes 320 cells/)
  })
  // 2026-11-24, PROD: the hardware district's sheets were counted as one ground, so 272 cells of devices broke it
  // whatever land it had. A district of 2 or 4 sheets holds a ground on each; each sheet keeps the ground's rules.
  it('a district of several sheets holds a ground on each, and each sheet keeps the ground', () => {
    const bs = (n: number, from = 0) => Array.from({ length: n }, (_, i) => ({ ...p(`b${from + i}`, 'building', '4x4'), sheet: from ? 1 : 0 }))
    const two = p('town', 'town', '8x8', [p('u', 'district', '4x8', [...bs(16), ...bs(4, 16)])])
    expect(rulesBroken({ tree: two })).toEqual([])
    const over = p('town', 'town', '8x8', [p('u', 'district', '4x8', [...bs(16), ...bs(17, 16)])])
    expect(rulesBroken({ tree: over }).join()).toMatch(/town\/u \(sheet 2 of 2\): holds 17 things/)
    const one = p('town', 'town', '8x8', [p('u', 'district', '4x4', bs(17))])
    expect(rulesBroken({ tree: one }).join()).toMatch(/town\/u: holds 17 things/)
  })
  it('a fourth level, a wrong kind, a shared id, an unknown function are refused', () => {
    const t = good()
    t.children[0].children[0].children[0].children = [p('x', 'room', '1x1')]
    t.children.push(p('a', 'building', '2x2'))
    t.children[0].function = 'nope'
    const all = rulesBroken({ tree: t, functions: [{ id: 'fa' }] }).join('\n')
    expect(all).toMatch(/no level below/)
    expect(all).toMatch(/holds districts, not a building/)
    expect(all).toMatch(/share this id/)
    expect(all).toMatch(/function nope/)
  })
  // 2026-11-19 (ISSUE-33): nothing on the screen is made up, nor written in once from a measurement and left.
  it('a number with no read behind it is refused; an unread meter may say why in words', () => {
    const t = good()
    const room = t.children[0].children[0].children[0]
    room.state = 'online'
    room.meter = { used: 3, of: 16, unit: 'cores', source: 'measured by hand on Tuesday' }
    expect(rulesBroken({ tree: t }).join()).toMatch(/must come from a read/)
    room.meter = { used: 3, of: 16, unit: 'cores', source: 'sum by (instance) (rate(node_cpu_seconds_total[5m]))' }
    expect(rulesBroken({ tree: t })).toEqual([])
    room.state = 'unread'
    room.meter = { used: null, of: null, unit: 'cores', source: 'no exporter: not read' }
    expect(rulesBroken({ tree: t })).toEqual([])
  })
  it('a tally is a count read from a query, never written in', () => {
    const t = good()
    t.children[0].children[0].face = { tally: { n: 12, what: 'pods', source: 'counted by hand' } }
    expect(rulesBroken({ tree: t }).join()).toMatch(/a tally is a count read/)
  })
})
