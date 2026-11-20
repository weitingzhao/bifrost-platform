import { describe, expect, it } from 'vitest'
import { cityHref, consoleHrefOf, ledgerHref, readCityQuery } from '../looks'
import type { Place } from '../../frame/places'

// Origin: the Owner, 2026-11-19 ("按这个画"): the city and the Console are two peer looks; switching keeps
// the place and the picked room. These pin that an address carries both and reads back the same.
const room = (id: string): Place => ({ id, kind: 'room', shape: '2x2', label: id, question: '', dashboard: null, children: [] })

describe('the looks keep the place and the pick', () => {
  it('a city address reads back the same place and room', () => {
    const q = readCityQuery(cityHref(['utilities', 'ubt-k3s-04'], 'disk').split('?')[1])
    expect(q).toEqual({ look: 'city', path: ['utilities', 'ubt-k3s-04'], sel: 'disk' })
  })

  it('a ledger address reads back as the ledger at the same place', () => {
    const q = readCityQuery(ledgerHref(['utilities', 'ubt-k3s-04'], 'memory').split('?')[1])
    expect(q).toEqual({ look: 'ledger', path: ['utilities', 'ubt-k3s-04'], sel: 'memory' })
  })

  it('the town has no at, and an empty query is the city at the town', () => {
    expect(cityHref([])).toBe('#city')
    expect(readCityQuery('')).toEqual({ look: 'city', path: [], sel: null })
  })

  it('the Console look is the place’s own page when it has one, else its ledger with the pick', () => {
    expect(consoleHrefOf({ ...room('releases'), kind: 'building', dashboard: '#releases' }, ['work', 'releases'], null)).toBe('#releases')
    expect(consoleHrefOf({ ...room('ubt'), kind: 'building' }, ['utilities', 'ubt'], room('disk'))).toBe('#city?look=ledger&at=utilities/ubt&sel=disk')
  })
})
