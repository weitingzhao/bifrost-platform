import type { PlanNode } from '@/api/planSnapshot'

export function responsibility(item: PlanNode | undefined) {
  const r = item?.view?.next_responsible
  return r ? `${r.kind} · ${r.actor_ref || r.seat_ref}${r.provider ? ` · ${r.provider}` : ''} (plan default)` : 'Unknown'
}
