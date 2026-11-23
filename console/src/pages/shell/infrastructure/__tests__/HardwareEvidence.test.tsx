import '@testing-library/jest-dom/vitest'
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { LedgerGrid } from '@bifrost/ui/city/ledger'
import { readingView, type ObservationSnapshot } from '@bifrost/ui/city/observation'
import { HardwareEvidence, HardwareSnapshot } from '../HardwareEvidence'

const access = vi.hoisted(() => vi.fn())
vi.mock('@/domains/foundation/observation-access', () => ({ useHardwareObservation: access }))
afterEach(() => { cleanup(); access.mockReset() })
// A cell as it reads on screen: a word, or a word with a tone (2026-11-23: readingView cells say the seven words,
// "ok · online", as objects; comparing String(cell) would read "[object Object]").
const shown = (cell: unknown): string => typeof cell === 'object' && cell !== null
  ? ('lines' in cell ? (cell as { lines: unknown[] }).lines.map(shown).join('') : String((cell as { text: unknown }).text))
  : String(cell)
const at = '2026-11-22T20:00:00Z'
const snapshot: ObservationSnapshot = {
  version: 1, id: 'same-source-snapshot', capturedAt: at, provenance: 'fixture',
  content: { version: 2, tree: { id: 'town', kind: 'town', shape: '8x8', label: 'Hardware', question: '', dashboard: null,
    children: [{ id: 'host/one', kind: 'building', shape: '2x2', label: 'Host one', question: '', dashboard: null, state: 'online', children: [
      { id: 'cpu', kind: 'room', shape: '1x1', label: 'CPU', question: '', dashboard: null, meter: { used: 0, of: 8, unit: 'cores', source: 'source-query' }, children: [] },
    ] }],
  } },
  evidence: { 'host%2Fone/cpu': [{ source: '/api/v1/telemetry/promql?q=source-query', outcome: 'read', receivedAt: at, evaluatedAt: at, expiresAt: '2026-11-22T20:01:00Z' }] },
}

// Owner 2026-11-22: Console and Signals must preserve identity/value/unit/time/source from the same snapshot.
it('renders the shared projection verbatim and preserves zero and full object evidence', () => {
  render(<HardwareSnapshot snapshot={snapshot} at={at} />)
  const block = readingView.present(snapshot, { selected: [], metrics: true, at })[0].blocks[0]
  if (block.kind !== 'table') throw new Error('Expected shared readings table')
  const rows = within(screen.getAllByRole('table')[0]).getAllByRole('row').slice(1)
  expect(rows).toHaveLength(block.rows.length)
  block.rows.forEach((row, i) => row.cells.forEach((cell, j) => expect(within(rows[i]).getAllByRole('cell')[j].textContent).toBe(shown(cell))))
  fireEvent.click(screen.getByRole('button', { name: 'CPU' }))
  expect(screen.getByText('host%2Fone/cpu')).toBeInTheDocument()
  expect(screen.getByText('0 / 8 cores')).toBeInTheDocument()
  expect(screen.getByText('/api/v1/telemetry/promql?q=source-query')).toBeInTheDocument()
  expect(screen.getAllByText(at).length).toBeGreaterThan(0)
  expect(screen.getByText(/Simulated fixture/)).toBeInTheDocument()
})

// Same acceptance: stale, failed, absent and unknown-time readings cannot become healthy zeroes.
it.each([
  ['Stale', { ...snapshot.evidence['host%2Fone/cpu'][0], expiresAt: '2026-11-22T19:59:00Z' }],
  ['Read failed', { ...snapshot.evidence['host%2Fone/cpu'][0], outcome: 'error' as const }],
  ['Missing', { ...snapshot.evidence['host%2Fone/cpu'][0], outcome: 'missing' as const }],
  ['Unknown measurement time', { ...snapshot.evidence['host%2Fone/cpu'][0], evaluatedAt: undefined }],
])('retains shared quality %s', (quality, read) => {
  render(<HardwareSnapshot snapshot={{ ...snapshot, evidence: { 'host%2Fone/cpu': [read] } }} at={at} />)
  expect(screen.getAllByText(quality, { exact: false }).length).toBeGreaterThan(0)
})

// Owner 2026-11-22: opt-in observations must not add an always-on mailbox or workflow action.
it('mounts only the public hardware reader when opened, and preserves retained data on failure', () => {
  access.mockReturnValue({ data: snapshot, isPending: false, isError: true, error: new Error('offline') })
  render(<HardwareEvidence />)
  expect(access).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: 'Show hardware evidence' }))
  expect(access).toHaveBeenCalledTimes(1)
  expect(screen.getByText(/Hardware refresh failed:/)).toHaveTextContent('offline')
  expect(screen.getByText(/same-source-snapshot/)).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: /ACK|Approve|Accept/ })).not.toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Hide hardware evidence' }))
  expect(screen.queryByText(/same-source-snapshot/)).not.toBeInTheDocument()
})

// Console migration: filtering is local navigation and must retain the original snapshot evidence.
it('retains selected evidence when the shared grid filter hides its row without reading again', () => {
  render(<HardwareSnapshot snapshot={snapshot} at={at} />)
  fireEvent.click(screen.getByRole('button', { name: 'CPU' }))
  fireEvent.change(screen.getByLabelText('Filter supplied rows'), { target: { value: 'no matching object' } })
  expect(screen.getByText(/Selected object is outside/)).toHaveTextContent('host%2Fone/cpu')
  expect(screen.getByText('0 / 8 cores')).toBeInTheDocument()
  expect(screen.getByText('/api/v1/telemetry/promql?q=source-query')).toBeInTheDocument()
  expect(access).not.toHaveBeenCalled()
})

// Same snapshot/context must give identical standalone and Console table values, including uncertainty.
it.each(['read', 'missing', 'error'] as const)('matches independent Ledger cells for %s evidence', outcome => {
  const input = { ...snapshot, evidence: { 'host%2Fone/cpu': [{ ...snapshot.evidence['host%2Fone/cpu'][0], outcome }] } }
  const block = readingView.present(input, { selected: [], metrics: true, at })[0].blocks[0]
  if (block.kind !== 'table') throw new Error('Expected shared readings table')
  render(<><div data-testid="independent"><LedgerGrid table={block} selected="" onSelect={() => {}} scope="Same fixture" /></div><div data-testid="console"><HardwareSnapshot snapshot={input} at={at} /></div></>)
  const cells = (id: string) => within(screen.getByTestId(id)).getAllByRole('table')[0].textContent
  expect(cells('console')).toBe(cells('independent'))
})
