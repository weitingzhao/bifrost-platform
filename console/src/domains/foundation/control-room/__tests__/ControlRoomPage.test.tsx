import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ControlRoomPage } from '../ControlRoomPage'
import { ControlRoomPage as CompatibilityPage } from '@/pages/ControlRoomPage'

vi.mock('@/domains/foundation/mission/useMissionSnapshot', () => ({
  useMissionSnapshot: () => ({ snapshot: { missionOverall: 'ok' }, dataUpdatedAt: 0, staleSources: [], isLoading: false }),
}))
vi.mock('@/hooks/useNetworkLiveProbe', () => ({ useNetworkLiveProbe: () => ({ probeReach: 'unknown' }) }))
vi.mock('@/lib/control-room/controlRoomBays', () => ({
  buildControlRoomAttentionItems: () => [], buildControlRoomBaySignals: () => [],
  loadOpenControlRoomBayIds: () => [], nextOpenBayIds: () => new Set(),
  parseControlRoomBayHash: () => null, persistOpenControlRoomBayIds: vi.fn(),
  resolveInitialOpenBayIds: () => [], scrollToControlRoomBay: vi.fn(),
}))

afterEach(cleanup)
describe('Foundation control room page entry', () => {
  it('retains the exact component through the existing entry', () => {
    expect(CompatibilityPage).toBe(ControlRoomPage)
  })
  it('retains the matrix loading state', () => {
    render(<ControlRoomPage context={undefined} matrixLoading matrixError={null} />)
    expect(screen.getByText('Loading mission control…')).toBeTruthy()
  })
  it('retains matrix read failure rather than presenting healthy data', () => {
    render(<ControlRoomPage context={undefined} matrixLoading={false} matrixError={new Error('offline')} />)
    expect(screen.getByText('Failed to load matrix: offline')).toBeTruthy()
  })
})
