import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  createLane,
  fetchLanes,
  LANES_QUERY_KEY,
  mapLaneApiToWorkLane,
  type CreateLaneRequest,
} from '@/api/lanes'
import { allWorkLanes, setLaneCatalog } from '@/lib/briefing/workLanes'

/** Hydrates the module-level lane catalog from GET /api/v1/lanes. */
export function useLaneCatalog() {
  const query = useQuery({
    queryKey: LANES_QUERY_KEY,
    queryFn: fetchLanes,
    staleTime: 30_000,
  })

  // Sync before children render in the same pass (query.data arrival re-renders ConsolePage).
  if (query.data?.lanes != null) {
    const mapped = query.data.lanes.map(mapLaneApiToWorkLane)
    const current = allWorkLanes()
    const changed =
      current.length !== mapped.length ||
      mapped.some((l, i) => current[i]?.id !== l.id || current[i]?.label !== l.label)
    if (changed) {
      setLaneCatalog(mapped)
    }
  }

  return query
}

export function useCreateLane() {
  const qc = useQueryClient()
  return async (body: CreateLaneRequest) => {
    const created = await createLane(body)
    await qc.invalidateQueries({ queryKey: LANES_QUERY_KEY })
    return created
  }
}
