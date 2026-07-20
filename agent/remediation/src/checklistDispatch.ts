/**
 * Runner-side checklist dispatch helpers (mirrors console checklistDispatch + Go PlanActions).
 * Execution is performed by platform-api when report_checklist_signals is called with auto_dispatch=true.
 * This module documents gate rules and can dry-run plan locally.
 */

export type FixCapability = 'full_auto' | 'semi_auto' | 'manual' | 'observe'
export type DispatchGate = 'auto' | 'queue' | 'notify' | 'skip'

export type PlannedAction = {
  itemId: string
  gate: DispatchGate
  fixScope: string | null
  detail: string
  skippedD10?: boolean
}

const ITEM_META: Record<string, { capability: FixCapability; scope: string | null }> = {
  'cluster-api': { capability: 'semi_auto', scope: 'cluster_issues_full_auto' },
  'nodes-ready': { capability: 'semi_auto', scope: 'cluster_issues_full_auto' },
  'failing-pods': { capability: 'full_auto', scope: 'cluster_issues_full_auto' },
  'platform-api': { capability: 'full_auto', scope: 'platform-self-health-recover' },
  'platform-console': { capability: 'full_auto', scope: 'platform-self-health-recover' },
  'argo-apps': { capability: 'semi_auto', scope: 'platform-self-health-recover' },
  'runners-ha': { capability: 'semi_auto', scope: 'operator-plane-remediate' },
  'git-bridge': { capability: 'semi_auto', scope: 'git-dirty-remediate' },
  'mac-probe-bridge': { capability: 'manual', scope: null },
  postgres: { capability: 'semi_auto', scope: 'cluster_issues_full_auto' },
  redis: { capability: 'full_auto', scope: 'cluster_issues_full_auto' },
  'nginx-edge': { capability: 'full_auto', scope: 'cluster_issues_full_auto' },
  'trade-apis': { capability: 'full_auto', scope: 'cluster_issues_full_auto' },
  'deliver-pipeline': { capability: 'full_auto', scope: 'deliver-stg-recover' },
  'stg-smoke': { capability: 'semi_auto', scope: 'deliver-stg-recover' },
  'massive-polygon': { capability: 'semi_auto', scope: 'cluster_issues_full_auto' },
  'ib-feed': { capability: 'observe', scope: null },
  'hermes-tooling': { capability: 'semi_auto', scope: 'operator-plane-remediate' },
}

export function gateForCapability(cap: FixCapability): DispatchGate {
  if (cap === 'full_auto') return 'auto'
  if (cap === 'semi_auto') return 'queue'
  return 'notify'
}

/** Dry-run plan: concurrent limit 1, D10 skip IB, optional 24h dedup set. */
export function planChecklistDispatch(
  signals: Array<{ item_id: string; signal: string; detail?: string }>,
  opts?: { dedupIds?: Set<string>; concurrentAutoRemaining?: number },
): PlannedAction[] {
  const dedup = opts?.dedupIds ?? new Set<string>()
  let autoSlots = opts?.concurrentAutoRemaining ?? 1
  const out: PlannedAction[] = []

  for (const sig of signals) {
    if (sig.signal !== 'fail' && sig.signal !== 'degraded') continue
    const meta = ITEM_META[sig.item_id]
    if (meta == null) continue

    if (sig.item_id === 'ib-feed' || meta.capability === 'observe') {
      out.push({
        itemId: sig.item_id,
        gate: 'skip',
        fixScope: meta.scope,
        detail: 'D10 observe — never auto-dispatch IB feed',
        skippedD10: true,
      })
      continue
    }

    let gate = gateForCapability(meta.capability)
    if (gate === 'notify') {
      out.push({
        itemId: sig.item_id,
        gate: 'notify',
        fixScope: meta.scope,
        detail: sig.detail ?? 'notify',
      })
      continue
    }
    if (dedup.has(sig.item_id)) {
      out.push({
        itemId: sig.item_id,
        gate: 'skip',
        fixScope: meta.scope,
        detail: 'dedup: within 24h',
      })
      continue
    }
    if (gate === 'auto' && autoSlots <= 0) gate = 'queue'
    if (gate === 'auto') autoSlots -= 1
    out.push({
      itemId: sig.item_id,
      gate,
      fixScope: meta.scope,
      detail: sig.detail ?? '',
    })
  }
  return out
}
