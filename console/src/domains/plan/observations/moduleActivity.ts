import { plainTitle } from '@/api/agentRuntimePlan'
import { isUnsetDecidedAt, type ApprovalItem } from '@/api/approvals'
import type { PlanSnapshot } from '@/api/planSnapshot'
import { optionLabel, questionNumberLabel, type Question } from '@/api/questions'
import { approvalHref, questionHref } from '@/lib/shell/consoleRoutes'
import { planNodeHref } from '@/domains/plan/display/usePlanSnapshot'

type Scope = { nodeIds: Set<string>; labels: Set<string> }
type Link = { step: string; via: string }
type ShellRead<T> = { state: 'ok'; data: T } | { state: 'reading' | 'unread' | 'unknown' }
type EvidenceReaders<S> = {
  questionEvidence: (id: string, view: PlanSnapshot, scopes: Map<string, S>) => Link[]
  approvalEvidence: (approval: ApprovalItem, scopes: Map<string, S>) => Link[]
  within: (links: Link[], scope: S) => Link | null
  touched: (event: PlanSnapshot['recent_changes'][number]) => string[]
  str: (value: unknown) => string
  cap: (value: string) => string
  isRecord: (value: unknown) => value is Record<string, unknown>
  STEP_WORDS: Record<string, string>
}

export type ActivityRow = { key: string; at: string; kind: string; text: string; href: string; source: string }

