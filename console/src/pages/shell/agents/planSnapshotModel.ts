import type { PlanNode } from '@/api/planSnapshot'

export function responsibility(item: PlanNode | undefined) {
  const r = item?.view?.next_responsible
  return r ? `${r.kind} · ${r.actor_ref || r.seat_ref}${r.provider ? ` · ${r.provider}` : ''} (plan default)` : 'Unknown'
}

// PV2 reading of a binding, an item or a milestone: "true" only from a trusted, fresh source.
export function validity(v: string | undefined) {
  return v === 'true' ? 'Verified' : v === 'false' ? 'Not verified' : 'Unknown'
}
