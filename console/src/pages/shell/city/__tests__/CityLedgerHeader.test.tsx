import { render } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

// Origin (2026-11-23, World's consistency-gap table item 3): on #city?look=ledger the Console's frame said Console,
// and nothing said the rows were the City's. The frame's crumbs are now the City's own place names, ending in Table.
const tree = { id: 'city', label: 'City', children: [{ id: 'utilities', label: 'Utilities', children: [] }] }
vi.mock('../useCityContent', () => ({ useCityContent: () => ({ tree }) }))
const seen: Array<{ label: string }[]> = []
vi.mock('@/components/ConsoleHeader', () => ({ ConsoleHeader: (p: { crumbs: { label: string }[] }) => { seen.push(p.crumbs); return null } }))

import CityLedgerHeader from '../CityLedgerHeader'

describe('CityLedgerHeader', () => {
  it("names the City's table by the map's place names", () => {
    window.location.hash = '#city?look=ledger&at=utilities'
    render(<CityLedgerHeader {...({} as never)} />)
    expect(seen.at(-1)?.map(c => c.label)).toEqual(['City', 'Utilities', 'Table'])
  })
})
