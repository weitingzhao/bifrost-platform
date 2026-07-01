import type { WorkIntent } from '@/lib/briefing/workIntents'
import { isLaneId, type LaneId } from '@/lib/briefing/workLanes'
import type { TrackId } from '@/lib/briefing/workTracks'

export const BRIEFING_TRACK_PARAM = 'track'
export const BRIEFING_LANE_PARAM = 'lane'
export const BRIEFING_INTENT_PARAM = 'intent'
export const BRIEFING_PACK_PARAM = 'pack'

export type BriefingPackSize = 'compact' | 'full'

export interface BriefingUrlState {
  track?: TrackId
  lane?: LaneId
  intent?: WorkIntent
  pack?: BriefingPackSize
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
  const track = url.searchParams.get(BRIEFING_TRACK_PARAM)
  const lane = url.searchParams.get(BRIEFING_LANE_PARAM)
  const intent = url.searchParams.get(BRIEFING_INTENT_PARAM)
  const pack = url.searchParams.get(BRIEFING_PACK_PARAM)

  if (track != null && isTrackId(track)) state.track = track
  if (lane != null && isLaneId(lane)) state.lane = lane
  if (intent != null && isWorkIntent(intent)) state.intent = intent
  if (pack != null && isBriefingPackSize(pack)) state.pack = pack

  return state
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

  if ('track' in partial) setOrDelete(BRIEFING_TRACK_PARAM, partial.track)
  if ('lane' in partial) setOrDelete(BRIEFING_LANE_PARAM, partial.lane)
  if ('intent' in partial) setOrDelete(BRIEFING_INTENT_PARAM, partial.intent)
  if ('pack' in partial) setOrDelete(BRIEFING_PACK_PARAM, partial.pack)

  window.history.replaceState(null, '', url)
}

export function buildBriefingDeepLink(opts: BriefingUrlState): string {
  const url = new URL(window.location.href)
  url.hash = '#briefing'
  writeBriefingUrlState(opts, url)
  return `${url.pathname}${url.search}${url.hash}`
}
