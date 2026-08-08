import { useCallback, useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  createProgramFromTemplate,
  fetchDeliveryBoardPrograms,
  fetchProgramDetail,
  invalidateProgramDeliveryQueries,
  PROGRAMS_BOARD_QUERY_KEY,
} from '@/api/programs'
import type { ProgramDetailResponse } from '@/api/programsTypes'
import {
  attachProgramToBriefingSession,
  clearProgramFromBriefingSession,
  loadBriefingActiveSession,
} from '@/lib/briefing/briefingActiveSession'
import { useBriefingActiveSessionLive } from '@/hooks/useBriefingActiveSessionLive'
import {
  programLaneCompatible,
  resolveDevProgramId,
} from '@/lib/task-mode/devProgramResolve'
import type { TaskModeDef } from '@/lib/task-mode/types'

const STORAGE_PREFIX = 'bifrost-task-program-'

function storageKey(modeId: string): string {
  return `${STORAGE_PREFIX}${modeId}`
}

function readStoredProgramId(modeId: string): string | null {
  try {
    return localStorage.getItem(storageKey(modeId))
  } catch {
    return null
  }
}

function persistProgramId(modeId: string, programId: string) {
  try {
    localStorage.setItem(storageKey(modeId), programId)
  } catch {
    // ignore
  }
}

function clearStoredProgramId(modeId: string) {
  try {
    localStorage.removeItem(storageKey(modeId))
  } catch {
    // ignore
  }
}

export type UseDevProgramInstanceResult = {
  programId: string | undefined
  programDetail: ProgramDetailResponse | undefined
  programLoading: boolean
  programError: Error | null
  createPending: boolean
  /** Active Session present — required before Create. */
  hasActiveSession: boolean
  activeLane: string | undefined
  canCreateProgram: boolean
  ensureProgram: () => void
  createNewInstance: (opts?: { instanceLabel?: string; notes?: string; laneId?: string }) => void
}

/**
 * Resolve a Delivery Board program for a dev task mode.
 * Authority: Active Session lane — never auto-create without a session.
 */
export function useDevProgramInstance(mode: TaskModeDef): UseDevProgramInstanceResult {
  const qc = useQueryClient()
  const templateId = mode.dev?.templateId
  const session = useBriefingActiveSessionLive()
  const activeLane = session?.lane
  const hasActiveSession = session != null && activeLane != null && activeLane !== ''

  const boardQ = useQuery({
    queryKey: PROGRAMS_BOARD_QUERY_KEY,
    queryFn: fetchDeliveryBoardPrograms,
    enabled: mode.loopArchetype === 'dev' && hasActiveSession,
    staleTime: 15_000,
    refetchInterval: 30_000,
  })

  const resolvedId = useMemo(
    () =>
      resolveDevProgramId({
        hasActiveSession,
        activeLane,
        sessionProgramId: session?.programId,
        boardPrograms: boardQ.data?.programs ?? [],
        boardFetched: boardQ.isFetched,
        storedProgramId: readStoredProgramId(mode.id),
      }),
    [
      hasActiveSession,
      activeLane,
      session?.programId,
      boardQ.data?.programs,
      boardQ.isFetched,
      mode.id,
    ],
  )

  const [programId, setProgramId] = useState<string | undefined>(resolvedId)

  useEffect(() => {
    setProgramId(resolvedId)
  }, [resolvedId])

  // Board already proves session.programId is wrong-lane — clear immediately (less flash).
  useEffect(() => {
    if (!hasActiveSession || activeLane == null || !boardQ.isFetched) return
    const sessionId = session?.programId?.trim()
    if (sessionId == null || sessionId === '') return
    const onBoard = (boardQ.data?.programs ?? []).find(p => p.id === sessionId)
    if (onBoard != null && !programLaneCompatible(onBoard.lane_id, activeLane)) {
      clearProgramFromBriefingSession()
      clearStoredProgramId(mode.id)
      setProgramId(undefined)
    }
  }, [
    hasActiveSession,
    activeLane,
    boardQ.isFetched,
    boardQ.data?.programs,
    session?.programId,
    mode.id,
  ])

  const programQ = useQuery({
    queryKey: ['programs', programId],
    queryFn: () => fetchProgramDetail(programId!),
    enabled: programId != null && mode.loopArchetype === 'dev',
    retry: false,
  })

  // Detail path: drop bind when lane missing or mismatches Active Session.
  useEffect(() => {
    if (!hasActiveSession || activeLane == null || programQ.data == null) return
    if (!programLaneCompatible(programQ.data.program.lane_id, activeLane)) {
      clearStoredProgramId(mode.id)
      if (session?.programId === programQ.data.program.id) {
        clearProgramFromBriefingSession()
      }
      setProgramId(undefined)
    }
  }, [hasActiveSession, activeLane, programQ.data, mode.id, session?.programId])

  // Persist + attach only after lane-compatible detail is confirmed.
  useEffect(() => {
    if (!hasActiveSession || activeLane == null || programId == null || programQ.data == null) return
    if (!programLaneCompatible(programQ.data.program.lane_id, activeLane)) return
    persistProgramId(mode.id, programId)
    attachProgramToBriefingSession(programId)
  }, [hasActiveSession, programId, programQ.data, activeLane, mode.id])

  const createMutation = useMutation({
    mutationFn: (body: { instance_label?: string; notes?: string; lane_id?: string }) =>
      createProgramFromTemplate({
        template_id: templateId!,
        instance_label: body.instance_label,
        notes: body.notes,
        lane_id: body.lane_id,
      }),
    onSuccess: data => {
      const id = data.program.id
      setProgramId(id)
      persistProgramId(mode.id, id)
      attachProgramToBriefingSession(id)
      qc.setQueryData(['programs', id], data)
      invalidateProgramDeliveryQueries(qc, id)
    },
  })

  const createNewInstance = useCallback(
    (opts?: { instanceLabel?: string; notes?: string; laneId?: string }) => {
      if (templateId == null) return
      const live = loadBriefingActiveSession()
      const lane = opts?.laneId ?? live?.lane
      if (lane == null || lane === '') return
      const label = opts?.instanceLabel ?? `${mode.label} · ${lane}`
      createMutation.mutate({
        instance_label: label,
        notes: opts?.notes,
        lane_id: lane,
      })
    },
    [createMutation, mode.label, templateId],
  )

  const canCreateProgram = hasActiveSession && templateId != null

  const ensureProgram = useCallback(() => {
    if (!canCreateProgram) return
    createNewInstance()
  }, [canCreateProgram, createNewInstance])

  const programError =
    createMutation.error != null
      ? (createMutation.error as Error)
      : programId != null && programQ.error != null
        ? (programQ.error as Error)
        : null

  const detailOk =
    programQ.data != null &&
    activeLane != null &&
    programLaneCompatible(programQ.data.program.lane_id, activeLane)

  const createdOk =
    createMutation.data != null &&
    activeLane != null &&
    programLaneCompatible(createMutation.data.program.lane_id, activeLane)

  return {
    programId,
    // Hide detail until lane-compatible — avoids wrong-program flash in UI.
    programDetail: detailOk ? programQ.data : createdOk ? createMutation.data : undefined,
    programLoading:
      createMutation.isPending ||
      (hasActiveSession && boardQ.isLoading && programId == null) ||
      (programId != null && programQ.isLoading) ||
      (programId != null && programQ.isSuccess && !detailOk),
    programError,
    createPending: createMutation.isPending,
    hasActiveSession,
    activeLane,
    canCreateProgram,
    ensureProgram,
    createNewInstance,
  }
}
