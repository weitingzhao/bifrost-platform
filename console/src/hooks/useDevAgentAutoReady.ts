import { useCallback, useMemo } from 'react'
import { useQueries, useQuery } from '@tanstack/react-query'
import { fetchDevAgentProgram, fetchDevAgentPrograms } from '@/api/devAgent'
import type { DevAgentProgramDetailResponse } from '@/api/devAgentTypes'
import { countAutoReadyPhases } from '@/lib/briefing/devAgentAutoReady'
import {
  DEV_AGENT_PROGRAMS_QUERY_KEY,
  devAgentProgramQueryKey,
} from './useDevAgentMutations'

export type DevAgentAutoReadyPhase = {
  id: string
  title: string
  status: string
  verify_cmd?: string
}

export type DevAgentAutoReadyEntry = {
  autoReadyCount: number
  phases: DevAgentAutoReadyPhase[]
}

const LIST_STALE_MS = 30_000
const DETAIL_STALE_MS = 30_000

function normalizeProgramIds(programIds: string[]): string[] {
  return [...new Set(programIds.map(id => id.trim()).filter(id => id !== ''))].sort()
}

function toEntry(detail: DevAgentProgramDetailResponse): DevAgentAutoReadyEntry {
  const phases: DevAgentAutoReadyPhase[] = detail.phases.map(p => ({
    id: p.id,
    title: p.title,
    status: p.status,
    verify_cmd: p.verify_cmd,
  }))
  return {
    autoReadyCount: countAutoReadyPhases(phases),
    phases,
  }
}

/**
 * List once, then detail-fetch only ids that exist in the agent catalog.
 * Callers should pass visible/selected program ids (not the full board).
 */
export function useDevAgentAutoReady(programIds: string[]): {
  byProgramId: Map<string, DevAgentAutoReadyEntry>
  autoReadyCount: (programId: string) => number
  entryFor: (programId: string) => DevAgentAutoReadyEntry | undefined
} {
  const idKey = normalizeProgramIds(programIds).join('\0')
  const normalizedIds = useMemo(
    () => (idKey === '' ? [] : idKey.split('\0')),
    [idKey],
  )

  const listQuery = useQuery({
    queryKey: DEV_AGENT_PROGRAMS_QUERY_KEY,
    queryFn: fetchDevAgentPrograms,
    staleTime: LIST_STALE_MS,
  })

  const agentIdSet = useMemo(
    () => new Set((listQuery.data?.programs ?? []).map(p => p.id)),
    [listQuery.data?.programs],
  )

  const fetchableIds = useMemo(
    () => normalizedIds.filter(id => agentIdSet.has(id)),
    [normalizedIds, agentIdSet],
  )

  const detailQueries = useQueries({
    queries: fetchableIds.map(id => ({
      queryKey: devAgentProgramQueryKey(id),
      queryFn: () => fetchDevAgentProgram(id),
      staleTime: DETAIL_STALE_MS,
      enabled: listQuery.isSuccess,
    })),
  })

  const byProgramId = useMemo(() => {
    const map = new Map<string, DevAgentAutoReadyEntry>()
    fetchableIds.forEach((id, i) => {
      const data = detailQueries[i]?.data
      if (data == null) return
      map.set(id, toEntry(data))
    })
    return map
  }, [fetchableIds, detailQueries])

  const autoReadyCount = useCallback(
    (programId: string) => byProgramId.get(programId)?.autoReadyCount ?? 0,
    [byProgramId],
  )
  const entryFor = useCallback(
    (programId: string) => byProgramId.get(programId),
    [byProgramId],
  )

  return { byProgramId, autoReadyCount, entryFor }
}
