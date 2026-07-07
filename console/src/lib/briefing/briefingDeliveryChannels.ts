import type { WorkIntent } from '@/lib/briefing/workIntents'

/** Intents where Ops runner (Agent Desk) tools align with the session pack. */
const AGENT_DESK_SUITED_INTENTS: ReadonlySet<WorkIntent> = new Set([
  'ops',
  'debug',
  'cluster',
  'automate',
  'release',
])

export function isAgentDeskSuitedIntent(intent: WorkIntent): boolean {
  return AGENT_DESK_SUITED_INTENTS.has(intent)
}

export function agentDeskPrefillDisabledReason(intent: WorkIntent): string | undefined {
  if (isAgentDeskSuitedIntent(intent)) return undefined
  return 'Feature, frontend, and business briefings need the full multi-repo Cursor IDE workspace — use Copy session pack.'
}

export const BRIEFING_IDE_DELIVERY_HINT =
  'Primary: Launch IDE Agent (Cursor SDK via platform-api). Fallback: Copy session pack into a new Cursor chat. Full workspace, rules, MCP, and first-reply protocol.'

export const BRIEFING_AGENT_DESK_DELIVERY_HINT =
  'Optional: prefill Agent Desk for the Mac Mini Ops runner (Cursor SDK). Suited to short cluster/debug/release tasks — not a substitute for IDE for multi-repo development.'
