/**
 * Daily Ops Checklist auto-dispatch gates (Step-2 Wave 3).
 * full_auto → auto remediation; semi_auto → Operate Queue; manual/observe → notify.
 * IB feed (observe / D10) always skip.
 */
import {
  DAILY_OPS_CHECKLIST,
  type ChecklistItem,
  type FixCapability,
} from '@/lib/control-room/dailyOpsChecklistCatalog'

export type ChecklistDispatchGate = 'auto' | 'queue' | 'notify' | 'skip'

export type ChecklistItemSignal = {
  itemId: string
  signal: 'ok' | 'degraded' | 'fail' | 'unknown'
  detail?: string
  env?: string
}

export type ChecklistFixAction = {
  itemId: string
  label: string
  gate: ChecklistDispatchGate
  fixCapability: FixCapability
  fixScope: string | null
  detail: string
  skippedD10?: boolean
}

export function gateForFixCapability(cap: FixCapability): ChecklistDispatchGate {
  switch (cap) {
    case 'full_auto':
      return 'auto'
    case 'semi_auto':
      return 'queue'
    case 'manual':
    case 'observe':
      return 'notify'
    default:
      return 'notify'
  }
}

function findItem(itemId: string): ChecklistItem | undefined {
  for (const step of DAILY_OPS_CHECKLIST) {
    const hit = step.items.find(i => i.id === itemId)
    if (hit != null) return hit
  }
  return undefined
}

/**
 * Build fix actions for non-ok checklist signals.
 * Does not start jobs — caller (API / TaskControlCenter) executes gates.
 */
export function buildChecklistFixActions(
  signals: ChecklistItemSignal[],
  opts?: { dedupItemIds?: Set<string>; concurrentAutoRemaining?: number },
): ChecklistFixAction[] {
  const dedup = opts?.dedupItemIds ?? new Set<string>()
  let autoSlots = opts?.concurrentAutoRemaining ?? 1
  const out: ChecklistFixAction[] = []

  for (const sig of signals) {
    if (sig.signal !== 'fail' && sig.signal !== 'degraded') continue
    const item = findItem(sig.itemId)
    if (item == null) continue

    if (item.id === 'ib-feed' || item.fixCapability === 'observe') {
      out.push({
        itemId: item.id,
        label: item.label,
        gate: 'skip',
        fixCapability: item.fixCapability,
        fixScope: item.fixScope,
        detail: 'D10 observe — never auto-dispatch IB feed',
        skippedD10: true,
      })
      continue
    }

    let gate = gateForFixCapability(item.fixCapability)
    if (gate === 'notify') {
      out.push({
        itemId: item.id,
        label: item.label,
        gate: 'notify',
        fixCapability: item.fixCapability,
        fixScope: item.fixScope,
        detail: sig.detail?.trim() || 'Operator notify only',
      })
      continue
    }

    if (dedup.has(item.id)) {
      out.push({
        itemId: item.id,
        label: item.label,
        gate: 'skip',
        fixCapability: item.fixCapability,
        fixScope: item.fixScope,
        detail: 'dedup: dispatched within last 24h',
      })
      continue
    }

    let demotedBusy = false
    if (gate === 'auto' && autoSlots <= 0) {
      gate = 'queue'
      demotedBusy = true
    }
    if (gate === 'auto') autoSlots -= 1

    out.push({
      itemId: item.id,
      label: item.label,
      gate,
      fixCapability: item.fixCapability,
      fixScope: item.fixScope,
      detail: demotedBusy
        ? 'concurrent auto limit reached — demoted to Operate Queue'
        : sig.detail?.trim() || `${item.label} unhealthy`,
    })
  }

  return out
}

export function checklistDispatchGateLabel(gate: ChecklistDispatchGate): string {
  switch (gate) {
    case 'auto':
      return 'Auto'
    case 'queue':
      return 'Queued'
    case 'notify':
      return 'Notify'
    case 'skip':
      return 'Skip'
    default:
      return gate
  }
}
