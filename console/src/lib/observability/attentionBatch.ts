/**
 * Batch Agent Fix for Attention — same playbookId, agent_fix CTA only.
 */

import type { AttentionItem } from './types'
import { buildAttentionRemediationPrompt } from './attentionRemediationCatalog'

export const ATTENTION_BATCH_MIN = 3

export type AttentionBatchGroup = {
  playbookId: string
  items: AttentionItem[]
}

/** Largest agent_fix group with shared playbookId (min ATTENTION_BATCH_MIN). */
export function largestAttentionBatchGroup(
  items: AttentionItem[],
  minSize = ATTENTION_BATCH_MIN,
): AttentionBatchGroup | null {
  const byPlaybook = new Map<string, AttentionItem[]>()
  for (const item of items) {
    if (item.triage.cta !== 'agent_fix') continue
    const pid = item.triage.playbookId
    if (pid == null || pid === '') continue
    const list = byPlaybook.get(pid) ?? []
    list.push(item)
    byPlaybook.set(pid, list)
  }
  let best: AttentionBatchGroup | null = null
  for (const [playbookId, group] of byPlaybook) {
    if (group.length < minSize) continue
    if (best == null || group.length > best.items.length) {
      best = { playbookId, items: group }
    }
  }
  return best
}

/** Combined prompt for one remediation job covering many Attention rows. */
export function buildAttentionBatchRemediationPrompt(group: AttentionBatchGroup): string {
  const head = [
    `Observability Attention · Batch Agent Fix (${group.items.length}× ${group.playbookId})`,
    'Playbook: ' + group.playbookId,
    '',
    'Fix the shared root cause for these Attention items (assisted — approve actuations):',
    ...group.items.map(
      (it, i) =>
        `${i + 1}. [${it.severity}] ${it.signalLabel} · ${it.domain}/${it.env} — ${it.summary}`,
    ),
    '',
    'Constraints:',
    '- Assisted only — propose steps; request operator approval for actuation',
    '- Mute/silence is separate L2 — do not treat silence as fixed',
    '- D10: never place_order / arm daemon / enable live trading',
    '',
    '--- Per-item context (first) ---',
    buildAttentionRemediationPrompt(group.items[0]),
  ]
  return head.join('\n')
}
