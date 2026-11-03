import { useQuery } from '@tanstack/react-query'
import { approvalNumbers } from '@/api/agentRuntimePlan'
import { fetchApprovalList, isAwaitingDecision, type ApprovalItem } from '@/api/approvals'
import { isLandingAction } from '@/api/landingGrant'
import type { PlanNode, PlanSnapshot } from '@/api/planSnapshot'
import { fetchQuestionList, type Question } from '@/api/questions'
import { isStale, shellRead, SHELL_POLL_MS, type ShellRead } from './moduleDetailModel'
import { ruleQuestionIds, type HeldItem } from './planModuleModel'

/*
 * What asks the Owner on a step besides the plan view (R5:5.5-10 slice 2, the Owner 2026-11-02, Q75: a step that waits
 * on you reads "Waiting on you"): the open questions and the requests awaiting his decision. They are the lists the
 * shell already polls for Needs you (useNeedsYou: questions and requests every 30 s), read from that cache with the
 * same keys and never fetched (enabled is false), the same shell reads the module page shows as its sources
 * (moduleDetailModel ShellRead, read now while not stale: isStale, two polls): the 10-31 PROD platform-api OOM came
 * from Work's reads, so the Work page adds no request.
 *
 * A question is on a step when it is open and either names one of the step's own tasks (task_id: a landing that failed
 * asks "Try again" this way) or is named by the step's done rule (question_answered). A request is on a step when its
 * done rule names its number (approval_executed) and it still awaits a decision as Needs you counts one: pending and
 * not expired (isAwaitingDecision), not a drill (never approved), not a landing request (a landing's grant is read from
 * the landing's own phase, awaiting_grant, and Needs you confirms a landing request apart). Nothing else ties a question
 * or a request to a step, so nothing else is read as one.
 */

/** The two shell reads that can ask the Owner on a step: the questions, and the requests open. */
export type OwnerAsks = { questions: ShellRead<Question[]>; requests: ShellRead<ApprovalItem[]> }

/** The shell's two reads, from the cache only (the Work page; the module page passes the reads it already holds). */
export function useOwnerAsks(): OwnerAsks {
  const questions = useQuery({ queryKey: ['questions', 'all'], queryFn: () => fetchQuestionList('all'), enabled: false, retry: false })
  const requests = useQuery({ queryKey: ['approvals', 'open'], queryFn: () => fetchApprovalList('open'), enabled: false, retry: false })
  return { questions: shellRead(questions), requests: shellRead(requests) }
}

/** The most of a question's title a line quotes; its page has it whole. */
const TITLE_CHARS = 80
const cut = (s: string) => { const c = [...s]; return c.length > TITLE_CHARS ? `${c.slice(0, TITLE_CHARS).join('')}…` : s }

/** What asks the Owner on one step from the two reads, and the reads that could have and are not read now (with why). */
export type StepAsks = { items: HeldItem[]; unread: string[] }

/**
 * The open questions and the requests awaiting a decision on a step. A read is used only when something could tie it
 * to the step (the step has tasks or its done rule names a question, for questions; its done rule names a request, for
 * requests); one not read now (not read yet, being read, failed, offline, or stale: older than two polls) is named in
 * unread, so its absence is never taken for "none".
 */
export function stepAsks(view: PlanSnapshot, step: PlanNode, asks: OwnerAsks, now: number): StepAsks {
  const items: HeldItem[] = [], unread: string[] = []
  const tasks = new Map(view.tasks.filter(t => t.plan_node_id === step.plan_node_id).map(t => [t.task_id, t.title || t.task_id]))
  const named = new Set(ruleQuestionIds(step.done_rule_json))
  const numbers = new Set(approvalNumbers(step.done_rule_json))
  const current = <T>(name: string, read: ShellRead<T[]>, poll: number): T[] | null => {
    if (read.state === 'ok' && !isStale(read, poll, now)) return read.data
    unread.push(`${name}: ${read.state === 'ok' ? `last read ${Math.round((now - read.at) / 1000)} s ago (stale)`
      : read.state === 'unknown' ? read.reason : read.state === 'reading' ? 'being read' : 'not read yet'}`)
    return null
  }
  const questions = tasks.size > 0 || named.size > 0 ? current('Questions', asks.questions, SHELL_POLL_MS.questions) : []
  for (const q of questions ?? []) {
    if (q.status !== 'open') continue
    const on = q.task_id && tasks.has(q.task_id) ? `on task ${tasks.get(q.task_id)}` : named.has(q.id) ? 'its done rule names it' : ''
    if (on) items.push({ kind: 'question', text: `question #${q.number} “${cut(q.title)}” waits for your answer (${on})` })
  }
  const requests = numbers.size > 0 ? current('Requests', asks.requests, SHELL_POLL_MS.open) : []
  for (const r of requests ?? []) {
    if (r.number == null || !numbers.has(r.number) || r.drill || isLandingAction(r.action) || !isAwaitingDecision(r, now)) continue
    items.push({ kind: 'request', text: `request #${r.number} waits for your decision (its done rule names it)` })
  }
  return { items, unread }
}
