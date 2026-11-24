import type { ApprovalItem } from '@/api/approvals'
import type { Question } from '@/api/questions'

/*
 * Laws and actions apart (the Owner 2026-11-23, by the coordinator; 箭10 ruled the screen): a change of law or of
 * direction and a piece of executive work do not share one queue. The kind comes from the data, never from a title:
 * - a request's action: law_amend is the one card the Owner approves for a change of law (platform
 *   actions/law_amend.go); every other action is executive work;
 * - a question's owner_duty: goal and rule set direction and law; irreversible is executive. A question without one
 *   is filed under actions and says "kind not declared" until its source declares it.
 * When the approvals line and Plan agree a declared field, this reads that instead.
 */

export type TicketKind = 'law' | 'action'

export const LAW_ACTIONS: ReadonlySet<string> = new Set(['law_amend'])
const LAW_DUTIES: ReadonlySet<string> = new Set(['goal', 'rule'])
const ACTION_DUTIES: ReadonlySet<string> = new Set(['irreversible'])

export const KIND_NOT_DECLARED = 'kind not declared'

export function approvalKind(item: Pick<ApprovalItem, 'action'>): TicketKind {
  return LAW_ACTIONS.has(item.action.trim()) ? 'law' : 'action'
}

/** A question's kind and whether its source declared it (owner_duty set to a known duty). */
export function questionKind(q: Pick<Question, 'owner_duty'>): { kind: TicketKind; declared: boolean } {
  const duty = q.owner_duty?.trim().toLowerCase() ?? ''
  if (LAW_DUTIES.has(duty)) return { kind: 'law', declared: true }
  return { kind: 'action', declared: ACTION_DUTIES.has(duty) }
}

export function splitApprovals<T extends Pick<ApprovalItem, 'action'>>(items: readonly T[]): { laws: T[]; actions: T[] } {
  const laws: T[] = []
  const actions: T[] = []
  for (const item of items) (approvalKind(item) === 'law' ? laws : actions).push(item)
  return { laws, actions }
}

export function splitQuestions<T extends Pick<Question, 'owner_duty'>>(items: readonly T[]): { laws: T[]; actions: T[] } {
  const laws: T[] = []
  const actions: T[] = []
  for (const q of items) (questionKind(q).kind === 'law' ? laws : actions).push(q)
  return { laws, actions }
}

const plural = (n: number, one: string) => `${n} ${one}${n === 1 ? '' : 's'}`

/** "2 laws · 5 actions waiting"; a count that could not be read says "?", never 0. */
export function kindSummary(laws: number | null, actions: number | null): string {
  const part = (n: number | null, one: string) => (n == null ? `? ${one}s` : plural(n, one))
  return `${part(laws, 'law')} · ${part(actions, 'action')} waiting`
}
