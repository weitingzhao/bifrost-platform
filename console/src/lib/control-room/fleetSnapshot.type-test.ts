/**
 * Compile-time contract checks for Fleet Desk types.
 * Included in `tsc --noEmit` / `npm run type-check`.
 */
import type { FleetSnapshot, FleetVerdictKind } from './fleetSnapshot'
import { operateQueueClearLabel } from './fleetSnapshot'
import { buildFleetSnapshot } from './buildFleetSnapshot'

const snap = buildFleetSnapshot({
  viewerEnv: 'dev',
  matrices: [],
}) satisfies FleetSnapshot

const kind: FleetVerdictKind = snap.verdict.kind
void kind
void operateQueueClearLabel(0, snap.fleetClear)
