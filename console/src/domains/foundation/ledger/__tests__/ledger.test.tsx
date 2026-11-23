import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { LedgerGrid, LedgerEvidence } from '@bifrost/ui/city/ledger'
import { readingView, type ObservationSnapshot } from '@bifrost/ui/city/observation'
import type { Block } from '@bifrost/ui/city/cards'
import { HardwareLedger } from '../../../../../fixtures/ledger/HardwareLedger'
import { qualityFixture } from '../../../../../fixtures/observation/qualityFixture'

afterEach(() => { cleanup(); vi.useRealTimers() })
// A cell as it reads on screen: a word, or a word with a tone (2026-11-23: readingView cells say the seven words,
// "ok · online", as objects; comparing String(cell) would read "[object Object]").
const shown = (cell: unknown): string => typeof cell === 'object' && cell !== null
  ? ('lines' in cell ? (cell as { lines: unknown[] }).lines.map(shown).join('') : String((cell as { text: unknown }).text))
  : String(cell)
const tableOf = (snapshot: ObservationSnapshot) => readingView.present(snapshot, { selected: [], at: snapshot.capturedAt, metrics: true })[0].blocks[0] as Extract<Block, {kind:'table'}>
// 2026-11-22 Ledger task: separate surfaces must preserve original evidence, zero and uncertainty.
describe('Ledger public snapshot consumption', () => {
  it.each([['zero', 'Within refresh window'], ['missing', 'Missing'], ['failed', 'Read failed'], ['stale', 'Stale'], ['unknown-time', 'Unknown measurement time']])('matches the grid and embedded evidence for %s', (id, quality) => {
    const snapshot = qualityFixture(), table = tableOf(snapshot), key = id + '/cpu'
    const row = table.rows.find(row => row.key === key)!
    render(<><LedgerGrid table={table} selected={key} onSelect={vi.fn()} scope="Simulated hardware" /><LedgerEvidence snapshot={snapshot} context={{selected:[id,'cpu'],at:snapshot.capturedAt,metrics:true}} /></>)
    const detail = within(screen.getByRole('region', {name:'Ledger object evidence'}))
    const grid = within(screen.getByRole('region', {name:'Ledger grid'}))
    const actual = grid.getAllByRole('row').find(element => element.getAttribute('title') === key)!
    expect(within(actual).getAllByRole('cell').map(cell => cell.textContent)).toEqual(row.cells.map(shown))
    for (const value of row.cells) expect(detail.getAllByRole('cell').some(cell => cell.textContent === shown(value))).toBe(true)
    expect(detail.getAllByText(quality, { exact: false }).length).toBeGreaterThan(0)
    expect(detail.getByText(snapshot.id)).toBeTruthy()
    if(id==='zero') expect(detail.getByText('0 / 8 cores')).toBeTruthy()
    expect(detail.getAllByText(snapshot.evidence[key][0].receivedAt).length).toBeGreaterThan(0)
  })
  // 2026-11-22 Ledger task: filtering/paging may hide selection, but must not change workflow state.
  it('bounds 5000 rows, sorts/filters locally and retains selection', () => {
    const onSelect = vi.fn(), fetchSpy = vi.spyOn(globalThis,'fetch')
    const table: Extract<Block,{kind:'table'}> = {kind:'table',head:[{label:'Object'},{label:'Count'}],rows:Array.from({length:5000},(_,i)=>({key:`row-${i}`,open:`row-${i}`,cells:[`Device ${i}`,i]}))}
    render(<LedgerGrid table={table} selected="row-4999" onSelect={onSelect} scope="Simulated supplied rows" />)
    expect(screen.getAllByRole('row')).toHaveLength(51)
    expect(screen.getByText(/Selection retained/)).toBeTruthy()
    fireEvent.click(screen.getByText('Next page'))
    expect(screen.getByText('Device 50')).toBeTruthy()
    fireEvent.change(screen.getByLabelText('Sort column'),{target:{value:'1'}})
    fireEvent.click(screen.getByText('Descending'))
    expect(screen.getAllByRole('row')[1].textContent).toBe('Device 49994999')
    fireEvent.change(screen.getByLabelText('Filter supplied rows'),{target:{value:'row-4999'}})
    expect(screen.getAllByRole('row')).toHaveLength(2)
    fireEvent.click(screen.getByText('Device 4999'))
    expect(onSelect).toHaveBeenCalledWith('row-4999')
    expect(fetchSpy).not.toHaveBeenCalled(); fetchSpy.mockRestore()
  })
  // 2026-11-22 Ledger task: empty input must expose its range rather than imply healthy data.
  it('reports an empty supplied range with no enabled paging', () => {
    render(<LedgerGrid table={{kind:'table',head:[{label:'Object'}],rows:[]}} selected="" onSelect={vi.fn()} scope="Empty fixture" />)
    expect(screen.getByRole('status').textContent).toContain('0–0 of 0')
    expect((screen.getByText('Next page') as HTMLButtonElement).disabled).toBe(true)
    expect((screen.getByText('Previous page') as HTMLButtonElement).disabled).toBe(true)
  })
  // 2026-11-22 Ledger task: an absent object is not a new root selection or invented historical row.
  it('keeps an absent selection explicit and displays the supplied time', () => {
    const snapshot = qualityFixture()
    render(<LedgerEvidence snapshot={snapshot} context={{selected:['gone'],metrics:true,at:'2000-01-01T00:00:00Z'}} />)
    expect(screen.getByText(/not represented/)).toBeTruthy()
    expect(screen.getByText('Selected: gone')).toBeTruthy()
    expect(screen.getByText('2000-01-01T00:00:00Z')).toBeTruthy()
  })
})

// 2026-11-22 chief follow-up: use the engine context across carriers; incoming data cannot rewrite fixed evidence.
it('keeps one engine snapshot, time and selection across carriers and resumes the latest input', () => {
  vi.useFakeTimers()
  const first = qualityFixture()
  first.id = 'first-snapshot'
  const next = structuredClone(first)
  next.id = 'next-snapshot'
  next.content.tree.children[0].children[0].meter!.used = 4
  const { rerender } = render(<HardwareLedger latest={first} />)
  fireEvent.change(screen.getByLabelText('Filter supplied rows'), {target:{value:'zero/cpu'}})
  fireEvent.click(within(screen.getByRole('region',{name:'Ledger grid'})).getByRole('button',{name:'CPU'}))
  fireEvent.click(screen.getByRole('button',{name:'Fix shared snapshot'}))
  const detail = () => within(screen.getByRole('region',{name:'Ledger object evidence'}))
  const fixed = screen.getByRole('region',{name:'Ledger object evidence'}).textContent
  rerender(<HardwareLedger latest={next} />)
  fireEvent.click(screen.getByRole('button',{name:'Embed beside details'}))
  expect(screen.getByRole('region',{name:'Ledger object evidence'}).textContent).toBe(fixed)
  expect(detail().getByText('first-snapshot')).toBeTruthy()
  expect((screen.getByLabelText('Filter supplied rows') as HTMLInputElement).value).toBe('zero/cpu')
  expect(screen.getByText('Selected: zero/cpu')).toBeTruthy()
  fireEvent.click(screen.getByRole('button',{name:'Resume latest snapshot'}))
  expect(detail().getByText('next-snapshot')).toBeTruthy()
  expect(detail().getByText('4 / 8 cores')).toBeTruthy()
  expect(screen.getByText('Selected: zero/cpu')).toBeTruthy()
})
