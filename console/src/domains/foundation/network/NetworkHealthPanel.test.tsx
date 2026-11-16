import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { NetworkHealthPanel } from './NetworkHealthPanel'

const probe = vi.hoisted(() => ({ probeReach: 'unknown', isLoading: false, summary: 'Network probe unread', audit: null, status: null }))
vi.mock('@/hooks/useNetworkLiveProbe', () => ({ useNetworkLiveProbe: () => probe }))
afterEach(cleanup)
// 2026-11-16 domain extraction: only the display owner moves; network probes and policy evaluation remain original.
describe('Foundation network display', () => {
  it('keeps a read-only probe visible without upgrade history and preserves navigation', () => {
    const open = vi.fn()
    render(<NetworkHealthPanel context={undefined} showUpgradeRecord={false} onOpenNetwork={open} />)
    expect(screen.getByText('Network probe unread')).toBeTruthy()
    expect(screen.getByText('Live probe UNKNOWN')).toBeTruthy()
    expect(screen.getByText('GET /api/v1/network/status')).toBeTruthy()
    expect(screen.queryByText('Stream')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Plugin → Network' }))
    expect(open).toHaveBeenCalledOnce()
  })
  it('retains optional catalog history without a network action callback', () => {
    render(<NetworkHealthPanel context={undefined} />)
    expect(screen.getByText('Stream')).toBeTruthy()
    expect(screen.getByText('· Projection catalog fallback')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Plugin → Network' })).toBeNull()
  })
})
