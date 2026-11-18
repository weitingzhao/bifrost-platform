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
import '@/lib/control-room/missionSignals'

import '@/lib/satellite-bus/satelliteBusNavSignal'

/** True when any bus-deep env reports daemon ib_not_connected (D10 observe gap). */
export function resolveDaemonIbObserve(buses: SatelliteBusDeepResponse[]): boolean {
  return buses.some(b => b.monitor?.daemon?.block_reasons?.includes('ib_not_connected') === true)
}
