import { createSession } from '@/api/sessions'

export type EnsureSessionForPackInput = {
  programId?: string | null
  phaseId?: string | null
  laneId?: string | null
  /** Build pack text with the session UUID injected into the header. */
  buildPack: (sessionId: string | undefined) => string
}

export type EnsureSessionForPackResult = {
  sessionId: string | undefined
  pack: string
  /** True when a Session Job was archived via POST /api/v1/sessions. */
  archived: boolean
}

/**
 * On Copy / Launch: archive a Session Job when program+phase are known,
 * then return pack text with a real session_id header.
 * Ad-hoc Briefing (no program) skips POST and may keep session_id: —.
 */
export async function ensureSessionForPack(
  input: EnsureSessionForPackInput,
): Promise<EnsureSessionForPackResult> {
  const programId = input.programId?.trim() || ''
  const phaseId = input.phaseId?.trim() || ''
  if (programId === '' || phaseId === '') {
    const pack = input.buildPack(undefined)
    return { sessionId: undefined, pack, archived: false }
  }

  const sessionId = crypto.randomUUID()
  const pack = input.buildPack(sessionId)
  await createSession({
    session_id: sessionId,
    program_id: programId,
    phase_id: phaseId,
    lane_id: input.laneId?.trim() || undefined,
    pack,
  })
  return { sessionId, pack, archived: true }
}
