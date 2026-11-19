import { describe, expect, it } from 'vitest'
import type { Place } from '../places'
import { rulesBroken } from '../rules'

// 2026-11-19, the Owner: the ground and its rules are the frame's; a domain that uses this look must
// keep them and cannot change them. Before this, only the Console content's own test held them, so a
// second domain could have been drawn broken.
const p = (id: string, kind: Place['kind'], shape: string, children: Place[] = [], fn?: string): Place =>
  ({ id, kind, shape: shape as Place['shape'], label: id, question: '', dashboard: null, children, ...(fn ? { function: fn } : {}) })
const good = () => p('town', 'town', '8x8', [p('a', 'district', '4x4', [p('b', 'building', '2x4', [p('r', 'room', '1x1')])], 'fa')])

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
  it('a door under 2 wide is refused; a 1-wide room is fine', () => {
    const t = good(); t.children[0].children[0].shape = '1x2'
    expect(rulesBroken({ tree: t }).join()).toMatch(/at least 2 wide/)
  })
  it('more than the ground holds is refused', () => {
    const t = good(); t.children = Array.from({ length: 5 }, (_, i) => p(`d${i}`, 'district', '8x8'))
    expect(rulesBroken({ tree: t }).join()).toMatch(/takes 320 cells/)
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
})
