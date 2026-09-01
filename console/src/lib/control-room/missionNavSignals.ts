import { missionStatus, type MissionSnapshot, type Signal } from '@/lib/control-room/missionSignals'

export type MissionNavLamp = { signal: Signal; title: string }

/**
 * Sidebar tint for Satellite / Rocket vehicle items.
 * Same color language as Research Engine / Plugin live probes (icon, not trailing lamp).
 */
export function resolveSatelliteRocketNavLamp(
  itemId: string,
  input: {
    snapshotLoading: boolean
    snapshot: MissionSnapshot
    busLoading: boolean
    bus: MissionNavLamp | null
  },
): MissionNavLamp | null {
  if (itemId === 'cluster') {
    if (input.snapshotLoading) return { signal: 'unknown', title: 'Cluster: probing' }
    return { signal: input.snapshot.infra.signal, title: input.snapshot.infra.detail }
  }
  if (itemId === 'rocket-health') {
    if (input.snapshotLoading) return { signal: 'unknown', title: 'Rocket Health: probing' }
    return { signal: input.snapshot.control.signal, title: input.snapshot.control.detail }
  }
  if (itemId === 'satellite-health') {
    if (input.snapshotLoading) return { signal: 'unknown', title: 'Satellite Health: probing' }
    const s = input.snapshot
    return {
      signal: s.payloadOverall,
      title:
        `Satellite Health: ${missionStatus(s.payloadOverall)}` +
        ` · DEV ${s.tradeDev.value} · STG ${s.tradeStg.value} · PROD ${s.tradeProd.value}`,
    }
  }
  if (itemId === 'satellite-bus') {
    if (input.busLoading && input.bus == null) {
      return { signal: 'unknown', title: 'Bus Status: probing' }
    }
    return input.bus ?? { signal: 'unknown', title: 'Bus Status: unprobed' }
  }
  return null
}
