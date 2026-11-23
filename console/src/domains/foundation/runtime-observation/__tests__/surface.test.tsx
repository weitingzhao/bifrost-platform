import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { ObservationSurface, RuntimeWorld, readQuality, type ObservationSnapshot, type ReadEvidence } from '@bifrost/ui/city/observation'
import { snapshotOf } from '../read'

afterEach(cleanup)
const at = '2026-11-22T12:00:00.000Z'
const read: ReadEvidence = { source: '/fixture', outcome: 'read', receivedAt: at, evaluatedAt: at, expiresAt: '2026-11-22T12:01:00.000Z' }
const room = { id: 'cpu', kind: 'room' as const, shape: '4x4' as const, label: 'CPU', question: 'CPU usage', dashboard: null, state: 'online' as const, meter: { used: 0, of: 8, unit: 'cores' as const, source: '/fixture' }, children: [] }
const building = { id: 'node-a', kind: 'building' as const, shape: '4x4' as const, label: 'Node A', question: 'State', dashboard: null, state: 'online' as const, children: [room] }
const fixture: ObservationSnapshot = { version: 1, id: 'fixture-1', capturedAt: at, provenance: 'fixture', content: { tree: { ...building, id: 'town', kind: 'town', children: [building] }, alerts: { source: '/alerts', alerts: [] } }, evidence: { 'node-a/cpu': [read] } }

// 2026-11-22 Owner multi-view acceptance: zero/missing/error/stale must remain distinct, not green guesses.
describe('observation contract', () => {
  it('keeps source quality states distinct', () => {
    expect(readQuality([read], at)).toBe('Within refresh window')
    expect(readQuality([{ ...read, outcome: 'missing' }], at)).toBe('Missing')
    expect(readQuality([{ ...read, outcome: 'error' }], at)).toBe('Read failed')
    expect(readQuality([read], '2026-11-22T12:02:00Z')).toBe('Stale')
    expect(readQuality([{ ...read, evaluatedAt: undefined }], at)).toBe('Unknown measurement time')
    expect(readQuality([{ ...read, expiresAt: undefined }], at)).toBe('Freshness unknown')
    expect(readQuality([], at)).toBe('Unknown provenance')
  })
  it('preserves identity, zero and pinned snapshot while live input changes and views/layers toggle', () => {
    const { rerender } = render(<ObservationSurface snapshot={fixture} initialSelection={['node-a', 'cpu']} />)
    fireEvent.click(screen.getByText('Pin this snapshot'))
    const inspector = screen.getByLabelText('Selected object evidence')
    expect(within(inspector).getByText('0% 0.0 of 8.0 cores')).toBeTruthy()
    const newer = structuredClone(fixture)
    newer.id = 'fixture-2'; newer.content.tree.children[0].children[0].meter!.used = 4
    rerender(<ObservationSurface snapshot={newer} initialSelection={['node-a', 'cpu']} />)
    fireEvent.click(screen.getByText('Status field'))
    fireEvent.click(screen.getByRole('checkbox', { name: 'Reported alerts' }))
    fireEvent.click(screen.getByLabelText('Measurements'))
    fireEvent.change(screen.getByLabelText('Skin'), { target: { value: 'night' } })
    expect(within(inspector).getByText('node-a/cpu')).toBeTruthy()
    expect(within(inspector).getByText('fixture-1')).toBeTruthy()
    expect(within(inspector).getByText('0% 0.0 of 8.0 cores')).toBeTruthy()
    fireEvent.click(screen.getByLabelText('Measurements'))
    fireEvent.click(screen.getByRole('checkbox', { name: 'Reported alerts' }))
    fireEvent.click(screen.getByText('Return to live context'))
    expect(within(inspector).getByText('fixture-2')).toBeTruthy()
    expect(within(inspector).getByText('50% 4.0 of 8.0 cores')).toBeTruthy()
  })
  // Same date/scope: views unable to represent a selection must explain it, not substitute another identity.
  it('retains absent selections and explains them', () => {
    render(<ObservationSurface snapshot={fixture} initialSelection={['missing', 'cpu']} />)
    fireEvent.click(screen.getByText('Status field'))
    expect(screen.getByText('Selected object is not represented in this snapshot. Selection retained.')).toBeTruthy()
  })
  it('renders a supplied communication projection without any transport or implied completion', () => {
    const s = { ...fixture, layerPanels: { communication: [{ id: 'letter', label: 'Communication evidence', blocks: [{ kind: 'facts' as const, rows: [{ label: 'ACK', value: 'ledger:event-2' }, { label: 'Completed', value: 'No evidence' }] }] }] } }
    render(<ObservationSurface snapshot={s} layers={[{ id: 'communication', label: 'Communication', present: x => x.layerPanels?.communication ?? [] }]} />)
    for (let i = 0; i < 3; i++) { fireEvent.click(screen.getByRole('checkbox', { name: 'Communication' })); expect(screen.getAllByText('ledger:event-2')).toHaveLength(1); fireEvent.click(screen.getByRole('checkbox', { name: 'Communication' })) }
    expect(s.layerPanels.communication[0].blocks[0].rows[1].value).toBe('No evidence')
  })
  // 2026-11-22 readback found successful empty queries and failed reads are different evidence.
  it('keeps a topology read failure explicit', async () => {
    const s = await snapshotOf({ '/api/v1/topology?env=prod': { error: 'HTTP 503', receivedAt: at } }, 'fixture')
    expect(s.content.tree.children[0].children).toEqual([])
    expect(s.content.alerts?.alerts).toBeUndefined()
  })
})

// 2026-11-22 Runtime scene: test the public renderer contract, without a GPU dependency.
vi.mock('phaser', () => ({ default: { Scene: class {}, Game: class { loop = { wake() {}, sleep() {} }; destroy() {} }, AUTO: 0, Scale: { RESIZE: 0 } } }))
describe('runtime world', () => {
  it('retains real object evidence while layers change and a newer snapshot arrives', () => {
    const { rerender } = render(<RuntimeWorld snapshot={fixture} links={[{label:'City',href:'#city'}]} />)
    fireEvent.change(screen.getByLabelText('Inspect device'), { target: { value: 'node-a' } })
    fireEvent.click(screen.getByRole('button', { name: /CPU/ }))
    fireEvent.click(screen.getByText('Pin snapshot'))
    const next = structuredClone(fixture)
    next.id = 'fixture-2'; next.capturedAt = '2026-11-22T12:00:30.000Z'
    next.content.tree.children[0].children[0].meter!.used = 4
    rerender(<RuntimeWorld snapshot={next} />)
    fireEvent.click(screen.getByLabelText('Measurements'))
    fireEvent.click(screen.getByLabelText('Alert markers'))
    const evidence = screen.getByLabelText('Runtime evidence')
    expect(within(evidence).getByText(/raw 0 \/ 8/)).toBeTruthy()
    expect(within(evidence).getByText('node-a/cpu')).toBeTruthy()
    expect(screen.getByText(/Latest incoming: 2026-11-22T12:00:30/)).toBeTruthy()
    fireEvent.click(screen.getByText('Return to live'))
    expect(within(evidence).getByText(/raw 4 \/ 8/)).toBeTruthy()
    expect(fixture.content.tree.children[0].children[0].meter!.used).toBe(0)
  })
})
