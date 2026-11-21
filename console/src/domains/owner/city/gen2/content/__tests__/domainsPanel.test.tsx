// 2026-11-20: City became the map line's first reader (perceive-first brief, item 4). These cases pin
// that the city level carries the map's lights, that a failed read says "not known" rather than an empty
// table, and that districts keep their own panels (the Owner has not said how districts meet domains).
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
  it('lists each domain with its overall light and the four lights, with why on hover', () => {
    render(<>{bodyOf(domainsPanel({ data: status }))}</>)
    expect(screen.getByText('ops')).toBeTruthy()
    expect(screen.getByTitle('problem because').textContent).toBe('problem')
    expect(screen.getAllByText('bad').length).toBe(2)
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

  it('says the lights are not known when the read fails', () => {
    render(<>{bodyOf(domainsPanel({ error: new Error('401') }))}</>)
    expect(screen.getByText(/not known: 401/)).toBeTruthy()
    expect(screen.queryByRole('table')).toBeNull()
  })

  it('rides with the city level only; a district keeps its own panels', () => {
    const c = consoleContent([], [domainsPanel({ data: status })])
    const open = () => {}
    expect(c.panels(c.tree, open).some(p => p.id === 'domains')).toBe(true)
    const district = c.tree.children[0]
    expect(c.panels(district, open).some(p => p.id === 'domains')).toBe(false)
  })
})
