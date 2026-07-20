import { useMemo, useSyncExternalStore } from 'react'
import {
  buildChecklistCoverageIndex,
  getChecklistTouchStoreEpoch,
  subscribeChecklistTouchStore,
  type ChecklistCoverageIndex,
} from '@/lib/control-room/dailyOpsChecklistCoverage'
import type { FleetSnapshot } from '@/lib/control-room/fleetSnapshot'

/**
 * Dry-run coverage index: maps every Fleet Board standard to its checklist owner.
 * Recomputes on fleet poll and when a real run touch is recorded.
 */
export function useDailyOpsChecklistCoverage(
  fleet: FleetSnapshot | null | undefined,
): ChecklistCoverageIndex | null {
  const touchEpoch = useSyncExternalStore(
    subscribeChecklistTouchStore,
    getChecklistTouchStoreEpoch,
    () => 0,
  )

  return useMemo(() => {
    if (fleet == null) return null
    return buildChecklistCoverageIndex(fleet)
  }, [fleet, fleet?.cells, fleet?.verdict.kind, fleet?.fleetClear, touchEpoch])
}
