import { describe, expect, it } from 'vitest'
import {
  buildControlRoomAttentionItems,
  buildControlRoomBaySignals,
  collapseOpenBayIdsForSingleMode,
  controlRoomBayCountsLabel,
  controlRoomBayDomId,
  controlRoomBayHash,
  controlRoomVerdictLabel,
  formatControlRoomFreshness,
  loadControlRoomExpandMode,
  nextOpenBayIds,
  parseControlRoomBayHash,
  persistOpenControlRoomBayIds,
  resolveInitialOpenBayIds,
  saveControlRoomExpandMode,
  type ControlRoomBayId,
} from '@/lib/control-room/controlRoomBays'
import type { MissionSnapshot } from '@/lib/control-room/missionSignals'

function snap(partial: Partial<MissionSnapshot> = {}): MissionSnapshot {
  const ok = { signal: 'ok' as const, value: 'ok', detail: 'ok' }
  return {
    infra: ok,
    release: ok,
    control: ok,
    agent: ok,
    tradeDev: ok,
    tradeStg: ok,
    tradeProd: ok,
    rocketOverall: 'ok',
    payloadOverall: 'ok',
    missionOverall: 'ok',
    ...partial,
  }
}

function memoryStorage(initial: Record<string, string> = {}): Storage {
  const map = new Map(Object.entries(initial))
  return {
    get length() {
      return map.size
    },
    clear: () => map.clear(),
    getItem: (k: string) => map.get(k) ?? null,
    setItem: (k: string, v: string) => {
      map.set(k, v)
    },
    removeItem: (k: string) => {
      map.delete(k)
    },
    key: (i: number) => [...map.keys()][i] ?? null,
  }
}

