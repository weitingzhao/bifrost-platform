import type { Facility, Look } from '@bifrost/ui/city/places'

/**
 * The platform's binding (the Owner, 2026-11-20: the world offers the things; the platform only says which of
 * them shows each part of its state, the way a player picks a character): every kind of device it knows, the
 * catalogue's facility it stands as. No drawing here, only the choice; the frame draws what is picked.
 *
 * Each choice says why: what the device is for in this system, so its building reads that way.
 */
export function facilityFor(look: Look, gpu: boolean): Facility {
  if (look === 'network') return { kind: 'mast' }
  // A GPU box is a server hall, whatever runs on it.
  if (gpu) return { kind: 'computer', look: 'servers', system: look === 'mac' ? 'mac' : look === 'windows' ? 'win' : 'linux' }
  switch (look) {
    case 'linux': return { kind: 'computer', look: 'factory', system: 'linux' } // the cluster's nodes: the works
    case 'mac': return { kind: 'computer', look: 'studio', system: 'mac' } // the agents' machines: the studio
    case 'windows': return { kind: 'computer', look: 'tower', system: 'win' } // the trading hosts: the office tower
    case 'storage': return { kind: 'computer', look: 'warehouse', system: 'unknown' } // the NAS: the warehouse
  }
}

/** The town the hardware district stands as on the city: the nuclear plant (the Owner, 2026-11-20). */
export const HARDWARE_TOWN = 'nuclear'
