import type { PlanSnapshot } from '@/api/planSnapshot'

type Change = PlanSnapshot['recent_changes'][number]
const record = (v: unknown): Record<string, unknown> => v !== null && typeof v === 'object' && !Array.isArray(v) ? v as Record<string, unknown> : {}
const str = (v: unknown) => typeof v === 'string' ? v : ''

/** The change in words: the event's own text when it has one, else its type. */
export function changeText(e: Change): string {
  return str(e.body_json.text) || (e.event_type.charAt(0).toUpperCase() + e.event_type.slice(1)).replace(/_/g, ' ')
}
/** What the change is about, when the event names it: an external run and its step, a node, a decision. */
export function changeRef(e: Change, view: PlanSnapshot): string {
  const work = record(e.body_json.external_work)
  if (str(work.source_run_key)) {
    const node = view.nodes.find(n => n.plan_node_id === str(work.plan_node_id))
    return [str(work.source_run_key), node?.label].filter(Boolean).join(' · ')
  }
  if (str(e.body_json.label)) return `${str(e.body_json.label)} ${str(e.body_json.title)}`.trim()
  return str(e.body_json.decision)
}
export function clock(at: string, now: number): string {
  const d = new Date(at)
  if (!Number.isFinite(d.getTime())) return at
  const time = d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false })
  return d.toDateString() === new Date(now).toDateString() ? time : `${d.toLocaleDateString([], { month: 'short', day: 'numeric' })} ${time.slice(0, 5)}`
}

