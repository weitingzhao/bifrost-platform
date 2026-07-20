/**
 * Shared Fleet Board / Daily Ops Checklist role visuals.
 * Keep icon + color + label identical across both surfaces for heartflow.
 */
import {
  HardHat,
  Rocket,
  Satellite,
  Server,
  Truck,
  type LucideIcon,
} from 'lucide-react'
import type { FleetRole } from '@/lib/control-room/fleetSnapshot'

export const FLEET_ROLE_LABEL: Record<FleetRole, string> = {
  rocket: 'Rocket',
  satellite: 'Satellite',
  engineer: 'Engineer',
  ground: 'Ground',
  vendor: 'Vendor',
}

export const FLEET_ROLE_ICON: Record<FleetRole, LucideIcon> = {
  rocket: Rocket,
  satellite: Satellite,
  engineer: HardHat,
  ground: Server,
  vendor: Truck,
}

export const FLEET_ROLE_COLOR: Record<FleetRole, string> = {
  rocket: 'text-sky-400',
  satellite: 'text-emerald-400',
  engineer: 'text-amber-400',
  ground: 'text-violet-400',
  vendor: 'text-rose-400',
}

/** Primary role for a checklist step (first fleetMapping entry). */
export function primaryFleetRole(
  mapping: Array<{ role: FleetRole }>,
): FleetRole {
  return mapping[0]?.role ?? 'ground'
}
