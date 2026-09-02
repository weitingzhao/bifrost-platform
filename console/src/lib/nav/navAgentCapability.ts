/**
 * Sidebar Ask-for-Agent — pages that already ship a Copy / Diagnose pack.
 *
 * Click gathers the same pack the page button would, without opening the page.
 * Add a tab here only when gather is page-independent (no mounted view-model).
 */

import type { LucideIcon } from 'lucide-react'
import { ClipboardCopy, Sparkles } from 'lucide-react'
import {
  buildFlexAgentPack,
  gatherFlexAgentSnapshot,
} from '@/components/flex-query/flexAgentPack'
import {
  buildMassiveAgentPack,
  gatherMassiveAgentSnapshot,
} from '@/components/market-data/massiveAgentPack'
import {
  buildResearchEngineAgentPack,
  gatherResearchEngineSnapshot,
} from '@/components/research/researchEngineAgentPack'
import {
  buildCodeHealthAgentPack,
  gatherCodeHealthSnapshot,
} from '@/lib/code-health/codeHealthAgentPack'
import {
  buildControlRoomAgentPack,
  gatherControlRoomAgentSnapshot,
} from '@/lib/control-room/controlRoomAgentPack'
import type { Signal } from '@/lib/control-room/missionSignals'

export type NavAgentCapableId =
  | 'market-data-manage'
  | 'flex-query-manage'
  | 'research-engine'
  | 'code-health'
  | 'control-room'

const CAPABLE = new Set<string>([
  'market-data-manage',
  'flex-query-manage',
  'research-engine',
  'code-health',
  'control-room',
])

export function isNavAgentCapable(tabId: string): tabId is NavAgentCapableId {
  return CAPABLE.has(tabId)
}

/**
 * Whether Ask chrome should escalate (colored / louder).
 * Code Health: only OVER escalates — Generate Agent Pack is always available,
 * and AT CEILING must not paint yellow on the pack affordance.
 */
export function navAgentNeedsAsk(signal: Signal | null, tabId?: string): boolean {
  if (tabId === 'code-health') return signal === 'fail'
  return signal != null && signal !== 'ok'
}

/** Trailing affordance icon — Code Health uses ClipboardCopy (Generate Agent Pack). */
export function navAgentAskIcon(tabId: string): LucideIcon {
  if (tabId === 'code-health') return ClipboardCopy
  return Sparkles
}

export function navAgentAskIdleTitle(tabId: string, needsAsk: boolean): string {
  if (tabId === 'code-health') {
    return needsAsk
      ? 'Generate Agent Pack — OVER baseline; Live Re-scan + copy Code Refactor Agent Task Content'
      : 'Generate Agent Pack — Live Re-scan when available, then copy Code Refactor Agent Task Content'
  }
  return needsAsk
    ? 'Ask for Agent — copy diagnose pack (same as the page button)'
    : 'Ask for Agent available — copy diagnose pack'
}

export async function gatherNavAgentPack(tabId: string): Promise<string> {
  if (tabId === 'market-data-manage') {
    return buildMassiveAgentPack(await gatherMassiveAgentSnapshot())
  }
  if (tabId === 'flex-query-manage') {
    return buildFlexAgentPack(await gatherFlexAgentSnapshot())
  }
  if (tabId === 'research-engine') {
    return buildResearchEngineAgentPack(await gatherResearchEngineSnapshot())
  }
  if (tabId === 'code-health') {
    return buildCodeHealthAgentPack(await gatherCodeHealthSnapshot({ liveRescanFirst: true }))
  }
  if (tabId === 'control-room') {
    return buildControlRoomAgentPack(await gatherControlRoomAgentSnapshot())
  }
  throw new Error(`No Ask-for-Agent pack for ${tabId}`)
}
