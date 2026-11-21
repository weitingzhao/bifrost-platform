import { describe, expect, it } from 'vitest'
import { fitsBudget, sidesOf, TAPPABLE } from '@bifrost/ui/city/grid'
import { rulesBroken } from '@bifrost/ui/city/rules'
import type { Place } from '@bifrost/ui/city/places'
import { consoleContent } from '../console'
import { consoleFunctions } from '../consolePlaces'
import { COMPUTE, domainTree, styleOf } from '../domainPlaces'
import type { MapStatus } from '../domainsPanel'

// 2026-11-20, the Owner: "the 2.5D is there to show the system's domain design; the menu is another
// dimension". These pin that the districts are the domains the map serves, in its order and never a list
// kept here; that the hardware stands in compute's district; that an unread map says so and keeps only
// what was read itself; and that the menu stays as the functions.
const light = { value: 'good', why: '' }
const row = (id: string, name?: string) => ({ id, name, overall: 'good', alive: light, interfaces: light, boundary: light, fresh: light })
const ids = ['foundation', 'plan', 'lineage', 'verification', 'delivery', 'owner', 'compute', 'tenant', 'coordination']
const status = { version: 1, domains: ids.map(id => row(id)) } as MapStatus
const hardware: Place = { id: 'utilities', kind: 'district', shape: '4x4', style: 'nuclear', label: 'Utilities', question: 'hw', dashboard: null, function: 'system', children: [] }

describe('the districts are the domains', () => {
  it('one district per domain the map serves, in its order', () => {
    expect(domainTree(status, null).children.map(d => d.id)).toEqual(ids)
  })
  it('names a district by the name the map serves, else its id', () => {
    const t = domainTree({ ...status, domains: [row('plan', 'Demand & planning'), row('owner')] } as MapStatus, null)
    expect(t.children.map(d => d.label)).toEqual(['Demand & planning', 'owner'])
  })
  it('the hardware stands in compute, keeping its town and its function', () => {
    const c = domainTree(status, hardware).children.find(d => d.id === COMPUTE)!
    expect([c.style, c.function, c.question]).toEqual(['nuclear', 'system', 'hw'])
    expect(domainTree(status, hardware).children).toHaveLength(ids.length)
  })
  it('an unread map says so; only the hardware, read on its own, stands', () => {
    const t = domainTree(undefined, hardware, 'HTTP 401')
    expect(t.children.map(d => d.id)).toEqual(['utilities'])
    expect(t.question).toMatch(/not known: HTTP 401/)
  })
  it('keeps the frame’s rules: within the ground, every door a finger wide', () => {
    const c = consoleContent({ data: status }, hardware)
    expect(rulesBroken(c)).toEqual([])
    expect(fitsBudget(c.tree.children)).toBe(true)
    for (const d of c.tree.children) expect(sidesOf(d.shape).short).toBeGreaterThanOrEqual(TAPPABLE)
  })
  it('the menu along the bottom stays the Console’s functions', () => {
    expect(consoleContent({ data: status }).functions).toEqual(consoleFunctions())
  })
})

// 2026-11-21, the Owner: every district stands as a picture of what it is, from the engine's catalogue, and
// the lines claim theirs (the Console's ownership table, facilities). These pin that a claimed kind becomes
// the district's style, an unclaimed one stays plain, and a district is solid only when the map serves its lights.
describe('a district stands as the building its line claimed', () => {
  it('the claimed kind is its style; none claimed, no style', () => {
    const t = domainTree({ version: 1, domains: [{ id: 'delivery', overall: 'good' }, { id: 'audit', overall: '' }] } as unknown as MapStatus, null)
    const [delivery, audit] = t.children
    expect(delivery.style).toBe(styleOf('delivery'))
    expect(delivery.style).toBeTruthy()
    expect(audit.style).toBeUndefined()
  })
  it('read when the map serves a light, a ghost when it does not', () => {
    const t = domainTree({ version: 1, domains: [{ id: 'delivery', overall: 'problem' }, { id: 'plan', overall: 'unknown' }] } as unknown as MapStatus, null)
    expect(t.children.map(c => c.state)).toEqual(['online', 'unread'])
  })
})
