import type { RuntimeHost } from '@/api/agentRuntime'

/**
 * A host's elastic slots in one phrase (R5 phase 2 step 3b): the count the platform's slots pass recorded and the policy
 * of its latest move, or that none is recorded yet; null from a platform before elastic slots.
 */
export function slotsPhrase(host: RuntimeHost): string | null {
  if (host.effective_slots === undefined) return null
  if (host.effective_slots === null) return 'Slots: per the concurrency rule (none recorded)'
  const policy = host.slot_moves?.[0]?.policy
  return `Slots: ${host.effective_slots}${policy ? ` (${policy})` : ''}`
}
