import type { WorkIntent } from '@/lib/briefing/workIntents'
import {
  componentLineForTaskMode,
  isBriefingScopeId,
  isWorkTrackType,
  trackTypeForTaskMode,
  type BriefingScopeId,
  type WorkTrackType,
} from '@/lib/briefing/briefingViewTabs'
import { isLaneId, type LaneId } from '@/lib/briefing/workLanes'
import type { TrackId } from '@/lib/briefing/workTracks'
import type { TaskModeBriefingContext } from '@/lib/task-mode/TaskModeContext'

export const BRIEFING_VIEW_PARAM = 'view'
export const BRIEFING_TRACK_TYPE_PARAM = 'tt'
export const BRIEFING_TRACK_PARAM = 'track'
export const BRIEFING_LANE_PARAM = 'lane'
export const BRIEFING_INTENT_PARAM = 'intent'
export const BRIEFING_PACK_PARAM = 'pack'
export const BRIEFING_PROGRAM_PARAM = 'program'
export const BRIEFING_TASK_MODE_CTX_STORAGE_KEY = 'bifrost-briefing-task-mode-context'

export type BriefingPackSize = 'compact' | 'full'

export interface BriefingUrlState {
  /** Layer 1 — Component line or All. */
  view?: BriefingScopeId
  /** Layer 2 — Work track type (Build / Migrate / Maintain / Release). */
  trackType?: WorkTrackType
  /** Spine data track (legacy compat; usually derived from lane). */
  track?: TrackId
  lane?: LaneId
  intent?: WorkIntent
  pack?: BriefingPackSize
  program?: string
  taskModeContext?: TaskModeBriefingContext
}

const TRACK_IDS = new Set<TrackId>(['build', 'migrate', 'automate', 'infra', 'operate'])

const WORK_INTENTS = new Set<WorkIntent>([
  'ops',
  'feature',
  'debug',
  'release',
  'cluster',
  'frontend',
  'business',
  'automate',
])

export function isTrackId(value: string): value is TrackId {
  return TRACK_IDS.has(value as TrackId)
}

export function isWorkIntent(value: string): value is WorkIntent {
  return WORK_INTENTS.has(value as WorkIntent)
}

export function isBriefingPackSize(value: string): value is BriefingPackSize {
  return value === 'compact' || value === 'full'
}

export function parseBriefingUrlState(url: URL = new URL(window.location.href)): BriefingUrlState {
  const state: BriefingUrlState = {}
  const view = url.searchParams.get(BRIEFING_VIEW_PARAM)
  const tt = url.searchParams.get(BRIEFING_TRACK_TYPE_PARAM)
  const track = url.searchParams.get(BRIEFING_TRACK_PARAM)
  const lane = url.searchParams.get(BRIEFING_LANE_PARAM)
  const intent = url.searchParams.get(BRIEFING_INTENT_PARAM)
  const pack = url.searchParams.get(BRIEFING_PACK_PARAM)
  const program = url.searchParams.get(BRIEFING_PROGRAM_PARAM)

  if (view != null && isBriefingScopeId(view)) state.view = view
  if (tt != null && isWorkTrackType(tt)) state.trackType = tt
  if (track != null && isTrackId(track)) state.track = track
  if (lane != null && isLaneId(lane)) state.lane = lane
  if (intent != null && isWorkIntent(intent)) state.intent = intent
  if (pack != null && isBriefingPackSize(pack)) state.pack = pack
  if (program != null && program.trim() !== '') state.program = program.trim()

  return state
}

/** Resolve initial scope from URL / task-mode context / defaults. */
export function resolveBriefingScope(state: BriefingUrlState): BriefingScopeId {
  if (state.view != null) return state.view
  if (state.taskModeContext?.modeId != null) {
    return componentLineForTaskMode(state.taskModeContext.modeId)
  }
  return 'rocket'
}

/** @deprecated Prefer resolveBriefingScope */
export function resolveComponentLine(state: BriefingUrlState): BriefingScopeId {
  return resolveBriefingScope(state)
}

/** Resolve initial track type from URL / task-mode context / defaults. */
export function resolveTrackType(state: BriefingUrlState): WorkTrackType {
  if (state.trackType != null) return state.trackType
  if (state.taskModeContext?.modeId != null) {
    return trackTypeForTaskMode(state.taskModeContext.modeId)
  }
  return 'build'
}

export function readBriefingTaskModeContext(): TaskModeBriefingContext | undefined {
  try {
    const raw = sessionStorage.getItem(BRIEFING_TASK_MODE_CTX_STORAGE_KEY)
    if (raw == null || raw === '') return undefined
    return JSON.parse(raw) as TaskModeBriefingContext
  } catch {
    return undefined
  }
}

/** Merge briefing query params into the current URL (preserves hash tab). */
export function writeBriefingUrlState(
  partial: BriefingUrlState,
  url: URL = new URL(window.location.href),
): void {
  const setOrDelete = (key: string, value: string | undefined) => {
    if (value == null || value === '') {
      url.searchParams.delete(key)
    } else {
      url.searchParams.set(key, value)
    }
  }

  if ('view' in partial) setOrDelete(BRIEFING_VIEW_PARAM, partial.view)
  if ('trackType' in partial) setOrDelete(BRIEFING_TRACK_TYPE_PARAM, partial.trackType)
  if ('track' in partial) setOrDelete(BRIEFING_TRACK_PARAM, partial.track)
  if ('lane' in partial) setOrDelete(BRIEFING_LANE_PARAM, partial.lane)
  if ('intent' in partial) setOrDelete(BRIEFING_INTENT_PARAM, partial.intent)
  if ('pack' in partial) setOrDelete(BRIEFING_PACK_PARAM, partial.pack)
  if ('program' in partial) setOrDelete(BRIEFING_PROGRAM_PARAM, partial.program)

  if ('taskModeContext' in partial) {
    if (partial.taskModeContext == null) {
      sessionStorage.removeItem(BRIEFING_TASK_MODE_CTX_STORAGE_KEY)
    } else {
      sessionStorage.setItem(
        BRIEFING_TASK_MODE_CTX_STORAGE_KEY,
        JSON.stringify(partial.taskModeContext),
      )
    }
  }

  window.history.replaceState(null, '', url)
}

export function buildBriefingDeepLink(opts: BriefingUrlState): string {
  const url = new URL(window.location.href)
  url.hash = '#briefing'
  writeBriefingUrlState(opts, url)
  return `${url.pathname}${url.search}${url.hash}`
}
