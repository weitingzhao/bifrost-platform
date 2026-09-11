import type { MissionSnapshot, Signal } from '@/lib/control-room/missionSignals'
import { missionStatus } from '@/lib/control-room/missionSignals'
import {
  resolveSatelliteRocketNavLamp,
  type MissionNavLamp,
} from '@/lib/control-room/missionNavSignals'
import type { LaunchDeskLaneId } from '@/hooks/useLaunchDeskChecklistSignals'

export type SidebarNavProbeInput = {
  controlRoomBaySignal: Signal
  ibGateway: { isLoading: boolean; probeReach: Signal; summary: string }
  marketQueue: { active: boolean; lamp: Signal; verdict: string; pending: number; detail: string }
  marketData: { isLoading: boolean; probeReach: Signal; summary: string }
  flexQuery: {
    isLoading: boolean
    probeReach: Signal
    summary: string
    /** `flex_batch` husbandry lane — did the day-end ingest land. */
    batch: { verdict: string | undefined; detail: string | undefined; lamp: Signal }
  }
  researchEngine: { isLoading: boolean; probeReach: Signal; summary: string }
  /** Planning lamp from codeHealthLens — not Observability fleet rollup. */
  codeHealth: { isLoading: boolean; signal: Signal; title: string }
  fleetLoading: boolean
  snapshot: MissionSnapshot
  busDeepLoading: boolean
  busNav: MissionNavLamp | null
  launchDeskSignals: Record<LaunchDeskLaneId, { signal: Signal; title: string }>
}

const RELEASE_LANES = new Set<string>([
  'platform-release',
  'trade-release',
  'research-release',
  'plugin-release',
  'agent-release',
  'satellite-launch',
])

/** Same lamp language as ConsoleSidebar item icons. */
export function resolveSidebarNavSignal(
  itemId: string,
  input: SidebarNavProbeInput,
): MissionNavLamp | null {
  if (itemId === 'control-room') {
    return {
      signal: input.controlRoomBaySignal,
      title: `Control Room Bay Scan: ${missionStatus(input.controlRoomBaySignal)}`,
    }
  }
  if (itemId === 'code-health') {
    // Planning slack is not fleet traffic-light. Only OVER paints the nav icon red;
    // AT CEILING / HELD stay muted (no green/yellow on the HeartPulse).
    if (input.codeHealth.isLoading) {
      return { signal: 'unknown', title: 'Code Health: loading…' }
    }
    const title = input.codeHealth.title
    if (input.codeHealth.signal === 'fail') {
      return { signal: 'fail', title }
    }
    return { signal: 'unknown', title }
  }
  if (itemId === 'ib-gateway-manage') {
    return {
      signal: input.ibGateway.isLoading ? 'unknown' : input.ibGateway.probeReach,
      title: `IB Client: ${input.ibGateway.summary}`,
    }
  }
  if (itemId === 'market-data-manage') {
    if (input.marketQueue.active) {
      return {
        signal: input.marketQueue.lamp,
        title: `Massive queue: ${input.marketQueue.verdict} · ${input.marketQueue.pending} ready · ${input.marketQueue.detail}`,
      }
    }
    return {
      signal: input.marketData.isLoading ? 'unknown' : input.marketData.probeReach,
      title: `Massive: ${input.marketData.summary}`,
    }
  }
  if (itemId === 'flex-query-manage') {
    // Same class of signal Massive's lamp carries above: the lane says whether
    // the day-end ingest landed. Reachability alone kept this icon green through
    // the 2026-09-08..09-10 `[1003]` outage, while the Massive icon next to it
    // went red on batch adherence — two lamps answering two different questions.
    const laneVerdict = input.flexQuery.batch.verdict
    const laneSpeaks = laneVerdict != null && laneVerdict !== '' && laneVerdict !== 'unknown'
    if (laneSpeaks && input.flexQuery.batch.lamp !== 'ok') {
      return {
        signal: input.flexQuery.batch.lamp,
        title: `IB Flex batch: ${laneVerdict} · ${input.flexQuery.batch.detail ?? input.flexQuery.summary}`,
      }
    }
    return {
      signal: input.flexQuery.isLoading ? 'unknown' : input.flexQuery.probeReach,
      title: `IB Flex: ${input.flexQuery.summary}`,
    }
  }
  if (itemId === 'research-engine') {
    return {
      signal: input.researchEngine.isLoading ? 'unknown' : input.researchEngine.probeReach,
      title: `Research Engine: ${input.researchEngine.summary}`,
    }
  }
  const vehicle = resolveSatelliteRocketNavLamp(itemId, {
    snapshotLoading: input.fleetLoading,
    snapshot: input.snapshot,
    busLoading: input.busDeepLoading,
    bus: input.busNav,
  })
  if (vehicle != null) return vehicle
  if (RELEASE_LANES.has(itemId)) {
    const laneId =
      itemId === 'satellite-launch' ? 'trade-release' : (itemId as LaunchDeskLaneId)
    const lane = input.launchDeskSignals[laneId]
    return { signal: lane.signal, title: lane.title }
  }
  return null
}
