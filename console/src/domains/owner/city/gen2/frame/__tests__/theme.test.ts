import { describe, expect, it } from 'vitest'
import type { Place } from '../places'
import { HUES } from '../sprites'
import { hueOf } from '../theme'

// 2026-11-19, the Owner: things differ and their look passes down (a district's theme to its
// buildings, a building's to its rooms); the ways between them are one standard.
const node = (id: string, children: Place[] = []): Place => ({ id, kind: 'district', shape: '4x4', label: id, question: '', dashboard: null, children })
const tree = node('town', [node('a', [node('a1', [node('r1'), node('r2')]), node('a2')]), node('b', [node('b1')])])

describe('themes pass down', () => {
  it('districts take hues from the palette, in order', () => {
    expect(hueOf(tree, ['a'])).toBe(HUES[0])
    expect(hueOf(tree, ['b'])).toBe(HUES[1])
  })
  it('a building is a shade of its district: the first keeps it, the next moves off it, never to another district', () => {
    expect(hueOf(tree, ['a', 'a1'])).toBe(HUES[0])
    expect(hueOf(tree, ['a', 'a2'])).not.toBe(HUES[0])
    expect(hueOf(tree, ['a', 'a2'])).not.toBe(HUES[1])
  })
  it('rooms are shades of their building', () => {
    expect(hueOf(tree, ['a', 'a1', 'r1'])).toBe(hueOf(tree, ['a', 'a1']))
    expect(hueOf(tree, ['a', 'a1', 'r2'])).not.toBe(hueOf(tree, ['a', 'a1']))
  })
})
