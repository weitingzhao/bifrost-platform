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
  type BriefingActiveSession,
} from '@/lib/briefing/briefingActiveSession'
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

function useBriefingActiveSessionLive(): BriefingActiveSession | null {
  const [session, setSession] = useState<BriefingActiveSession | null>(() =>
    loadBriefingActiveSession(),
  )

  useEffect(() => {
    const refresh = () => setSession(loadBriefingActiveSession())
    refresh()
    window.addEventListener('storage', refresh)
    window.addEventListener('focus', refresh)
    window.addEventListener('bifrost-briefing-active-session', refresh)
    return () => {
      window.removeEventListener('storage', refresh)
      window.removeEventListener('focus', refresh)
      window.removeEventListener('bifrost-briefing-active-session', refresh)
    }
  }, [])

  return session
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

  const resolvedId = useMemo(() => {
    if (mode.loopArchetype !== 'dev' || !hasActiveSession || activeLane == null) {
      return undefined
    }
    if (session?.programId != null && session.programId.trim() !== '') {
      return session.programId.trim()
    }
    const boardHit = (boardQ.data?.programs ?? []).find(p => p.lane_id === activeLane)
    if (boardHit != null) return boardHit.id
    // Prefer board lane match before localStorage; wait for first board fetch.
    // Detail fetch still validates program.lane_id === activeLane.
    if (!boardQ.isFetched) return undefined
    const stored = readStoredProgramId(mode.id)
    return stored ?? undefined
  }, [
    mode.loopArchetype,
    mode.id,
    hasActiveSession,
    activeLane,
    session?.programId,
    boardQ.data?.programs,
    boardQ.isFetched,
  ])

  const [programId, setProgramId] = useState<string | undefined>(resolvedId)

  useEffect(() => {
    setProgramId(resolvedId)
  }, [resolvedId])

  const programQ = useQuery({
    queryKey: ['programs', programId],
    queryFn: () => fetchProgramDetail(programId!),
    enabled: programId != null && mode.loopArchetype === 'dev',
    retry: false,
  })

  // Drop stored/session bind when program lane mismatches Active Session.
  // Must clear session.programId too — otherwise resolvedId rebinds the same id.
  useEffect(() => {
    if (!hasActiveSession || activeLane == null || programQ.data == null) return
    const progLane = programQ.data.program.lane_id
    if (progLane != null && progLane !== '' && progLane !== activeLane) {
      clearStoredProgramId(mode.id)
      if (session?.programId === programQ.data.program.id) {
        clearProgramFromBriefingSession()
      }
      setProgramId(undefined)
    }
  }, [hasActiveSession, activeLane, programQ.data, mode.id, session?.programId])

  // Persist + attach when a matching program is resolved.
  useEffect(() => {
    if (!hasActiveSession || programId == null || programQ.data == null) return
    const progLane = programQ.data.program.lane_id
    if (progLane != null && progLane !== '' && progLane !== activeLane) return
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

  // Surface template/API errors only when user attempted create or resolved id fails.
  const programError =
    createMutation.error != null
      ? (createMutation.error as Error)
      : programId != null && programQ.error != null
        ? (programQ.error as Error)
        : null

  return {
    programId,
    programDetail: programQ.data ?? createMutation.data,
    programLoading:
      (programId != null && programQ.isLoading) ||
      createMutation.isPending ||
      (hasActiveSession && boardQ.isLoading && programId == null),
    programError,
    createPending: createMutation.isPending,
    hasActiveSession,
    activeLane,
    canCreateProgram,
    ensureProgram,
    createNewInstance,
  }
}
