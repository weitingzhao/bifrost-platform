/**
 * Compile-time contract checks for Fleet Desk types.
 * Included in `tsc --noEmit` / `npm run type-check`.
 */
import type { FleetSnapshot, FleetVerdictKind } from './fleetSnapshot'
import { buildFleetSnapshot, operateQueueClearLabel } from './fleetSnapshot'
import { lookupFleetFixRoute, pickFleetFixCell } from './fleetCellFix'

const snap = buildFleetSnapshot({
  viewerEnv: 'dev',
  matrices: [],
}) satisfies FleetSnapshot

const kind: FleetVerdictKind = snap.verdict.kind
void kind
void operateQueueClearLabel(0, snap.fleetClear)
void lookupFleetFixRoute('engineer', 'span')
void pickFleetFixCell(snap)
