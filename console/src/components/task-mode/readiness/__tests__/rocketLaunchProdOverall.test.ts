import { describe, expect, it } from 'vitest'
import {
  isProdReleaseBlocked,
  rocketLaunchProdOverall,
} from '@/components/task-mode/readiness/utils'

describe('rocketLaunchProdOverall', () => {
  it('stays GO when namespaces and gate are healthy even if last deliver was CAUTION', () => {
    const live = rocketLaunchProdOverall({
      k8s: 'ok',
      selfHealth: 'ok',
      prodGate: 'ok',
    })
    expect(live).toBe('ok')
    expect(isProdReleaseBlocked(live)).toBe(false)
  })

  it('blocks on live namespace / self-health / prod gate', () => {
    expect(
      isProdReleaseBlocked(
        rocketLaunchProdOverall({ k8s: 'degraded', selfHealth: 'ok', prodGate: 'ok' }),
      ),
    ).toBe(true)
    expect(
      isProdReleaseBlocked(
        rocketLaunchProdOverall({ k8s: 'ok', selfHealth: 'fail', prodGate: 'ok' }),
      ),
    ).toBe(true)
    expect(
      isProdReleaseBlocked(
        rocketLaunchProdOverall({ k8s: 'ok', selfHealth: 'ok', prodGate: 'degraded' }),
      ),
    ).toBe(true)
  })
})
