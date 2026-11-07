import '@testing-library/jest-dom/vitest'
import { render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { parseRuntimeHost, type RuntimeProgress } from '@/api/agentRuntime'
import { slotsPhrase } from '../slotsModel'
import { RuntimeHosts } from '../RuntimeHosts'

const moves = [
  { at: '2026-11-07T10:05:00Z', policy: 'aimd', old: 2, new: 1, reason: 'load per CPU 200% is above 100%: halved from 2 to 1' },
  { at: '2026-11-07T10:00:00Z', policy: 'aimd', old: 1, new: 2, reason: 'load per CPU 13% is below 70% with every slot busy: up from 1 to 2' },
]
const hosts = [
  parseRuntimeHost({ host_id: 'agent-06', state: 'active', online: true, poll_age_s: 3, last_poll_at: '2026-11-07T10:06:00Z',
    live_attempts: 1, effective_slots: 1, slot_moves: moves }),
  parseRuntimeHost({ host_id: 'agent-07', state: 'active', online: true, poll_age_s: 2, last_poll_at: '2026-11-07T10:06:00Z',
    live_attempts: 0, effective_slots: null, slot_moves: [] }),
  parseRuntimeHost({ host_id: 'old-platform', state: 'active', last_poll_at: '' }),
]

vi.mock('../../needs-you/useNeedsYou', () => ({
  useNeedsYou: () => ({ runtimeQuery: { isPending: false, isPaused: false, isError: false, dataUpdatedAt: 0, error: null,
    data: { generated_at: '2026-11-07T10:06:00Z', hosts } as unknown as RuntimeProgress } }),
}))

describe('elastic slots on the hosts panel', () => {
  it('reads a host\'s recorded count and moves, and leaves them unset from a platform before them', () => {
    expect(hosts[0]!.effective_slots).toBe(1)
    expect(hosts[0]!.slot_moves).toEqual(moves)
    expect(hosts[1]!.effective_slots).toBeNull()
    expect(hosts[2]!.effective_slots).toBeUndefined()
    expect(hosts[2]!.slot_moves).toBeUndefined()
    expect(slotsPhrase(hosts[0]!)).toBe('Slots: 1 (aimd)')
    expect(slotsPhrase(hosts[1]!)).toBe('Slots: per the concurrency rule (none recorded)')
    expect(slotsPhrase(hosts[2]!)).toBeNull()
  })
  it('shows the count, the policy and each move with its reason', () => {
    render(<RuntimeHosts />)
    const six = screen.getByText(/agent-06 · Online/).closest('li')!
    expect(six).toHaveTextContent('Slots: 1 (aimd)')
    expect(within(six).getByText('Slot moves (2)')).toBeInTheDocument()
    const rows = six.querySelectorAll('[data-slot-move]')
    expect([...rows].map(r => r.getAttribute('data-slot-move'))).toEqual(['2-1', '1-2'])
    expect(rows[0]).toHaveTextContent('2 → 1 (aimd): load per CPU 200% is above 100%: halved from 2 to 1')
    expect(screen.getByText(/agent-07 · Online/).closest('li')).toHaveTextContent('Slots: per the concurrency rule (none recorded)')
    expect(screen.getByText(/old-platform · Online/).closest('li')).not.toHaveTextContent('Slots')
  })
})
