import { describe, expect, it } from 'vitest'
import { rebootPendingLabel } from '@/components/cluster/rebootPending'

const SINCE = Date.parse('2026-09-11T15:04:00Z') / 1000

describe('rebootPendingLabel', () => {
  it('matches the node label and formats since-seconds as a date', () => {
    const required = [
      { labels: { node: 'k3s-a' }, value: 0 },
      { labels: { node: 'k3s-b' }, value: 1 },
    ]
    const since = [
      { labels: { node: 'k3s-a' }, value: SINCE },
      { labels: { node: 'k3s-b' }, value: SINCE },
    ]
    expect(rebootPendingLabel('k3s-b', required, since)).toBe('Reboot pending (since 2026-09-11)')
    expect(rebootPendingLabel('k3s-a', required, since)).toBeNull()
    expect(rebootPendingLabel('k3s-c', required, since)).toBeNull()
  })

  it('stays blank when a query result is missing', () => {
    const required = [{ labels: { node: 'k3s-b' }, value: 1 }]
    expect(rebootPendingLabel('k3s-b', null, required)).toBeNull()
    expect(rebootPendingLabel('k3s-b', required, null)).toBeNull()
    expect(rebootPendingLabel('k3s-b', required, [])).toBeNull()
  })
})