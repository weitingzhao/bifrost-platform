import type { ProgramSummary } from '@/api/programsTypes'

/** True when program lane is safe to bind under an Active Session lane. */
export function programLaneCompatible(
  programLane: string | undefined | null,
  activeLane: string,
): boolean {
  const lane = activeLane.trim()
  if (lane === '') return false
  const prog = (programLane ?? '').trim()
  // Missing lane_id on legacy programs is not trustworthy when a session lane is set.
  if (prog === '') return false
  return prog === lane
}

/**
 * Pick a Delivery Board program for the Active Session lane.
 * Order: preferred id (if lane-compatible) → active → incomplete → id.
 */
export function pickBoardProgramForLane(
  programs: ProgramSummary[],
  activeLane: string,
  preferredProgramId?: string | null,
): ProgramSummary | undefined {
  const lane = activeLane.trim()
  if (lane === '') return undefined
  const linked = programs.filter(p => programLaneCompatible(p.lane_id, lane))
  if (linked.length === 0) return undefined

  const preferred = preferredProgramId?.trim()
  if (preferred) {
    const hit = linked.find(p => p.id === preferred)
    if (hit != null) return hit
  }

  return [...linked].sort((a, b) => {
    if (a.active !== b.active) return a.active ? -1 : 1
    if (a.complete !== b.complete) return a.complete ? 1 : -1
    return a.id.localeCompare(b.id)
  })[0]
}

export type ResolveDevProgramIdInput = {
  hasActiveSession: boolean
  activeLane: string | undefined
  sessionProgramId: string | undefined
  boardPrograms: ProgramSummary[]
  boardFetched: boolean
  storedProgramId: string | null
}

/**
 * Resolve which program id Build TCC should bind.
 * Never returns a session/stored id that the board already proves is wrong-lane.
 * Waits for first board fetch before falling back to localStorage.
 */
export function resolveDevProgramId(input: ResolveDevProgramIdInput): string | undefined {
  if (!input.hasActiveSession || input.activeLane == null || input.activeLane.trim() === '') {
    return undefined
  }
  const lane = input.activeLane.trim()
  const sessionId = input.sessionProgramId?.trim() || undefined

  if (input.boardFetched) {
    if (sessionId != null) {
      const onBoard = input.boardPrograms.find(p => p.id === sessionId)
      if (onBoard != null) {
        if (programLaneCompatible(onBoard.lane_id, lane)) return sessionId
        // Board proves mismatch — do not bind; caller clears session.programId.
      } else {
        // Not listed on board yet — keep provisional id for detail fetch + lane check.
        return sessionId
      }
    }

    const boardPick = pickBoardProgramForLane(input.boardPrograms, lane, sessionId)
    if (boardPick != null) return boardPick.id

    const stored = input.storedProgramId?.trim()
    if (stored) {
      const storedOnBoard = input.boardPrograms.find(p => p.id === stored)
      if (storedOnBoard != null) {
        return programLaneCompatible(storedOnBoard.lane_id, lane) ? stored : undefined
      }
      // Stored id not on board — allow detail validation path.
      return stored
    }
    return undefined
  }

  // Board not fetched: only trust session id provisionally (detail will validate).
  if (sessionId != null) return sessionId
  return undefined
}
