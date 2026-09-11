import { describe, expect, it } from 'vitest'
import type { MissionSnapshot } from '@/lib/control-room/missionSignals'
import { resolveSidebarNavSignal, type SidebarNavProbeInput } from '@/lib/nav/sidebarNavSignal'

/**
 * 2026-09-08..09-10: every IB Flex ingest failed with `[1003] Statement is not
 * available`, the husbandry gate blocked the Research batch for three nights —
 * and the IB Flex nav icon stayed green the whole time, because it only asked
 * whether the plugin's HTTP port answered. Meanwhile the Massive icon beside it
 * went red on batch adherence. The two lamps must ask the same question.
 */
function baseInput(over: Partial<SidebarNavProbeInput> = {}): SidebarNavProbeInput {
  return {
    controlRoomBaySignal: 'ok',
    ibGateway: { isLoading: false, probeReach: 'ok', summary: 'ok' },
    marketQueue: { active: false, lamp: 'ok', verdict: 'idle', pending: 0, detail: '' },
    marketData: { isLoading: false, probeReach: 'ok', summary: 'ok' },
    flexQuery: {
      isLoading: false,
      probeReach: 'ok',
      summary: 'reachable',
      batch: { verdict: 'healthy', detail: 'source=secret · freshness ok', lamp: 'ok' },
    },
    researchEngine: { isLoading: false, probeReach: 'ok', summary: 'ok' },
    codeHealth: { isLoading: false, signal: 'ok', title: 'ok' },
    fleetLoading: false,
    snapshot: { missionOverall: 'ok' } as MissionSnapshot,
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

describe('IB Flex nav lamp reads its own husbandry lane', () => {
  it('goes red on a failing ingest even while the plugin is perfectly reachable', () => {
    const lamp = resolveSidebarNavSignal(
      'flex-query-manage',
      baseInput({
        flexQuery: {
          isLoading: false,
          probeReach: 'ok',
          summary: 'reachable',
          batch: {
            verdict: 'degraded',
            detail: 'flex-trades: not_ready: [1003] Statement is not available',
            lamp: 'fail',
          },
        },
      }),
    )
    expect(lamp?.signal).toBe('fail')
    expect(lamp?.title).toContain('[1003]')
  })

  it('stays green when the lane is healthy', () => {
    const lamp = resolveSidebarNavSignal('flex-query-manage', baseInput())
    expect(lamp?.signal).toBe('ok')
    expect(lamp?.title).toBe('IB Flex: reachable')
  })

  it('falls back to reachability when the lane has no opinion', () => {
    for (const verdict of [undefined, '', 'unknown']) {
      const lamp = resolveSidebarNavSignal(
        'flex-query-manage',
        baseInput({
          flexQuery: {
            isLoading: false,
            probeReach: 'fail',
            summary: 'proxy 502',
            batch: { verdict, detail: undefined, lamp: 'degraded' },
          },
        }),
      )
      expect(lamp?.signal).toBe('fail')
      expect(lamp?.title).toBe('IB Flex: proxy 502')
    }
  })
})
