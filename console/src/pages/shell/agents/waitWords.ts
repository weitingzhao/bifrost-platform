import type { ExplainRead, PlanSnapshot } from '@/api/planSnapshot'
import type { ExplainBlocker } from '@/api/planExplainSchema'
import { approvalHref, questionHref } from '@/lib/shell/consoleRoutes'

type Task = PlanSnapshot['tasks'][number]
export type WaitWords = { text: string; since: string; age: number | undefined; href: string; tone: 'attention' | 'info' }

/** The same recorded wait in Live and on a step. Time is displayed in the reader's local timezone. */
export function waitWords(task: Task, explain: ExplainRead | undefined = undefined, now = Date.now()): WaitWords {
  const w = task.waiting
  // A finished task has no wait, except an accepted one on its landing: the API records
  // that wait only for accepted tasks (store/planviewwaits.go landingWait).
  const finished = ['accepted', 'rejected', 'cancelled'].includes(task.state) && !(task.state === 'accepted' && w?.on === 'landing')
  if (!w || task.on_main || finished) return { text: '', since: '', age: undefined, href: '', tone: 'info' }
  const age = w.since ? Math.max(0, Math.floor((now - Date.parse(w.since)) / 60_000)) : undefined
  const since = w.since ? `since ${new Date(w.since).toTimeString().slice(0, 5)} · ${age} min` : 'since Unknown'
  let href = ''
  let words = w.reason
  switch (w.on) {
    case 'owner':
      words = `Waits on the Owner${w.question_id ? ` · question ${w.question_id}` : ''}`
      href = w.question_id ? questionHref(w.question_id) : ''
      break
    case 'checks': words = `Waits on checks: ${(w.checks ?? []).map(c => `${c.role} t_…${c.task_id.slice(-6)} ${c.state}`).join(', ')}`; break
    case 'landing': {
      if (w.slot_holder) {
        const h = w.slot_holder
        words = `Waits for the slot · held by ${h.title} (${h.phase})`
        break
      }
      words = `Waits on landing: ${task.landing?.phase || w.reason}${task.landing?.run ? ` · CI run ${task.landing.run}` : ''}`
      const findGrant = (blockers: ExplainBlocker[]): Extract<ExplainBlocker, { state: 'landing_pending' }>['grant'] => {
        for (const b of blockers) {
          if (b.atom === 'one_of') { const grant = findGrant(b.branches.flat()); if (grant) return grant }
          else if ((b.state === 'landing_pending' || b.state === 'landing_main_unsettled') && b.task_id === task.task_id && b.grant) return b.grant
        }
        return null
      }
      if (explain?.ok) {
        const grant = findGrant(Object.values(explain.explain.steps).flatMap(s => s.kind === 'explained' ? s.blockers : []))
        if (grant) { href = approvalHref(grant.approval_id); words += ` · Request #${grant.number} · ${grant.last_status}` }
      }
      break
    }
    case 'stop': words = `Stopped by ${task.stopped?.by || 'Unknown'}: ${w.reason}`; break
    case 'host': case 'queued': {
      const e = w.executor
      const target = e && [e.pool, e.executor, e.host_id].filter(Boolean).join(' · ')
      words = `Waits for an executor slot${target ? ` · ${target}` : w.reason ? ` · ${w.reason}` : ''}`
      break
    }
    case 'dependencies':
      words = w.depends_on?.length
        ? `Waits on dependencies: ${w.depends_on.map(d => `${d.title} (t_…${d.task_id.slice(-6)}, ${d.state})`).join('; ')}`
        : `Waits on dependencies${w.reason ? ` · ${w.reason}` : ''}`
      break
  }
  return { text: `${words} · ${since}`, since, age, href, tone: w.on === 'owner' || w.on === 'stop' ? 'attention' : 'info' }
}
