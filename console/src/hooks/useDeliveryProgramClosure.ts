import { useCallback, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { fetchDeliveryBoardPrograms, PROGRAMS_BOARD_QUERY_KEY } from '@/api/programs'
import {
  buildLaneProgramsSessionReleasedMap,
  openDeliveryProgramsForLane,
  programsReleasedForLane,
} from '@/lib/briefing/briefingStatus'

/** Shared board query → per-lane sessionReleased map (TanStack dedupes). */
export function useDeliveryProgramClosure() {
  const query = useQuery({
    queryKey: PROGRAMS_BOARD_QUERY_KEY,
    queryFn: fetchDeliveryBoardPrograms,
    staleTime: 15_000,
    refetchInterval: 30_000,
  })
  const releasedByLane = useMemo(
    () =>
      query.isSuccess ? buildLaneProgramsSessionReleasedMap(query.data.programs ?? []) : undefined,
    [query.isSuccess, query.data?.programs],
  )
  const programs = useMemo(() => query.data?.programs ?? [], [query.data?.programs])
  const programsReleasedFor = useCallback(
    (laneId: string) => programsReleasedForLane(laneId, releasedByLane),
    [releasedByLane],
  )
  const openProgramsFor = useCallback(
    (laneId: string) => openDeliveryProgramsForLane(laneId, programs),
    [programs],
  )

  return {
    releasedByLane,
    /** @deprecated alias of releasedByLane */
    closedByLane: releasedByLane,
    programsReady: query.isSuccess,
    programsReleasedFor,
    /** @deprecated alias of programsReleasedFor */
    programsClosedFor: programsReleasedFor,
    openProgramsFor,
  }
}
