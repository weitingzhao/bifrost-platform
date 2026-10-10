/**
 * Sidebar Ask-for-Agent — retired with Console Agent dispatch (ADR §6).
 *
 * No tab ships a Copy / Diagnose pack any more, so no tab is capable and the
 * sidebar slot renders nothing. Exports stay until the slot itself is removed.
 */

import type { LucideIcon } from 'lucide-react'
import { Sparkles } from 'lucide-react'
import type { Signal } from '@/lib/control-room/missionSignals'

export type NavAgentCapableId = never

export function isNavAgentCapable(tabId: string): tabId is NavAgentCapableId {
  void tabId
  return false
}

export function navAgentNeedsAsk(signal: Signal | null, tabId?: string): boolean {
  void tabId
  return signal != null && signal !== 'ok'
}

export function navAgentAskIcon(tabId: string): LucideIcon {
  void tabId
  return Sparkles
}

export function navAgentAskIdleTitle(tabId: string, needsAsk: boolean): string {
  void tabId
  return needsAsk ? 'Ask for Agent' : 'Ask for Agent available'
}

export async function gatherNavAgentPack(tabId: string): Promise<string> {
  throw new Error(`No Ask-for-Agent pack for ${tabId}`)
}
