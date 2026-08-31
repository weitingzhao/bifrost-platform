import { describe, expect, it } from 'vitest'
import type { MissionSnapshot } from '@/lib/control-room/missionSignals'
import { resolveSidebarNavSignal, type SidebarNavProbeInput } from '@/lib/nav/sidebarNavSignal'

function baseInput(over: Partial<SidebarNavProbeInput> = {}): SidebarNavProbeInput {
  const snapshot = { missionOverall: 'ok' } as MissionSnapshot
  return {
    controlRoomBaySignal: 'ok',
    ibGateway: { isLoading: false, probeReach: 'ok', summary: 'ok' },
    marketQueue: { active: false, lamp: 'ok', verdict: 'idle', pending: 0, detail: '' },
    marketData: { isLoading: false, probeReach: 'ok', summary: 'ok' },
    flexQuery: { isLoading: false, probeReach: 'ok', summary: 'ok' },
    researchEngine: { isLoading: false, probeReach: 'ok', summary: 'ok' },
    codeHealth: {
      isLoading: false,
      signal: 'degraded',
      title: 'Code Health: 7 at ceiling · min slack 0',
    },
    fleetLoading: false,
    snapshot,
    busDeepLoading: false,
    busNav: null,
    launchDeskSignals: {
      'platform-release': { signal: 'ok', title: 'ok' },
      'trade-release': { signal: 'ok', title: 'ok' },
      'research-release': { signal: 'ok', title: 'ok' },
      'plugin-release': { signal: 'ok', title: 'ok' },
      'agent-release': { signal: 'ok', title: 'ok' },
    },
    ...over,
  }
}

describe('resolveSidebarNavSignal code-health', () => {
  it('keeps planning title but does not paint yellow for AT CEILING', () => {
    const lamp = resolveSidebarNavSignal('code-health', baseInput())
    expect(lamp?.signal).toBe('unknown')
    expect(lamp?.title).toContain('at ceiling')
  })

  it('does not paint green for HELD', () => {
    const lamp = resolveSidebarNavSignal(
      'code-health',
      baseInput({
        codeHealth: {
          isLoading: false,
          signal: 'ok',
          title: 'Code Health: HELD · min slack 2',
        },
      }),
    )
    expect(lamp?.signal).toBe('unknown')
    expect(lamp?.title).toContain('HELD')
  })

  it('is unknown while loading', () => {
    const lamp = resolveSidebarNavSignal(
      'code-health',
      baseInput({
        codeHealth: { isLoading: true, signal: 'ok', title: 'ignored while loading' },
      }),
    )
    expect(lamp?.signal).toBe('unknown')
  })

  it('maps OVER planning lamp to fail (red)', () => {
    const lamp = resolveSidebarNavSignal(
      'code-health',
      baseInput({
        codeHealth: {
          isLoading: false,
          signal: 'fail',
          title: 'Code Health: 1 over baseline — CI blocks',
        },
      }),
    )
    expect(lamp?.signal).toBe('fail')
  })
})
