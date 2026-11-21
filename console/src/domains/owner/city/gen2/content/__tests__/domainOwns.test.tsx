// 2026-11-21: the perceive-first brief, item 4 — City goes into a district and reads what the domain owns from the
// map's domain read (GET /api/v1/catalog/domain). These pin that it reads only the district entered, shows what runs,
// tables, packages and the contract by kind, marks a shared row, and says "not known" on a failed read. The card is
// data the engine draws (CardView), as the engine draws it.
import { render, screen, waitFor } from '@testing-library/react'
import { CardView } from '@bifrost/ui/city/CardView'
import type { CardBody } from '@bifrost/ui/city/cards'
import { describe, expect, it, vi } from 'vitest'
import { consoleContent } from '../console'
import { ownsPanel, type DomainHoldings } from '../domainOwns'
import type { MapStatus } from '../domainsPanel'

const holdings: DomainHoldings = {
  id: 'owner', repo: 'bifrost-platform', commit: 'abcdef0123',
  packages: [{ path: 'pkg/approvals' }, { path: 'pkg/shared-one', shared: ['foundation'] }],
  groups: [], paths: [], workloads: [{ path: 'bifrost-owner-exec' }], tables: [],
  contract: { request_response: [1, 2, 3], events: [1], commands: [] },
}
const draw = (read: () => Promise<DomainHoldings>) => render(<CardView body={ownsPanel(() => read(), 'owner').cards as CardBody} />)

describe('what a domain owns', () => {
  it('shows what runs, its packages (a shared one marked) and its contract by kind', async () => {
    draw(() => Promise.resolve(holdings))
    await waitFor(() => expect(screen.getByText('bifrost-owner-exec')).toBeTruthy())
    expect(screen.getByText('pkg/approvals')).toBeTruthy()
    expect(screen.getByTitle('shared with foundation')).toBeTruthy()
    expect(screen.getByText('Routes').nextSibling?.textContent).toBe('3')
    expect(screen.getByText('Event streams').nextSibling?.textContent).toBe('not written')
  })
  it('says it is not known when the read fails', async () => {
    draw(() => Promise.reject(new Error('HTTP 401')))
    await waitFor(() => expect(screen.getByText(/Not known: HTTP 401/)).toBeTruthy())
  })
  it('a district carries the card only when a reader is given, and nothing is read until it is shown', () => {
    const read = vi.fn(() => Promise.resolve(holdings))
    const status = { version: 1, domains: [{ id: 'owner', overall: 'good' }] } as unknown as MapStatus
    const c = consoleContent({ data: status }, null, {}, read)
    const district = c.tree.children[0]
    expect(c.panels!(district, () => {}).map(p => p.id)).toEqual(['domain-owner', 'owns-owner'])
    expect(read).not.toHaveBeenCalled()
    expect(consoleContent({ data: status }).panels!(district, () => {}).map(p => p.id)).toEqual(['domain-owner'])
  })
})
