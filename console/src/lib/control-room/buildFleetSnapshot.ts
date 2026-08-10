/**
 * Single public entry for Fleet Desk snapshots: probe lattice + Checklist union.
 *
 * Kept in a thin module so `fleetSnapshot.ts` (core) and `dailyOpsChecklistInject.ts`
 * (union) stay free of circular imports. UI, hooks, and scripts should import
 * `buildFleetSnapshot` / `finalizeFleetSnapshot` from here.
 */
import {
  applyChecklistFleetUnion,
  type ChecklistSignalPaint,
} from '@/lib/control-room/dailyOpsChecklistInject'
import {
  buildFleetSnapshotCore,
  type BuildFleetSnapshotInput,
  type FleetSnapshot,
} from '@/lib/control-room/fleetSnapshot'

export type BuildFleetSnapshotWithChecklistInput = BuildFleetSnapshotInput & {
  checklistSignals?: ChecklistSignalPaint[]
}

/** Recompute verdict after injecting checklist virtual standards. */
export function finalizeFleetSnapshot(
  fleet: FleetSnapshot,
  checklistSignals?: ChecklistSignalPaint[],
): FleetSnapshot {
  return applyChecklistFleetUnion(fleet, checklistSignals)
}

/** Canonical Fleet snapshot builder — always includes Checklist↔Board union. */
export function buildFleetSnapshot(input: BuildFleetSnapshotWithChecklistInput): FleetSnapshot {
  return finalizeFleetSnapshot(buildFleetSnapshotCore(input), input.checklistSignals)
}

export type { BuildFleetSnapshotInput, FleetSnapshot }
