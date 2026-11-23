// 2026-11-23, gap table item 9 (cockpit/ui-world/consistency-gaps-2026-11-23.md): a Signals pin was lost on every
// change of place; Console now keeps it in the shared context above the routes (4cde0abd2). Every frame says the same
// sentence when a pin is held, and nothing when Signals was never opened or is live.
import { act, cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { publishSharedObservation } from '@/domains/foundation/runtime-observation/sharedObservationStore'
import { SignalsPinNote } from '../SignalsPinNote'

const ready = (pinned: boolean) => ({ state: 'ready', observation: { pinned, snapshot: { capturedAt: '2026-11-23T21:15:00Z' } } }) as never
afterEach(() => { cleanup(); act(() => publishSharedObservation({ state: 'off' })) })

describe('the Signals pin note', () => {
  it('says the snapshot is retained, with its capture time, when Signals holds a pin', () => {
    render(<SignalsPinNote />)
    act(() => publishSharedObservation(ready(true)))
    const note = screen.getByText(/Signals snapshot retained/).closest('a')!
    expect(note.getAttribute('href')).toBe('#observation')
    expect(note.getAttribute('title')).toContain('2026-11-23T21:15:00Z')
  })

  it('says nothing when Signals is live, reading, or never opened', () => {
    const { container } = render(<SignalsPinNote />)
    expect(container.textContent).toBe('')
    act(() => publishSharedObservation(ready(false)))
    expect(container.textContent).toBe('')
    act(() => publishSharedObservation({ state: 'reading' }))
    expect(container.textContent).toBe('')
  })
})