describe('controlRoomBays', () => {
  it('maps bay ids to stable DOM / hash anchors', () => {
    expect(controlRoomBayDomId('mission')).toBe('cr-mission')
    expect(controlRoomBayHash('launch')).toBe('#cr-launch')
    expect(parseControlRoomBayHash('#cr-operate')).toBe('operate')
    expect(parseControlRoomBayHash('#control-room')).toBeNull()
  })

  it('builds ≤6 bay signals with live mission lamps', () => {
    const bays = buildControlRoomBaySignals({
      snapshot: snap({
        missionOverall: 'fail',
        release: { signal: 'degraded', value: 'stg', detail: 'stg caution' },
        payloadOverall: 'ok',
        agent: { signal: 'ok', value: 'ok', detail: 'ok' },
      }),
      operateOpenCount: 2,
      pendingBriefCount: 1,
      activeAgentJobCount: 0,
      networkProbe: 'ok',
      promoteLamp: 'degraded',
      showHealth: true,
    })
    expect(bays.map(b => b.id)).toEqual([
      'mission',
      'launch',
      'operate',
      'release',
      'health',
      'governance',
    ])
    expect(bays.find(b => b.id === 'mission')?.signal).toBe('fail')
    expect(bays.find(b => b.id === 'launch')?.signal).toBe('degraded')
    expect(bays.find(b => b.id === 'operate')?.signal).toBe('degraded')
    expect(bays.find(b => b.id === 'operate')?.reason).toContain('handoff')
    expect(bays.find(b => b.id === 'health')?.signal).toBe('ok')
  })

  it('does not map narrative-ready promoteLamp (unknown) to Release PROBING', () => {
    const bays = buildControlRoomBaySignals({
      snapshot: snap({
        release: { signal: 'ok', value: 'shipped', detail: 'Last deliver: Succeeded' },
      }),
      promoteLamp: 'unknown',
      showHealth: false,
    })
    const release = bays.find(b => b.id === 'release')
    expect(release?.signal).toBe('ok')
    expect(release?.reason).toBe('Narrative ready · awaiting live cutover')
    expect(controlRoomVerdictLabel(release!.signal)).toBe('NOMINAL')
    expect(controlRoomBayCountsLabel(bays)).not.toMatch(/probing/)
  })

  it('keeps deliver caution when promote is narrative-ready', () => {
    const bays = buildControlRoomBaySignals({
      snapshot: snap({
        release: { signal: 'degraded', value: 'shipping', detail: 'Deliver running' },
      }),
      promoteLamp: 'unknown',
      showHealth: false,
    })
    const release = bays.find(b => b.id === 'release')
    expect(release?.signal).toBe('degraded')
    expect(release?.reason).toContain('narrative ready')
  })

  it('omits Health bay when showHealth is false', () => {
    const bays = buildControlRoomBaySignals({ snapshot: snap(), showHealth: false })
    expect(bays.map(b => b.id)).toEqual([
      'mission',
      'launch',
      'operate',
      'release',
      'governance',
    ])
  })

  it('persists expand mode defaulting to single', () => {
    const storage = memoryStorage()
    expect(loadControlRoomExpandMode(storage)).toBe('single')
    saveControlRoomExpandMode('multi', storage)
    expect(loadControlRoomExpandMode(storage)).toBe('multi')
    saveControlRoomExpandMode('single', storage)
    expect(loadControlRoomExpandMode(storage)).toBe('single')
  })

  it('resolves initial open set: single keeps one; multi keeps many', () => {
    const stored: ControlRoomBayId[] = ['mission', 'launch', 'operate']
    expect(
      resolveInitialOpenBayIds({ mode: 'single', preferredId: 'release', storedOpen: stored }),
    ).toEqual(['release'])
    expect(
      resolveInitialOpenBayIds({ mode: 'single', preferredId: null, storedOpen: stored }),
    ).toEqual(['mission'])
    expect(
      resolveInitialOpenBayIds({ mode: 'multi', preferredId: 'health', storedOpen: stored }),
    ).toEqual(['mission', 'launch', 'operate', 'health'])
  })

  it('nextOpenBayIds respects single accordion vs multi', () => {
    const cur = new Set<ControlRoomBayId>(['mission', 'launch'])
    expect([...nextOpenBayIds('single', cur, 'operate', true)]).toEqual(['operate'])
    expect([...nextOpenBayIds('single', cur, 'mission', false)]).toEqual([])
    expect([...nextOpenBayIds('multi', cur, 'operate', true)].sort()).toEqual([
      'launch',
      'mission',
      'operate',
    ])
    expect([...nextOpenBayIds('multi', cur, 'mission', false)]).toEqual(['launch'])
  })

  it('collapseOpenBayIdsForSingleMode prefers activeBay', () => {
    const cur = new Set<ControlRoomBayId>(['mission', 'launch'])
    expect([...collapseOpenBayIdsForSingleMode(cur, 'launch')]).toEqual(['launch'])
    expect([...collapseOpenBayIdsForSingleMode(cur, null)]).toEqual(['mission'])
  })

  it('persistOpenControlRoomBayIds writes per-bay keys', () => {
    const storage = memoryStorage()
    persistOpenControlRoomBayIds(['launch', 'operate'], storage)
    expect(storage.getItem('bifrost_control_room_bay_launch_open')).toBe('true')
    expect(storage.getItem('bifrost_control_room_bay_operate_open')).toBe('true')
    expect(storage.getItem('bifrost_control_room_bay_mission_open')).toBe('false')
  })

  it('formats freshness and verdict labels', () => {
    expect(controlRoomVerdictLabel('ok')).toBe('NOMINAL')
    expect(controlRoomVerdictLabel('degraded')).toBe('CAUTION')
    expect(controlRoomVerdictLabel('fail')).toBe('CRITICAL')
    expect(formatControlRoomFreshness(Date.now() - 5_000, Date.now())).toBe('5s ago')
    expect(formatControlRoomFreshness(Date.now() - 120_000, Date.now())).toBe('2m ago')
  })

  it('builds attention items for non-ok bays and counts label', () => {
    const bays = buildControlRoomBaySignals({
      snapshot: snap({
        missionOverall: 'degraded',
        release: { signal: 'fail', value: 'fail', detail: 'blocked' },
        payloadOverall: 'ok',
      }),
      operateOpenCount: 1,
      networkProbe: 'ok',
      showHealth: true,
    })
    const attention = buildControlRoomAttentionItems(bays)
    expect(attention.some(a => a.bayId === 'mission')).toBe(true)
    expect(attention[0]?.severity).toBe('critical')
    expect(controlRoomBayCountsLabel(bays)).toMatch(/critical|caution|clear/)
  })
})
