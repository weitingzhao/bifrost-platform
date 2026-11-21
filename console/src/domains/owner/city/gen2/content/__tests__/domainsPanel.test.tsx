// 2026-11-20: City became the map line's first reader (perceive-first brief, item 4). These cases pin
// that the city level carries what the map counts, that a failed read says "not known" rather than an
// empty table, and that each domain district carries its own lights.
import { render, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { describe, expect, it } from 'vitest'
import { consoleContent } from '../console'
import { domainsPanel } from '../domainsPanel'

const light = (value: string, why = `${value} because`) => ({ value, why })
const status = {
  version: 1, at: '2026-11-20T12:00:00Z', commit: 'abcdef0123',
  domains: [{ id: 'ops', overall: 'bad', alive: light('good'), interfaces: light('problem'), boundary: light('bad'), fresh: light('unknown') }],
}
const bodyOf = (p: ReturnType<typeof domainsPanel>) => (typeof p.body === 'function' ? p.body({ size: 'card' } as never) : p.body) as ReactNode

describe('domains panel', () => {
  // 2026-11-20: the lights moved onto the tech tree's nodes (techTree.test.ts); the town's panel keeps the counts.
  it('lists each domain by name without its lights', () => {
    render(<>{bodyOf(domainsPanel({ data: status }))}</>)
    expect(screen.getByText('ops')).toBeTruthy()
    expect(screen.queryByTitle('problem because')).toBeNull()
  })

  it('shows the off-map pool by domain and the equation per repository, a gap marked', () => {
    const data = { ...status, off_map_by_domain: { ops: 4, unclaimed: 7 }, inventory: [
      { repo: 'bifrost-platform', measured: true, total: 10, on_map: 8, off_map: 1, gap: 1 },
      { repo: 'bifrost-research', measured: true, whole: true, total: 1, on_map: 1, off_map: 0, gap: 0 },
    ] }
    render(<>{bodyOf(domainsPanel({ data }))}</>)
    expect(screen.getByText('4')).toBeTruthy()
    expect(screen.getByText('7')).toBeTruthy()
    expect(screen.getAllByText('1').some(td => td.className.includes('g2-warn'))).toBe(true)
    expect(screen.getByText(/inside not seen/)).toBeTruthy()
  })

  // 2026-11-21: the map line serves who uses whom (status.references); City reads it out, a loop and reaching past the contract marked.
  it('lists who uses whom, a loop first, reaching past the contract marked', () => {
    const data = { ...status, domains: [...status.domains, { ...status.domains[0], id: 'plan' }], references: [
      { from: 'plan', to: 'ops', imports: 5, registered: 5, unregistered: 0, wrong_way: false },
      { from: 'ops', to: 'plan', imports: 2, registered: 0, unregistered: 2, wrong_way: true },
    ] }
    render(<>{bodyOf(domainsPanel({ data }))}</>)
    const cells = screen.getAllByText(/→/).map(td => td.textContent).filter(t => t !== 'Uses → used')
    expect(cells).toEqual(['ops → plan · loop', 'plan → ops'])
    expect(screen.getByTitle('0 through its contract, 2 not (by package)').className).toContain('g2-warn')
  })
  it('says when the map does not serve references yet', () => {
    render(<>{bodyOf(domainsPanel({ data: status }))}</>)
    expect(screen.getByText(/not served by the map yet/)).toBeTruthy()
  })

  it('says the lights are not known when the read fails', () => {
    render(<>{bodyOf(domainsPanel({ error: new Error('401') }))}</>)
    expect(screen.getByText(/not known: 401/)).toBeTruthy()
    expect(screen.queryByRole('table')).toBeNull()
  })

  it('rides with the city level; a domain district carries its own lights', () => {
    const c = consoleContent({ data: status })
    const open = () => {}
    expect(c.panels(c.tree, open).some(p => p.id === 'domains')).toBe(true)
    const district = c.tree.children[0]
    expect(c.panels(district, open).map(p => p.id)).toEqual(['domain-ops'])
  })
})
