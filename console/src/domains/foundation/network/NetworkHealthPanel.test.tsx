import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { OpsContextResponse } from '@/api/opsContextTypes'
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
  // 2026-11-17 OWNER_LAW #30 (audit rows 21-23): stream progress comes only from the spine; without it the page says unknown instead of a hand-written 6/6.
  it('shows spine network streams, and unknown without a fallback when the spine is not loaded', () => {
    render(<NetworkHealthPanel context={undefined} />)
    expect(screen.queryByText('Stream')).toBeNull()
    expect(screen.getByText('Streams unknown — spine not loaded')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Plugin → Network' })).toBeNull()
    cleanup()
    const context = { tracks: { infra: { label: 'Infra', streams: [
      { id: 'network-upgrade-core', label: 'Core', done: 2, total: 6, status: 'in_progress' },
      { id: 'k3s-other', label: 'Other', done: 1, total: 1, status: 'closed' },
    ] } } } as unknown as OpsContextResponse
    render(<NetworkHealthPanel context={context} />)
    expect(screen.getByText('Streams from spine')).toBeTruthy()
    expect(screen.getByText('network-upgrade-core')).toBeTruthy()
    expect(screen.getByText('2/6')).toBeTruthy()
    expect(screen.queryByText('k3s-other')).toBeNull()
  })
})
