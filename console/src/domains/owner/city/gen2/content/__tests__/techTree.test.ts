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

// 2026-11-21/22, the Owner: the tree follows the level; inside a district it is who among its buildings stands on
// whom. The buildings are the map's (catalog/domain buildings), the edges /ci/domains inner (production imports,
// aa0ca3cf). These pin the merge: packages to the map's buildings, a package of a building that did not fit to
// Other, two-way pairs one marked a loop, and no inner read: nodes alone with why. The hardware district has none.
describe('a district’s own tech tree', () => {
  const district = (id: string, children: string[] = []) => ({ id, kind: 'district' as const, shape: '4x4' as const, label: id, question: '', dashboard: null, children: children.map(c => ({ id: c, kind: 'building' as const, shape: '4x4' as const, label: c, question: '', dashboard: null, children: [] })) })
  const ids = new Set(['delivery', 'compute'])
  const holdings = { id: 'delivery', packages: [], groups: [], paths: [], workloads: [], buildings: [
    { id: 'releases', packages: ['pkg/releases', 'pkg/releases/x'] },
    { id: 'gitops', packages: ['pkg/gitops'] },
    { id: 'promote', packages: ['pkg/promote'] },
  ] }
  const graph = (pairs: { from: string; to: string }[]) => ({ version: 1, repo: 'r', commit: 'abcdef123456', measured: true, note: '', domains: [], edges: [], shared_packages: [], inner: [{ domain: 'delivery', imports: pairs.length, pairs }] })
  it('the imports inside the domain merged by building, counted', () => {
    const t = districtTechTree(district('delivery', ['releases', 'gitops']), ids, graph([
      { from: 'pkg/releases', to: 'pkg/gitops' }, { from: 'pkg/releases/x', to: 'pkg/gitops' },
      { from: 'pkg/releases/x', to: 'pkg/releases' },
    ]), holdings)!
    expect(t.edges).toEqual([{ from: 'releases', to: 'gitops', weight: 2, wrong: false }])
  })
  it('a package of a building that did not fit goes to Other', () => {
    const t = districtTechTree(district('delivery', ['releases', 'other']), ids, graph([{ from: 'pkg/promote', to: 'pkg/releases' }]), holdings)!
    expect(t.edges).toEqual([{ from: 'other', to: 'releases', weight: 1, wrong: false }])
  })
  it('two buildings that use each other: the lighter way is the loop', () => {
    const t = districtTechTree(district('delivery', ['releases', 'gitops']), ids, graph([
      { from: 'pkg/releases', to: 'pkg/gitops' }, { from: 'pkg/releases/x', to: 'pkg/gitops' },
      { from: 'pkg/gitops', to: 'pkg/releases' },
    ]), holdings)!
    expect(t.edges!.find(e => e.from === 'gitops')!.wrong).toBe(true)
    expect(t.edges!.find(e => e.from === 'releases')!.wrong).toBe(false)
  })
  it('no inner read: the buildings alone, saying why; no buildings: says so', () => {
    const t = districtTechTree(district('delivery', ['releases']), ids, undefined, holdings)!
    expect(t.edges).toBeUndefined()
    expect(t.note).toContain('not read')
    expect(districtTechTree(district('delivery'), ids)!.note).toContain('serves none')
  })
  it('the hardware district and anything not a domain have no tree', () => {
    expect(districtTechTree(district('compute', ['gpu-server']), ids)).toBeUndefined()
    expect(districtTechTree(district('elsewhere'), ids)).toBeUndefined()
  })
})
