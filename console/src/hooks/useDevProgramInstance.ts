import { useCallback, useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  createProgramFromTemplate,
  fetchProgramDetail,
  invalidateProgramDeliveryQueries,
} from '@/api/programs'
import type { ProgramDetailResponse } from '@/api/programsTypes'
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

export type UseDevProgramInstanceResult = {
  programId: string | undefined
  programDetail: ProgramDetailResponse | undefined
  programLoading: boolean
  programError: Error | null
  createPending: boolean
  ensureProgram: () => void
  createNewInstance: (opts?: { instanceLabel?: string; notes?: string }) => void
}

/** Resolve or auto-create a Delivery Board program instance for a dev task mode. */
export function useDevProgramInstance(mode: TaskModeDef): UseDevProgramInstanceResult {
  const qc = useQueryClient()
  const templateId = mode.dev?.templateId
  const autoCreateAttempted = useRef(false)

  const [programId, setProgramId] = useState<string | undefined>(() => {
    if (mode.loopArchetype !== 'dev') return undefined
    return readStoredProgramId(mode.id) ?? undefined
  })

  const programQ = useQuery({
    queryKey: ['programs', programId],
    queryFn: () => fetchProgramDetail(programId!),
    enabled: programId != null && mode.loopArchetype === 'dev',
    retry: false,
  })

  const createMutation = useMutation({
    mutationFn: (body: { instance_label?: string; notes?: string }) =>
      createProgramFromTemplate({
        template_id: templateId!,
        instance_label: body.instance_label,
        notes: body.notes,
      }),
    onSuccess: data => {
      const id = data.program.id
      setProgramId(id)
      persistProgramId(mode.id, id)
      qc.setQueryData(['programs', id], data)
      invalidateProgramDeliveryQueries(qc, id)
    },
  })

  const createNewInstance = useCallback(
    (opts?: { instanceLabel?: string; notes?: string }) => {
      if (templateId == null) return
      createMutation.mutate({
        instance_label: opts?.instanceLabel ?? mode.label,
        notes: opts?.notes,
      })
    },
    [createMutation, mode.label, templateId],
  )

  const ensureProgram = useCallback(() => {
    createNewInstance({ instanceLabel: mode.label })
  }, [createNewInstance, mode.label])

  useEffect(() => {
    autoCreateAttempted.current = false
    if (mode.loopArchetype !== 'dev') {
      setProgramId(undefined)
      return
    }
    setProgramId(readStoredProgramId(mode.id) ?? undefined)
  }, [mode.id, mode.loopArchetype])

  useEffect(() => {
    if (mode.loopArchetype !== 'dev' || templateId == null) return
    if (programId != null || createMutation.isPending) return
    if (autoCreateAttempted.current) return
    autoCreateAttempted.current = true
    createNewInstance({ instanceLabel: mode.label })
  }, [
    mode.loopArchetype,
    mode.label,
    templateId,
    programId,
    createMutation.isPending,
    createNewInstance,
  ])

  useEffect(() => {
    if (mode.loopArchetype !== 'dev' || templateId == null) return
    if (programQ.isLoading || createMutation.isPending) return
    if (programQ.error == null) return
    createNewInstance({ instanceLabel: mode.label })
  }, [
    mode.loopArchetype,
    mode.label,
    templateId,
    programQ.isLoading,
    programQ.error,
    createMutation.isPending,
    createNewInstance,
  ])

  return {
    programId,
    programDetail: programQ.data ?? createMutation.data,
    programLoading: programQ.isLoading || createMutation.isPending,
    programError: (programQ.error as Error | null) ?? (createMutation.error as Error | null),
    createPending: createMutation.isPending,
    ensureProgram,
    createNewInstance,
  }
}
