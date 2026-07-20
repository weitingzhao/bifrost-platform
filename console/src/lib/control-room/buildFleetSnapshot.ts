/**
 * Single public entry for Fleet Desk snapshots: probe lattice + Checklist union.
 *
 * Kept in a thin module so `fleetSnapshot.ts` (core) and `dailyOpsChecklistInject.ts`
 * (union) stay free of circular imports. UI, hooks, and scripts should import
 * `buildFleetSnapshot` / `finalizeFleetSnapshot` from here.
 */
import {
  applyChecklistFleetUnion,
} from '@/lib/control-room/dailyOpsChecklistInject'
import {
  buildFleetSnapshotCore,
  type BuildFleetSnapshotInput,
  type FleetSnapshot,
} from '@/lib/control-room/fleetSnapshot'

/** Recompute verdict after injecting checklist virtual standards. */
export function finalizeFleetSnapshot(fleet: FleetSnapshot): FleetSnapshot {
  return applyChecklistFleetUnion(fleet)
}

/** Canonical Fleet snapshot builder — always includes Checklist↔Board union. */
export function buildFleetSnapshot(input: BuildFleetSnapshotInput): FleetSnapshot {
  return finalizeFleetSnapshot(buildFleetSnapshotCore(input))
}

export type { BuildFleetSnapshotInput, FleetSnapshot }
