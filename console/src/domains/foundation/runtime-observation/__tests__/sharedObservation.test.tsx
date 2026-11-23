import { act, render, renderHook, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

// Origin (2026-11-23, World's consistency-gap table item 9): a Signals pin was lost on every change of place, because
// each place mounted its own observation context. The Console keeps one above its routes; Signals reads it there.
const readers = vi.fn(() => ({ snapshot: undefined, error: undefined }))
vi.mock('../useRuntimeObservationSnapshot', () => ({ useRuntimeObservationSnapshot: () => readers() }))
vi.mock('@/domains/foundation/shell/useShellStatusLine', () => ({ useShellStatusLine: () => ({ viewerEnvLoading: true }) }))

import { publishSharedObservation, useSharedObservation } from '../sharedObservationStore'
import RuntimeObservation from '../RuntimeObservation'

afterEach(() => { act(() => publishSharedObservation({ state: 'off' })); readers.mockClear() })

describe('shared observation', () => {
  it('hands the published context to whoever asks', () => {
    const { result } = renderHook(() => useSharedObservation())
    expect(result.current.state).toBe('off')
    act(() => publishSharedObservation({ state: 'reading' }))
    expect(result.current.state).toBe('reading')
  })

  it('a hosted Signals route waits for the shared context and never mounts readers of its own', () => {
    render(<RuntimeObservation hosted />)
    expect(screen.getByRole('status').textContent).toBe('Reading platform telemetry…')
    expect(readers).not.toHaveBeenCalled()
  })

  it('without the host (the preview pages) Signals still reads on its own', () => {
    render(<RuntimeObservation />)
    expect(readers).toHaveBeenCalled()
  })
})