/** Display-only projection. Evidence linkage is supplied by the unchanged caller. */
export function createModuleActivity<ModuleScope extends Scope>({ questionEvidence, approvalEvidence, within, touched, str, cap, isRecord, STEP_WORDS }: EvidenceReaders<ModuleScope>) {
/** One line of Recent activity: when, what kind, what happened, and where it opens. */


/**
 * The plan events of the snapshot's recent window that this module's nodes own. Nothing is counted of the
 * rest: what is not shown here is not said to be anything. A plan change's events in the window (its proposal, its
 * decision, the change applied) are one group, joined by the ids they name (proposal_event_id, decided_event_id; an
 * event whose id is missing or not a positive integer joins no group and stands on its own evidence); the group is this
 * module's when any of them has direct evidence: the nodes the proposal touches, the question the
 * decision answers, the nodes the change added. Then every event of the group in the window is shown.
 */
/** An event id an event names: a positive integer, else 0 (none). */
const eventRef = (v: unknown) => typeof v === 'number' && Number.isSafeInteger(v) && v > 0 ? v : 0

function planActivity(view: PlanSnapshot, scope: ModuleScope, scopes: Map<string, ModuleScope>): { rows: ActivityRow[] } {
  const byId = new Map(view.nodes.map(n => [n.plan_node_id, n]))
  const byStateEvent = new Map(view.nodes.filter(n => n.state_event_id != null && scope.nodeIds.has(n.plan_node_id)).map(n => [n.state_event_id!, n]))
  const ordered = [...view.recent_changes].sort((a, b) => a.event_id - b.event_id)
  // Plan changes first: the group of each event and whether any event of a group is this module's.
  const groupOf = new Map<number, string>(), mineGroups = new Set<string>()
  for (const e of ordered) {
    const b = e.body_json
    let group = '', mine = false
    if (e.event_type === 'plan_change_proposed') {
      group = `p${e.event_id}`
      mine = touched(e).some(n => scope.nodeIds.has(n))
    } else if (e.event_type === 'plan_change_decided') {
      const proposal = eventRef(b.proposal_event_id)
      group = proposal ? groupOf.get(proposal) ?? `p${proposal}` : `e${e.event_id}`
      const q = str(b.question_id)
      mine = q !== '' && within(questionEvidence(q, view, scopes), scope) !== null
    } else if (e.event_type === 'plan_changed') {
      const decided = eventRef(b.decided_event_id)
      group = decided ? groupOf.get(decided) ?? `d${decided}` : `e${e.event_id}`
      mine = Array.isArray(b.nodes) && b.nodes.some(n => typeof n === 'string' && scope.nodeIds.has(n))
    }
    if (!group) continue
    groupOf.set(e.event_id, group)
    if (mine) mineGroups.add(group)
  }
  const rows: ActivityRow[] = []
  for (const e of ordered) {
    const b = e.body_json
    const row = (kind: string, words: string, href = '') => rows.push({ key: `plan-${e.event_id}`, at: e.at, kind, text: words, href, source: 'plan' })
    const ours = mineGroups.has(groupOf.get(e.event_id) ?? '')
    switch (e.event_type) {
      case 'plan_node_created': case 'plan_node_retitled': {
        const label = str(b.label)
        if (!scope.labels.has(label)) break
        const node = view.nodes.find(n => n.label === label)
        row(e.event_type === 'plan_node_created' ? 'Created' : 'Retitled', `${label} · ${plainTitle(str(b.title))}`, node ? planNodeHref(view.root, node.plan_node_id) : '')
        break
      }
      case 'plan_node_state_changed': {
        const node = byStateEvent.get(e.event_id)
        if (!node) break
        row('Step', `${node.label} · ${STEP_WORDS[str(b.from)] ?? str(b.from)} → ${STEP_WORDS[str(b.to)] ?? str(b.to)}${str(b.cause) ? ` · ${str(b.cause)}` : ''}`, planNodeHref(view.root, node.plan_node_id))
        break
      }
      case 'plan_change_proposed':
        if (ours) row('Plan change', `Proposed · ${plainTitle(str(b.reason))}`)
        break
      case 'plan_change_decided':
        if (ours) row('Plan change', `${cap(str(b.decision)) || 'Decided'}${str(b.by) ? ` by ${str(b.by)}` : ''}`)
        break
      case 'plan_changed':
        if (ours) row('Plan change', `Applied · ${str(b.change)}`)
        break
      case 'note': {
        const work = isRecord(b.external_work) ? b.external_work : null
        const observation = isRecord(b.plan_observation) ? b.plan_observation : null
        if (work && scope.nodeIds.has(str(work.plan_node_id))) {
          const record = view.external_work.find(w => w.external_work_id === str(work.external_work_id))
          row('External work', `${byId.get(str(work.plan_node_id))?.label ?? ''} · ${str(b.text) || str(work.type)}${record ? ` · ${record.description.task}` : ''}`)
        } else if (observation && scope.nodeIds.has(str(observation.step))) {
          row('Evidence', `${byId.get(str(observation.step))?.label ?? ''} · ${str(observation.binding)} reads ${str(observation.value) || 'Unknown'}`)
        }
        break
      }
    }
  }
  return { rows }
}

/** Questions about this module asked or answered (GET /questions, the shell's read). */
function questionActivity(view: PlanSnapshot, scope: ModuleScope, scopes: Map<string, ModuleScope>, questions: ShellRead<Question[]>): ActivityRow[] {
  if (questions.state !== 'ok') return []
  return questions.data.flatMap(q => {
    if (!within(questionEvidence(q.id, view, scopes), scope)) return []
    const name = `${questionNumberLabel(q.number)} · ${q.title}`
    const rows: ActivityRow[] = [{ key: `q-${q.id}-asked`, at: q.created_at, kind: 'Question', text: `Asked · ${name}`, href: questionHref(q.id), source: 'questions' }]
    if (q.answer) rows.push({ key: `q-${q.id}-answered`, at: q.answer.at, kind: 'Question', text: `Answered “${q.answer.option ? optionLabel(q, q.answer.option) : q.answer.text ?? ''}” · ${name}`, href: questionHref(q.id), source: 'questions' })
    return rows
  })
}

/** Requests about this module made or decided (GET /approvals?status=all, the shell's read). */
function approvalActivity(scope: ModuleScope, scopes: Map<string, ModuleScope>, approvals: ShellRead<ApprovalItem[]>): ActivityRow[] {
  if (approvals.state !== 'ok') return []
  return approvals.data.flatMap(a => {
    if (!within(approvalEvidence(a, scopes), scope)) return []
    const name = `Request #${a.number ?? '?'} · ${a.summary || a.action}`
    const rows: ActivityRow[] = [{ key: `a-${a.id}-made`, at: a.created_at, kind: 'Request', text: `Made · ${name}`, href: approvalHref(a.id), source: 'requests' }]
    if (!isUnsetDecidedAt(a.decided_at)) rows.push({ key: `a-${a.id}-decided`, at: a.decided_at!, kind: 'Request', text: `${cap(a.status)} · ${name}`, href: approvalHref(a.id), source: 'requests' })
    return rows
  })
}

/** Every source's rows, newest first; rows with no readable time last. */
function mergeActivity(...lists: ActivityRow[][]): ActivityRow[] {
  const time = (r: ActivityRow) => { const t = Date.parse(r.at); return Number.isFinite(t) ? t : -Infinity }
  return lists.flat().sort((a, b) => time(b) - time(a) || a.key.localeCompare(b.key))
}

return { planActivity, questionActivity, approvalActivity, mergeActivity }
}
