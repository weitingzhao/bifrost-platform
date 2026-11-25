import { describe, expect, it } from 'vitest'
import { fitsBudget, sidesOf, TAPPABLE } from '@bifrost/ui/city/grid'
import { rulesBroken } from '@bifrost/ui/city/rules'
import type { Place } from '@bifrost/ui/city/places'
import { consoleContent } from '../console'
import { consoleFunctions } from '../consolePlaces'
import { COMPUTE, domainConsoleHref, domainTree, styleOf } from '../domainPlaces'
import { consoleHrefOf } from '../looks'
import type { MapStatus } from '../domainsPanel'

// (ISSUE-9) The 2.5D shows the system's domain design; the menu is another dimension. These pin that the districts are the domains the map serves, in its order and never a list
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
  // 2026-11-21: audit claimed measuring-office, so the unclaimed case uses an id no line can claim.
  it('the claimed kind is its style; none claimed, no style', () => {
    const t = domainTree({ version: 1, domains: [{ id: 'delivery', overall: 'good' }, { id: 'no-such-domain', overall: '' }] } as unknown as MapStatus, null)
    const [delivery, unclaimed] = t.children
    expect(delivery.style).toBe(styleOf('delivery'))
    expect(delivery.style).toBeTruthy()
    expect(unclaimed.style).toBeUndefined()
  })
  it('read when the map serves a light, a ghost when it does not', () => {
    const t = domainTree({ version: 1, domains: [{ id: 'delivery', overall: 'problem' }, { id: 'plan', overall: 'unknown' }] } as unknown as MapStatus, null)
    expect(t.children.map(c => c.state)).toEqual(['online', 'unread'])
  })
})

// 2026-11-23, gap table item 8 (cockpit/ui-world/consistency-gaps-2026-11-23.md): from a district the frame's Console
// cell opened the City's own table, not the domain in the Console. It now opens the domain's row in Status → Domains.
describe('a district opens its domain in the Console', () => {
  it('links the district to its row in Status → Domains', () => {
    const t = domainTree({ version: 1, at: '', commit: '', domains: [{ id: 'lineage', overall: 'good' }] } as never, null)
    const d = t.children.find(c => c.id === 'lineage')!
    expect(d.dashboard).toBe('#status?domain=lineage')
    expect(consoleHrefOf(d, ['lineage'], null)).toBe('#status?domain=lineage')
    expect(domainConsoleHref('a b')).toBe('#status?domain=a%20b')
  })
})

// 2026-11-24, PROD: while a district's buildings were still being read (20–50 s), the scene said "Nothing inside yet"
// and the card "Inside 0", which read as no buildings. The engine's Place.inside (0.60.0) says it instead.
describe('a district says when its buildings are still being read', () => {
  it("marks a district reading until its read is back, or why it failed, and nothing once it is back", () => {
    const status = { version: 1, at: '', commit: '', domains: [{ id: 'plan', overall: 'good' }, { id: 'lineage', overall: 'good' }, { id: 'audit', overall: 'good' }] } as never
    const holdings = { lineage: { id: 'lineage', packages: [], groups: [], paths: [], workloads: [], buildings: [{ id: 'lineage', packages: ['p/lineage'] }] } }
    const t = domainTree(status, null, '', holdings, undefined, '', { plan: 'reading', audit: { why: 'HTTP 502' }, lineage: 'reading' })
    const by = (id: string) => t.children.find(c => c.id === id)!
    expect(by('plan').inside).toBe('reading')
    expect(by('audit').inside).toEqual({ why: 'HTTP 502' })
    expect(by('lineage').inside).toBeUndefined()
    expect(rulesBroken({ tree: t })).toEqual([])
  })
})
