import { isLaneLifecycleHold } from '@/lib/briefing/briefingStatus'
import type { QueueItem } from '@/lib/briefing/workLanes'
import {
  resolveSessionLaneFocus,
  type SessionLaneFocusKind,
} from '@/lib/task-mode/sessionLaneFocus'

export type BuildWorkbenchLamp = 'ok' | 'degraded' | 'fail' | 'unknown'

export type BuildWorkbenchCta =
  | { kind: 'navigate'; tabId: string; label: string }
  | { kind: 'scroll'; elementId: string; label: string }

export type BuildWorkbenchVerdict = {
  lamp: BuildWorkbenchLamp
  /** Binding line — Session / lane / program only (no playbook echo). */
  summary: string
  /** Single next action for meta row — same source as Session Lane Focus. */
  nextLine: string
  cta?: BuildWorkbenchCta
}

export type BuildWorkbenchInput = {
  hasActiveSession: boolean
  activeLane?: string | null
  programId?: string | null
  programLoading?: boolean
  packReady: boolean
  laneQueue?: QueueItem[]
  /** sessionReleased for the Active Session lane. */
  programsReleased?: boolean
  programSigned?: number
  programPhaseCount?: number
  /** True when Dev Agent status probe failed. */
  devAgentError?: boolean
}

/** Dev-loop surface id — Verdict CTA scrolls here for pack / create program. */
export const BUILD_DEV_LOOP_ELEMENT_ID = 'task-cc-dev-loop'

function bindingSummary(lane: string | null, programId: string | null): string {
  return `Lane ${lane ?? '—'} · program ${programId}`
}

function lampFromFocusKind(
  kind: SessionLaneFocusKind,
  agentSoftDegrade: boolean,
): BuildWorkbenchLamp {
  if (kind === 'archive') return 'ok'
  if (agentSoftDegrade) return 'degraded'
  if (kind === 'doing') return 'unknown'
  return 'degraded'
}

function ctaFromFocusKind(kind: SessionLaneFocusKind): BuildWorkbenchCta {
  switch (kind) {
    case 'pick-session':
      return { kind: 'navigate', tabId: 'briefing', label: 'Open Briefing →' }
    case 'signoff':
      return { kind: 'navigate', tabId: 'active-session', label: 'Active Session →' }
    case 'archive':
      return { kind: 'navigate', tabId: 'briefing', label: 'Open Briefing →' }
    case 'plan':
    case 'start':
      return {
        kind: 'scroll',
        elementId: BUILD_DEV_LOOP_ELEMENT_ID,
        label: 'Copy pack →',
      }
    case 'doing':
      return {
        kind: 'scroll',
        elementId: BUILD_DEV_LOOP_ELEMENT_ID,
        label: 'Open Dev loop →',
      }
  }
}

/**
 * Build TCC Verdict — Session / Workbench model.
 * Next line + CTA share resolveSessionLaneFocus with the Dev-loop Session Lane strip.
 */
export function resolveBuildWorkbenchVerdict(input: BuildWorkbenchInput): BuildWorkbenchVerdict {
  const lane = input.activeLane?.trim() || null
  const programId = input.programId?.trim() || null
  const agentSoftDegrade = input.devAgentError === true

  if (!input.hasActiveSession) {
    return {
      lamp: 'fail',
      summary: 'No Active Session — Copy a Briefing session before linking work',
      nextLine: 'Next: Copy session in Agent Briefing',
      cta: { kind: 'navigate', tabId: 'briefing', label: 'Open Briefing →' },
    }
  }

  if (input.programLoading) {
    return {
      lamp: 'unknown',
      summary: `Session · ${lane ?? 'lane'} · loading program…`,
      nextLine: 'Next: Wait for Delivery program bind',
    }
  }

  if (isLaneLifecycleHold(input.laneQueue ?? [], input.programsReleased)) {
    return {
      lamp: 'unknown',
      summary: bindingSummary(lane, programId ?? '—'),
      nextLine: 'Next: Wait for Delivery close state',
    }
  }

  if (programId == null) {
    return {
      lamp: 'degraded',
      summary: `Session · ${lane ?? 'lane'} · no Delivery program`,
      nextLine: 'Next: Create program for this lane',
      cta: {
        kind: 'scroll',
        elementId: BUILD_DEV_LOOP_ELEMENT_ID,
        label: 'Create program →',
      },
    }
  }

  const laneFocus = resolveSessionLaneFocus({
    queue: input.laneQueue ?? [],
    hasActiveSession: true,
    hasProgram: true,
    programsReleased: input.programsReleased,
  })

  const nextLine =
    !input.packReady && (laneFocus.kind === 'plan' || laneFocus.kind === 'start')
      ? 'Next: Prepare Briefing pack (lane context loading)'
      : laneFocus.line

  return {
    lamp: lampFromFocusKind(laneFocus.kind, agentSoftDegrade),
    summary: bindingSummary(lane, programId),
    nextLine,
    cta: ctaFromFocusKind(laneFocus.kind),
  }
}
