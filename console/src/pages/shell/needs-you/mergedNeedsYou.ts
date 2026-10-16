import { actionLink, type OwnerNow } from '@/api/agentProgress'
import { record, text } from '@/api/agentRuntime'
import type { Question } from '@/api/questions'
import { questionHref } from '@/lib/shell/consoleRoutes'
import type { NeedsYouCount } from './needsYouModel'

export type WaitingItem = { id: string; title: string; href: string; instruction: string; kind?: string; since?: string; coordinator?: OwnerNow }

export function runtimeWaiting(items: unknown[], questions: readonly Question[]): WaitingItem[] {
  const open = new Set(questions.filter(q => q.status === 'open').map(q => q.id))
  return items.flatMap((value, i) => {
    const item = record(value), action = record(item.action), qid = text(action.question_id)
    if (qid && open.has(qid)) return []
    return [{ id: text(item.id) || `runtime-${i}`, title: text(item.title) || 'Untitled runtime request',
      kind: text(item.kind), since: text(item.since), href: qid ? questionHref(qid) : '#needs-you',
      instruction: qid ? 'Console question · Choose an option or write an answer' : 'Needs you · Review the runtime request' }]
  })
}
export function coordinatorWaiting(items: readonly OwnerNow[]): WaitingItem[] {
  return items.filter(item => item.approval == null).map((item, i) => {
    const link = item.actions?.map(actionLink).find(action => action.kind === 'anchor')
    return {
    id: `coordinator-${i}`, title: [item.name, item.work].filter(Boolean).join(' · ') || 'Coordinator request',
    href: link?.kind === 'anchor' ? link.href : '#needs-you',
    instruction: item.actions?.map(a => a.text).filter(Boolean).join(' · ') || 'Needs you · Follow up in the coordinator thread',
    since: item.since ? new Date(item.since * 1000).toISOString() : undefined, coordinator: item,
    }
  })
}
/** One total for the sidebar, home heading and Progress. A partial list never looks complete. */
export function mergedWaitingCount(sources: readonly NeedsYouCount[]): NeedsYouCount {
  const failed = sources.find(s => s.state === 'unknown')
  if (failed) return failed
  if (sources.some(s => s.state === 'loading')) return { state: 'loading' }
  return { state: 'known', count: sources.reduce((n, s) => n + (s.state === 'known' ? s.count : 0), 0) }
}
