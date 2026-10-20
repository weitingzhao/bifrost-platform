import { record, text } from '@/api/agentRuntime'
import { recommendedLine, type Question } from '@/api/questions'
import { NEEDS_YOU_HREF, questionHref } from '@/lib/shell/consoleRoutes'
import type { NeedsYouCount } from './needsYouModel'

export type WaitingItem = { id: string; title: string; href: string; instruction: string; kind?: string; since?: string }

/** An open question as a waiting item: what it recommends and why, when it recommends an option. */
export function openQuestionItem(q: Question): WaitingItem {
  return { id: q.id, title: q.title, href: questionHref(q.id), instruction: `Console question · ${recommendedLine(q) ?? 'Choose an option or write an answer'}` }
}

export function runtimeWaiting(items: unknown[], questions: readonly Question[]): WaitingItem[] {
  const open = new Set(questions.filter(q => q.status === 'open').map(q => q.id))
  return items.flatMap((value, i) => {
    const item = record(value), action = record(item.action), qid = text(action.question_id)
    if (qid && open.has(qid)) return []
    return [{ id: text(item.id) || `runtime-${i}`, title: text(item.title) || 'Untitled runtime request',
      kind: text(item.kind), since: text(item.since), href: qid ? questionHref(qid) : NEEDS_YOU_HREF,
      instruction: qid ? 'Console question · Choose an option or write an answer' : 'For you · Review the runtime request' }]
  })
}
/** One total for the sidebar, home heading and Progress. A partial list never looks complete. */
export function mergedWaitingCount(sources: readonly NeedsYouCount[]): NeedsYouCount {
  const failed = sources.find(s => s.state === 'unknown')
  if (failed) return failed
  if (sources.some(s => s.state === 'loading')) return { state: 'loading' }
  return { state: 'known', count: sources.reduce((n, s) => n + (s.state === 'known' ? s.count : 0), 0) }
}
