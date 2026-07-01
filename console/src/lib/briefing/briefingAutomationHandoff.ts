import type { BriefingPackSize } from '@/lib/briefing/briefingUrlState'
import type { LaneId } from '@/lib/briefing/workLanes'
import type { WorkIntent } from '@/lib/briefing/workIntents'
import type { TrackId } from '@/lib/briefing/workTracks'

export const BRIEFING_AUTOMATION_VERSION = 'briefing-automation-v1'

export interface BriefingAutomationHandoff {
  version: typeof BRIEFING_AUTOMATION_VERSION
  trigger: 'bifrost.briefing.session_pack'
  generated_at: string
  meta: {
    track: TrackId
    lane: LaneId
    intent: WorkIntent
    pack: BriefingPackSize
    char_count: number
  }
  cursor_automation: {
    description: string
    prefill: string
    suggested_name: string
  }
}

export function buildBriefingAutomationHandoff(opts: {
  pack: string
  track: TrackId
  lane: LaneId
  intent: WorkIntent
  packSize: BriefingPackSize
}): BriefingAutomationHandoff {
  return {
    version: BRIEFING_AUTOMATION_VERSION,
    trigger: 'bifrost.briefing.session_pack',
    generated_at: new Date().toISOString(),
    meta: {
      track: opts.track,
      lane: opts.lane,
      intent: opts.intent,
      pack: opts.packSize,
      char_count: opts.pack.length,
    },
    cursor_automation: {
      description:
        'Import into Cursor Automation: trigger on manual run or schedule; prefill opens a new Agent chat with the session pack.',
      prefill: opts.pack,
      suggested_name: `Bifrost briefing · ${opts.track}/${opts.lane}`,
    },
  }
}

export function formatAutomationHandoffJson(handoff: BriefingAutomationHandoff): string {
  return JSON.stringify(handoff, null, 2)
}
