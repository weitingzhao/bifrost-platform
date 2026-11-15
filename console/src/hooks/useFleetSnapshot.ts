import 'react'
import '@tanstack/react-query'
import '@/api/checklist'
import '@/api/cluster'
import '@/api/delivery'
import '@/api/promote'
import '@/api/core'
import type { SatelliteBusDeepResponse } from '@/api/satelliteBusTypes'
import '@/api/agentOps'
import '@/api/network'

import '@/lib/control-room/buildFleetSnapshot'
import { type FleetViewerEnv } from '@/lib/control-room/fleetSnapshot'
import '@/lib/control-room/missionSignals'

import '@/lib/satellite-bus/satelliteBusNavSignal'

/**
 * Mac seat readiness for Engineer row (not a board column).
 * Prod/STG viewers cannot reach Mac 127.0.0.1 — only mark ready when bridge reports ok
 * AND viewer is on a Mac-adjacent seat (dev / dev-local).
 */
export function resolveGroundBridgeReady(
  viewerEnv: FleetViewerEnv,
  bridgeStatus: string | undefined,
): boolean {
  if (viewerEnv === 'prod' || viewerEnv === 'stg') return false
  return bridgeStatus === 'ok'
}

/** True when any bus-deep env reports daemon ib_not_connected (D10 observe gap). */
export function resolveDaemonIbObserve(buses: SatelliteBusDeepResponse[]): boolean {
  return buses.some(b => b.monitor?.daemon?.block_reasons?.includes('ib_not_connected') === true)
}
