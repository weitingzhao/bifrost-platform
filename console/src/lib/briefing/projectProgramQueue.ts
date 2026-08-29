/**
 * Project board-visible program phases into Briefing / In Flight QueueItem[].
 * Used when a lane has no spine/hardcoded queue (program-first lanes).
 */

import type { ProgramPhaseDetail, ProgramSummary } from '@/api/programsTypes'
import { isProgramSessionReleased } from '@/lib/briefing/programClose'
import type { QueueItem, QueueItemStatus } from '@/lib/briefing/workLanes'

function phaseRequiresGate(phase: ProgramPhaseDetail): boolean {
  // Legacy: sign_off nil ⇒ required. Explicit required:false ⇒ work phase.
  return phase.sign_off == null || phase.sign_off.required !== false
}

function phaseIsDone(phase: ProgramPhaseDetail): boolean {
  const st = (phase.status ?? '').toLowerCase()
  if (st === 'done' || st === 'closed' || st === 'complete' || st === 'completed') return true
  if (phase.progress?.status === 'done') return true
  return false
}

/** Map one program phase to a queue status (shared with Active Session join by phase id). */
export function mapPhaseToQueueStatus(
  phase: ProgramPhaseDetail,
  doneIds: Set<string>,
): QueueItemStatus {
  const gate = phaseRequiresGate(phase)
  if (phase.signed_off === true) return 'done'
  if (phaseIsDone(phase)) {
    if (gate) return 'ready_for_signoff'
    return 'done'
  }
  if (phase.progress?.status === 'in_progress') return 'in_progress'
  if (phase.progress?.verify_passed === true && gate && !phase.signed_off) {
    return 'ready_for_signoff'
  }

  const deps = phase.depends_on ?? []
  const depsMet = deps.every(d => doneIds.has(d))
  if (deps.length > 0 && !depsMet) return 'pending'
  return 'next'
}

function phaseDoneForDeps(phase: ProgramPhaseDetail): boolean {
  return phase.signed_off === true || phaseIsDone(phase) || (!phaseRequiresGate(phase) && phaseIsDone(phase))
}

export function projectPhasesToQueue(phases: ProgramPhaseDetail[]): QueueItem[] {
  const doneIds = new Set<string>()
  for (const p of phases) {
    if (phaseDoneForDeps(p) || p.signed_off) doneIds.add(p.id)
    // Non-gate done counts as done for depends_on; gate ready_for_signoff also unblocks dependents.
    if (phaseIsDone(p)) doneIds.add(p.id)
  }

  return phases.map(phase => {
    const status = mapPhaseToQueueStatus(phase, doneIds)
    const noteParts: string[] = []
    if (phase.progress?.summary) noteParts.push(phase.progress.summary)
    if (phase.signed_off) {
      noteParts.push(
        `Signed${phase.signed_off_by != null && phase.signed_off_by !== '' ? ` by ${phase.signed_off_by}` : ''}`,
      )
    } else if (status === 'ready_for_signoff') {
      noteParts.push('Ready for Owner sign-off')
    }
    return {
      id: phase.id,
      label: phase.title || phase.id,
      status,
      note: noteParts.length > 0 ? noteParts.join(' · ') : undefined,
      progress:
        status === 'done' || status === 'closed'
          ? { done: 1, total: 1 }
          : status === 'ready_for_signoff' || status === 'in_progress'
            ? { done: 0, total: 1 }
            : undefined,
    }
  })
}

/**
 * Project open (not sessionReleased) board programs for a lane into a queue.
 * Returns [] when no open program or no phases — caller keeps empty lifecycle.
 */
export function projectQueueFromOpenPrograms(
  laneId: string,
  programs: ProgramSummary[],
): QueueItem[] {
  const open = programs.filter(
    p =>
      (p.lane_id ?? '') === laneId &&
      !isProgramSessionReleased(p) &&
      Array.isArray(p.phases) &&
      (p.phases?.length ?? 0) > 0,
  )
  if (open.length === 0) return []

  // Prefer incomplete then id (same spirit as ActiveSessionPhaseBoard sort).
  const sorted = [...open].sort((a, b) => {
    const aOpen = !isProgramSessionReleased(a)
    const bOpen = !isProgramSessionReleased(b)
    if (aOpen !== bOpen) return aOpen ? -1 : 1
    return a.id.localeCompare(b.id)
  })

  const primary = sorted[0]
  return projectPhasesToQueue(primary.phases ?? [])
}
