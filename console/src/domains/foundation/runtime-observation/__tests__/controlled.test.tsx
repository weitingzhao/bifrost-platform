import { Profiler, useState } from 'react'
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { RuntimeWorld, type ObservationSnapshot } from '@bifrost/ui/city/observation'
import RuntimeObservation from '../RuntimeObservation'
import { useObservationContext } from '@bifrost/ui/city/observationContext'

const makeSnapshot = (id: string): ObservationSnapshot => ({
  version: 1, id, provenance: 'live', capturedAt: '2026-11-22T12:00:00Z',
  evidence: {}, content: { tree: { id: 'town', kind: 'town', shape: '8x8', label: 'Test', question: 'State', dashboard: null, children: [] } },
  layerPanels: { domains: [{ id: 'domain:foundation', label: 'foundation', blocks: [{ kind: 'note', text: id }] }] },
})
afterEach(() => { cleanup(); vi.useRealTimers() })

it('retains the host pin, selection and metrics across scene unmount without a second clock', () => {
  vi.useFakeTimers()
  const rendered = vi.fn()
  function Host() {
    const [latest, update] = useState(makeSnapshot('snapshot-A'))
    const [visible, show] = useState(true)
    const observation = useObservationContext(latest)
    return <>
      <button onClick={() => update(makeSnapshot('snapshot-B'))}>Incoming B</button>
      <button onClick={() => show(v => !v)}>Change place</button>
      <button onClick={() => observation.select(['missing-device'])}>Host selection</button>
      <output aria-label="Host snapshot">{observation.snapshot.id}</output>
      <output aria-label="Host selection">{observation.context.selected.join('/')}</output>
      {visible && <Profiler id="scene" onRender={rendered}><RuntimeWorld observation={observation}/></Profiler>}
    </>
  }
  render(<Host/>)
  expect(vi.getTimerCount()).toBe(1)
  fireEvent.click(screen.getByRole('button', { name: 'Pin snapshot' }))
  expect(vi.getTimerCount()).toBe(0)
  const fixedRenders = rendered.mock.calls.length
  act(() => vi.advanceTimersByTime(30_000))
  expect(rendered).toHaveBeenCalledTimes(fixedRenders)
  fireEvent.click(screen.getByLabelText('Measurements'))
  fireEvent.click(screen.getByText('Host selection'))
  fireEvent.click(screen.getByText('Incoming B'))
  expect(screen.getByLabelText('Host snapshot').textContent).toBe('snapshot-A')
  fireEvent.click(screen.getByText('Change place'))
  fireEvent.click(screen.getByText('Change place'))
  expect(screen.getByLabelText('Host snapshot').textContent).toBe('snapshot-A')
  expect(screen.getByLabelText('Host selection').textContent).toBe('missing-device')
  expect((screen.getByLabelText('Measurements') as HTMLInputElement).checked).toBe(false)
  expect(vi.getTimerCount()).toBe(0)
  fireEvent.click(screen.getByRole('button', { name: 'Return to live', exact: true }))
  expect(screen.getByLabelText('Host snapshot').textContent).toBe('snapshot-B')
  expect(vi.getTimerCount()).toBe(1)
})

it('keeps legacy standalone mode and stops its display clock while pinned', () => {
  vi.useFakeTimers()
  render(<RuntimeWorld snapshot={makeSnapshot('standalone')}/>)
  expect(vi.getTimerCount()).toBe(1)
  fireEvent.click(screen.getByRole('button', { name: 'Pin snapshot' }))
  expect(vi.getTimerCount()).toBe(0)
  fireEvent.click(screen.getByRole('button', { name: 'Return to live', exact: true }))
  expect(vi.getTimerCount()).toBe(1)
})

it('controlled platform entry renders without a QueryClient or mounting its readers', () => {
  function Host() {
    const observation = useObservationContext(makeSnapshot('host-supplied'))
    return <RuntimeObservation observation={observation}/>
  }
  render(<Host/>)
  expect(screen.getByRole('heading', {name:'Signal village.'})).toBeTruthy()
  expect(screen.getByText('host-supplied')).toBeTruthy()
})

// 2026-11-23 (箭10, cockpit/ui-world/frame): Signals wears the one world frame. Its own kicker and place links give
// way; live/pinned sits in the frame's time chip; alerts not read say so, never 0.
it('wears the world frame: Signals current, pinned time in the chip, alerts not read', () => {
  function Host() {
    const observation = useObservationContext(makeSnapshot('snapshot-A'))
    return <RuntimeObservation observation={observation} frame={{ hrefs: { city: '#city', agents: '#collaboration', console: '#system' }, env: 'PROD' }}/>
  }
  render(<Host/>)
  expect(screen.getByRole('navigation', { name: 'Places' }).textContent).toBe('CitySignalsAgentsConsole')
  expect(screen.getByText('Signals', { selector: '[aria-current="page"]' })).toBeTruthy()
  expect(screen.queryByText('BIFROST / SIGNALS')).toBeNull()
  expect(screen.getByText('PROD')).toBeTruthy()
  expect(screen.getByRole('banner', { name: 'Where you are' }).textContent).toMatch(/Alerts.not read/)
  fireEvent.click(screen.getByRole('button', { name: 'Pin snapshot' }))
  expect(screen.getByRole('banner', { name: 'Where you are' }).textContent).toContain('Pinned')
})
