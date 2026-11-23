import { render } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

// Origin (2026-11-23, World's consistency-gap table, Ledger item 6): the Console's pages and the City's tech tree wore
// each domain's glyph, the City's table did not. A district row now wears its domain's glyph from the one table.
const district = (id: string, label: string) => ({ id, label, kind: 'district', shape: '8x8', question: '', dashboard: null, children: [] })
const tree = { id: 'city', label: 'City', kind: 'town', shape: '8x8', question: '', dashboard: null, children: [district('lineage', 'Work registration & lineage'), district('nowhere', 'Not a domain')] }
vi.mock('../useCityContent', () => ({ useCityContent: () => ({ tree }) }))

import CityLedger from '../CityLedger'

describe('the City table', () => {
  it("puts a district's domain glyph beside its name, and none for an id the table does not know", () => {
    window.location.hash = '#city?look=ledger'
    const { container } = render(<CityLedger />)
    const lineage = container.querySelector('[data-domain-name="lineage"]')!
    expect(lineage.textContent).toContain('Work registration & lineage')
    expect(lineage.querySelector('svg')).toBeTruthy()
    expect(container.querySelector('[data-domain-name="nowhere"]')).toBeNull()
  })
})
