/**
 * Sidebar Ask-for-Agent — pages that already ship a Copy / Diagnose pack.
 *
 * Click gathers the same pack the page button would, without opening the page.
 * Add a tab here only when gather is page-independent (no mounted view-model).
 */

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
import type { Signal } from '@/lib/control-room/missionSignals'

export type NavAgentCapableId =
  | 'market-data-manage'
  | 'flex-query-manage'
  | 'research-engine'
  | 'code-health'

const CAPABLE = new Set<string>([
  'market-data-manage',
  'flex-query-manage',
  'research-engine',
  'code-health',
])

export function isNavAgentCapable(tabId: string): tabId is NavAgentCapableId {
  return CAPABLE.has(tabId)
}

/** Green lamp = healthy; anything else can raise Ask. */
export function navAgentNeedsAsk(signal: Signal | null): boolean {
  return signal != null && signal !== 'ok'
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
    return buildCodeHealthAgentPack(await gatherCodeHealthSnapshot())
  }
  throw new Error(`No Ask-for-Agent pack for ${tabId}`)
}
