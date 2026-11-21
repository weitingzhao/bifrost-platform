import { describe, expect, it } from 'vitest'
import type { CiDomainGraph } from '@/api/ciDomains'
import { consoleContent } from '../console'
import type { MapStatus } from '../domainsPanel'
import { domainTechTree } from '../techTree'

// 2026-11-20, the Owner: the domains' tech tree is configured from the real graph. These pin that its
// nodes are the districts with their map lights, its edges the imports GET /api/v1/ci/domains serves
// (a wrong-way edge kept as a loop), that it says what it does not see, and that an unread graph leaves
// the nodes alone with why rather than a guessed edge.
const light = (value: string) => ({ value, why: '' })
const row = (id: string, overall = 'good') => ({ id, overall, alive: light('good'), interfaces: light('problem'), boundary: light('good'), fresh: light('unknown') })
const status = { version: 1, domains: [row('foundation'), row('owner', 'problem'), row('coordination')] } as MapStatus
const graph: CiDomainGraph = {
  version: 1, repo: 'bifrost-platform', commit: '9d9fd3e2aaaa', measured: true, note: '', base: 'foundation', domains: [],
  edges: [
    { from: 'owner', to: 'foundation', imports: 36, wrong_way: false },
    { from: 'owner', to: 'coordination', imports: 2, wrong_way: false },
    { from: 'coordination', to: 'owner', imports: 1, wrong_way: true },
    { from: 'shared', to: 'foundation', imports: 9, wrong_way: false },
  ],
  shared_packages: ['a-shared-package'],
}

describe('the domains tech tree', () => {
  it('nodes are the districts, each with its overall light and its four lights said', () => {
    const t = domainTechTree(status, { data: graph })!
    expect(t.nodes.map(n => [n.id, n.tone])).toEqual([['foundation', 'good'], ['owner', 'problem'], ['coordination', 'good']])
    expect(t.nodes[0].why).toBe('alive good · interfaces problem · boundary good · fresh unknown')
    expect(t.nodes.map(n => n.icon)).toEqual(['layers', 'gavel', 'signal'])
  })
  it('edges are the served imports between districts; a wrong-way one is a loop; others dropped', () => {
    const t = domainTechTree(status, { data: graph })!
    expect(t.edges).toEqual([
      { from: 'owner', to: 'foundation', weight: 36, wrong: false },
      { from: 'owner', to: 'coordination', weight: 2, wrong: false },
      { from: 'coordination', to: 'owner', weight: 1, wrong: true },
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
