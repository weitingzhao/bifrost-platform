import { describe, expect, it } from 'vitest'
import type { CiDomainGraph } from '@/api/ciDomains'
import { consoleContent } from '../console'
import type { MapStatus } from '../domainsPanel'
import { districtTechTree, domainTechTree } from '../techTree'

// 2026-11-20, the Owner: the domains' tech tree is configured from the real graph. These pin that its
// nodes are the districts with their map lights, its edges the imports GET /api/v1/ci/domains serves
// (a wrong-way edge kept as a loop), that it says what it does not see, and that an unread graph leaves
// the nodes alone with why rather than a guessed edge.
// The approvals domain was owner until law 66 (2026-11-21): the ids follow the map.
const light = (value: string) => ({ value, why: '' })
const row = (id: string, overall = 'good') => ({ id, overall, alive: light('good'), interfaces: light('problem'), boundary: light('good'), fresh: light('unknown') })
const status = { version: 1, domains: [row('foundation'), row('approvals', 'problem'), row('coordination')] } as MapStatus
const graph: CiDomainGraph = {
  version: 1, repo: 'bifrost-platform', commit: '9d9fd3e2aaaa', measured: true, note: '', base: 'foundation', domains: [],
  edges: [
    { from: 'approvals', to: 'foundation', imports: 36, wrong_way: false },
    { from: 'approvals', to: 'coordination', imports: 2, wrong_way: false },
    { from: 'coordination', to: 'approvals', imports: 1, wrong_way: true },
    { from: 'shared', to: 'foundation', imports: 9, wrong_way: false },
  ],
  shared_packages: ['a-shared-package'],
}

describe('the domains tech tree', () => {
  it('nodes are the districts, each with its overall light and its four lights said', () => {
    const t = domainTechTree(status, { data: graph })!
    expect(t.nodes.map(n => [n.id, n.tone])).toEqual([['foundation', 'good'], ['approvals', 'problem'], ['coordination', 'good']])
    expect(t.nodes[0].why).toBe('alive good · interfaces problem · boundary good · fresh unknown')
    expect(t.nodes.map(n => n.icon)).toEqual(['layers', 'gavel', 'signal'])
  })
  it('edges are the served imports between districts; a wrong-way one is a loop; others dropped', () => {
    const t = domainTechTree(status, { data: graph })!
    expect(t.edges).toEqual([
      { from: 'approvals', to: 'foundation', weight: 36, wrong: false },
      { from: 'approvals', to: 'coordination', weight: 2, wrong: false },
      { from: 'coordination', to: 'approvals', weight: 1, wrong: true },
    ])
    expect(t.note).toMatch(/bifrost-platform at 9d9fd3e2.*Not seen here.*1 shared package not drawn/)
  })
  it('an unread or unmeasured graph: the nodes alone, with why', () => {
    const unread = domainTechTree(status, { error: new Error('HTTP 401') })!
    expect(unread.edges).toBeUndefined()
    expect(unread.note).toMatch(/not known: HTTP 401/)
    expect(domainTechTree(status, { data: { ...graph, measured: false, note: 'no table' } })!.note).toMatch(/not measured: no table/)
  })
  it('no tree while the domains themselves are not read', () => {
    expect(domainTechTree(undefined, { data: graph })).toBeUndefined()
  })
  it('the content hands the frame its tree', () => {
    expect(consoleContent({ data: status }, null, { data: graph }).techTree?.nodes).toHaveLength(3)
  })
})

// 2026-11-21, the Owner: the tree follows the level; inside a district it is who among its buildings stands on
// whom. That day the map's groups were empty and /ci/domains served no imports inside a domain: the district's
// tree says so rather than guess, and the hardware district (machines do not stand on one another) has none.
describe('a district’s own tech tree', () => {
  const district = (id: string, children: string[] = []) => ({ id, kind: 'district' as const, shape: '4x4' as const, label: id, question: '', dashboard: null, children: children.map(c => ({ id: c, kind: 'building' as const, shape: '4x4' as const, label: c, question: '', dashboard: null, children: [] })) })
  const ids = new Set(['delivery', 'compute'])
  it('no buildings yet: an empty tree that says why, no edge', () => {
    const t = districtTechTree(district('delivery'), ids)!
    expect(t.nodes).toEqual([])
    expect(t.edges).toBeUndefined()
    expect(t.note).toContain('groups are empty')
  })
  it('its buildings as nodes, the edges still not measured', () => {
    const t = districtTechTree(district('delivery', ['release', 'window']), ids)!
    expect(t.nodes.map(n => n.id)).toEqual(['release', 'window'])
    expect(t.note).toContain('not measured')
  })
  it('the hardware district and anything not a domain have no tree', () => {
    expect(districtTechTree(district('compute', ['gpu-server']), ids)).toBeUndefined()
    expect(districtTechTree(district('elsewhere'), ids)).toBeUndefined()
  })
})
