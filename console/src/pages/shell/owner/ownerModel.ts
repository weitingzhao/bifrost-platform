import type { TelemetryAlertEntry } from '@/api/clusterTypes'
import type { RuntimeProgress } from '@/api/agentRuntime'
import { record, text } from '@/api/agentRuntime'
import type { RuntimeRound } from '@/api/agentRuntimePlan'
import type { ProgressPool } from '@/api/agentProgressExtra'
import { annotateExpectedNeutralAlerts, mapAlerts } from '@/lib/observability/alertMapping'
import type { MappedAlert } from '@/lib/observability/types'
import { mergedWaitingCount, type WaitingItem } from '@/pages/shell/needs-you/mergedNeedsYou'
import type { NeedsYouCount } from '@/pages/shell/needs-you/needsYouModel'
import type { ConsoleNavBadge } from '@/components/shell/ConsoleNavSlotItem'
import { standbyVerdict, type StandbyEvidence } from './standbyEvidence'

/**
 * The Owner Console (Owner 2026-10-16): what is his to do first, then what is worth a look, the
 * work, the system and the data. Each block answers "do I have to act, whose is it, what next";
 * the evidence stays on the detail pages.
 */

export type Tone = 'ok' | 'attention' | 'fail' | 'warn' | 'info' | 'muted' | 'unknown'

/** One row the Owner reads: what, since when, whose, and where to go. */
export type OwnerRow = {
  id: string
  title: string
  detail: string
  tone: Tone
  href?: string
  action?: string
  since?: string
  whose?: string
  /** A goal draft for the chief of staff; the Owner edits and sends it himself. */
  goalDraft?: string
  /** Kept by hand by the coordinator thread, not a system record. */
  hand?: boolean
}

/** Lamp colours of the console, one per tone. */
export const TONE_COLOR: Record<Tone, string> = {
  ok: 'var(--color-lamp-green)',
  attention: 'var(--color-lamp-yellow)',
  warn: 'var(--color-lamp-yellow)',
  fail: 'var(--color-lamp-red)',
  info: 'var(--color-info)',
  muted: 'var(--color-lamp-gray)',
  unknown: 'var(--color-lamp-gray)',
}

export function ago(at: string | number | undefined, now: number): string {
  const ms = typeof at === 'number' ? at : Date.parse(at ?? '')
  if (!Number.isFinite(ms)) return 'time unknown'
  const s = Math.max(0, Math.floor((now - ms) / 1000))
  if (s < 60) return 'just now'
  if (s < 3600) return `${Math.floor(s / 60)} min ago`
  if (s < 48 * 3600) return `${Math.floor(s / 3600)} h ago`
  return `${Math.floor(s / 86_400)} days ago`
}

/** Alerts only the Owner can clear (Owner 2026-10-16: the TWS login is his alone). */
const OWNER_ALERTS: Record<string, (labels: Record<string, string>) => { title: string; detail: string }> = {
  BifrostIBGatewayDisconnected: labels => ({
    title: `Log in to TWS for the IB ${labels.slot || 'gateway'} slot`,
    detail: 'The gateway has no TWS session; positions, account values and live streams stop behind it. Only you hold the TWS login.',
  }),
}

/** A plugin alert that is the same event as an Owner alert, told once. */
const SAME_EVENT: Record<string, (alert: MappedAlert) => boolean> = {
  BifrostIBGatewayDisconnected: alert => alert.name === 'BifrostPluginDegraded' && alert.labels.plugin === 'ib-gateway',
}

/** A waiting item from Needs you, as a row of Yours. Coordinator items are kept by hand. */
export function waitingRow(item: WaitingItem): OwnerRow {
  return { id: item.id, title: item.title, detail: item.instruction, tone: 'attention', href: item.href, action: 'Open', since: item.since, hand: item.coordinator != null }
}

export type AlertSplit = {
  yours: OwnerRow[]
  worth: OwnerRow[]
  /** Planned standby (proven), catalog noise, info and the watchdog: counted, not listed. */
  noise: number
  /** Told once with an Owner alert (the IB plugin degraded with the TWS login). */
  merged: number
  critical: number
  warning: number
}

function alertTitle(alert: MappedAlert): string {
  const l = alert.labels
  if (alert.name === 'TargetDown') return `${l.job || l.service || 'Scrape'} targets down`
  if (alert.name === 'KubeJobFailed') return `Job ${l.job_name || 'unknown'} failed`
  if (alert.name === 'KubePodNotReady') return `Pod ${l.pod || 'unknown'} not ready`
  if (alert.name.startsWith('KubeDaemonSet')) return `DaemonSet ${l.daemonset || 'unknown'} ${alert.name === 'KubeDaemonSetMisScheduled' ? 'mis-scheduled' : 'rollout stuck'}`
  return alert.annotations.summary || alert.name
}

function alertDetail(alert: MappedAlert, now: number): string {
  const l = alert.labels
  // kube-state-metrics' own pod label is the exporter's, not where the trouble is.
  const where = [l.namespace, l.pod && alert.name !== 'KubePodNotReady' && l.container !== 'kube-state-metrics' ? l.pod : ''].filter(Boolean).join(' · ')
  return [alert.severity, where, `since ${ago(alert.activeAt, now)}`].filter(Boolean).join(' · ')
}

/**
 * Firing alerts only, like the shell's verdict; a pending alert has not fired yet. Standby noise
 * needs the cluster's evidence (`standbyEvidence.ts`); without it every alert stays real.
 */
export function splitAlerts(alerts: readonly TelemetryAlertEntry[], evidence: StandbyEvidence | null, now: number): AlertSplit {
  const firing = alerts.filter(a => a.state === 'firing')
  const mapped = annotateExpectedNeutralAlerts(mapAlerts([...firing]))
  const out: AlertSplit = { yours: [], worth: [], noise: 0, merged: 0, critical: 0, warning: 0 }
  const real: MappedAlert[] = []
  const likely: MappedAlert[] = []
  for (const alert of mapped) {
    const severe = alert.severity === 'critical' || alert.severity === 'warning'
    const standby = severe && !alert.expectedNeutral ? standbyVerdict(alert, evidence) : 'noise'
    if (!severe || alert.expectedNeutral || standby === 'noise') out.noise += 1
    else if (standby === 'likely') likely.push(alert)
    else real.push(alert)
  }
  const ownerNames = new Set(real.filter(a => OWNER_ALERTS[a.name]).map(a => a.name))
  for (const alert of real) {
    if ([...ownerNames].some(name => SAME_EVENT[name]?.(alert))) {
      out.merged += 1
      continue
    }
    if (alert.severity === 'critical') out.critical += 1
    else out.warning += 1
    const tone: Tone = alert.severity === 'critical' ? 'fail' : 'warn'
    const owner = OWNER_ALERTS[alert.name]
    if (owner) {
      const said = owner(alert.labels)
      out.yours.push({ id: alert.id, title: said.title, detail: `${said.detail} Since ${ago(alert.activeAt, now)}.`, tone,
        since: alert.activeAt, href: '#ib', action: 'IB page' })
      continue
    }
    const title = alertTitle(alert)
    out.worth.push({ id: alert.id, title, detail: alertDetail(alert, now), tone, since: alert.activeAt, whose: 'No goal yet',
      goalDraft: `Find out why "${title}" (${alert.name}) has been firing since ${alert.activeAt?.slice(0, 16).replace('T', ' ') ?? 'it started'} UTC, and fix the cause or say why it should keep firing.` })
  }
  if (likely.length) {
    // Listed, as one row: the counts fit the standby node, but nothing says which pod is short.
    for (const alert of likely) {
      if (alert.severity === 'critical') out.critical += 1
      else out.warning += 1
    }
    const sets = [...new Set(likely.map(a => a.labels.daemonset).filter(Boolean))]
    const nodes = (evidence?.nodes ?? []).map(n => n.name).join(', ')
    const since = likely.map(a => a.activeAt ?? '').filter(Boolean).sort()[0]
    out.worth.push({ id: 'standby-daemonsets', title: `${plural(likely.length, 'DaemonSet alert')} that fit ${nodes} being on standby`,
      detail: `${sets.join(', ')} · since ${ago(since, now)}`, tone: likely.some(a => a.severity === 'critical') ? 'fail' : 'warn', since,
      whose: 'Likely the planned standby; not proven',
      goalDraft: `Check whether the DaemonSet alerts for ${sets.join(', ')} come only from ${nodes} being on standby; if so make them stop firing while it sleeps, otherwise fix the cause.` })
  }
  const order = (r: OwnerRow) => (r.tone === 'fail' ? 0 : 1)
  out.worth.sort((a, b) => order(a) - order(b) || (a.since ?? '').localeCompare(b.since ?? ''))
  return out
}

export type Verdict = { tone: Tone; headline: string; line: string }

export function plural(n: number, one: string, many = `${one}s`): string {
  return `${n} ${n === 1 ? one : many}`
}

/** The one sentence at the top: is anything mine, then the four facts behind "no". */
/**
 * Yours = Needs you's decision items (no silent threads) plus the alerts only the Owner can
 * clear. Either source loading or unread makes the whole count so (review 221 A1): an alert
 * list that could not be read may hold the TWS login, so it never adds up to "nothing".
 */
export function yoursCount(decisions: NeedsYouCount, ownerAlerts: NeedsYouCount): NeedsYouCount {
  return mergedWaitingCount([decisions, ownerAlerts])
}

export function ownerVerdict(input: {
  yours: NeedsYouCount
  approvals: number | null
  running: number | null
  lane: string
  nodes: { ready: number; total: number } | null
}): Verdict {
  const facts = [
    input.approvals == null ? 'approvals unknown' : input.approvals > 0 ? `${plural(input.approvals, 'request')} to approve` : 'Nothing to approve',
    input.running == null ? 'agents unknown' : input.running > 0 ? `${plural(input.running, 'agent')} running` : 'agents idle',
    input.lane,
    input.nodes == null ? 'nodes unknown' : `${input.nodes.ready} of ${input.nodes.total} nodes ready`,
  ]
  const line = facts.join(' · ')
  if (input.yours.state === 'loading') return { tone: 'muted', headline: 'Checking what is yours…', line }
  if (input.yours.state === 'unknown') {
    return { tone: 'unknown', headline: 'Could not read everything waiting for you', line: `${input.yours.reason} · ${line}` }
  }
  const n = input.yours.count
  if (n === 0) return { tone: 'ok', headline: 'Nothing needs you', line }
  return { tone: 'attention', headline: `${plural(n, 'thing')} ${n === 1 ? 'is' : 'are'} yours`, line }
}

export type WorkNow = {
  running: number
  stuck: number
  open: number
  /** The latest accepted implementation, and its mission's state when the open list has it. Accepted is not done (review 224 A-224-1). */
  lastAccepted: { mission: string; at: string; openState: string } | null
}

const STUCK_TASK = new Set(['blocked', 'rejected', 'failed'])
const STUCK_MISSION = new Set(['paused', 'blocked', 'failed'])

export function workNow(runtime: RuntimeProgress | undefined): WorkNow | null {
  if (!runtime) return null
  const missions = runtime.work.missions
  const stuck = missions.filter(m => STUCK_MISSION.has(m.state) || m.tasks.some(t => STUCK_TASK.has(t.state))).length
  const accepted = runtime.recent.tasks
    .filter(t => text(t.role) === 'implement' && text(t.state) === 'accepted' && text(t.mission_id))
    .sort((a, b) => text(b.ended_at).localeCompare(text(a.ended_at)))[0]
  const mission = text(accepted?.mission_id)
  return {
    running: runtime.live.length,
    stuck,
    open: missions.length,
    lastAccepted: accepted ? { mission, at: text(accepted.ended_at), openState: missions.find(m => m.mission_id === mission)?.state ?? '' } : null,
  }
}

/** A goal not yet done is read again every 30 s while its block is shown; a done one never changes (review 225 B-225-1). */
export const GOAL_STATE_REFRESH_MS = 30_000

export function goalRefreshMs(missionState: string): number | false {
  return missionState === 'done' ? false : GOAL_STATE_REFRESH_MS
}

export type LastResult = { mission: string; at: string; status: 'done' | 'open' | 'unknown'; state: string }

/**
 * What became of the latest accepted implementation's goal. Done only when the mission says so
 * (the goal read, `mission.state`); a mission still on the open list is open whatever its tasks
 * say; otherwise all that is known is that the implementation was accepted.
 */
export function lastResult(work: WorkNow | null, missionState: string): LastResult | null {
  const last = work?.lastAccepted
  if (!last) return null
  if (missionState === 'done') return { mission: last.mission, at: last.at, status: 'done', state: 'done' }
  const open = missionState || last.openState
  if (open) return { mission: last.mission, at: last.at, status: 'open', state: open }
  return { mission: last.mission, at: last.at, status: 'unknown', state: '' }
}

/** One line for it: "M-2 done 15 h ago", "M-7 implementation accepted 2 h ago; the goal is paused". */
export function lastResultLine(r: LastResult, now: number): string {
  if (r.status === 'done') return `${r.mission} done ${ago(r.at, now)}`
  const accepted = `${r.mission} implementation accepted ${ago(r.at, now)}`
  return r.status === 'open' ? `${accepted}; the goal is ${r.state}` : accepted
}

export type PlanRow = { key: string; name: string; done: number; total: number; tone: Tone; tag: '' | 'now' | 'next' }

export function planRows(round: RuntimeRound | undefined): PlanRow[] {
  if (!round) return []
  const firstTodo = round.items.findIndex(i => i.state === 'todo')
  return round.items.map((item, i) => {
    const live = record(item).live === true
    const tone: Tone = item.state === 'done' ? 'ok' : item.state === 'todo' ? 'muted' : 'info'
    return { key: item.key, name: item.name, done: item.done ?? 0, total: item.total ?? 0, tone,
      tag: live ? 'now' : i === firstTodo ? 'next' : '' }
  })
}

export type QuotaRow = { id: string; name: string; used: number | null; readAt: number | null; stale: boolean; tone: Tone }

/** A pool read more than a day ago is stale: shown grey, never red. */
export const QUOTA_STALE_MS = 24 * 3600 * 1000

export function quotaRows(pools: readonly ProgressPool[] | undefined, now: number): QuotaRow[] {
  return (pools ?? []).filter(p => !p.sublimit).map((pool, i) => {
    const readAt = pool.read_at != null && pool.read_at > 0 ? pool.read_at * 1000 : null
    const used = typeof pool.used === 'number' && Number.isFinite(pool.used) ? pool.used : null
    const stale = readAt == null || now - readAt > QUOTA_STALE_MS
    const tone: Tone = stale || used == null ? 'muted' : used >= 90 ? 'fail' : used >= 60 ? 'warn' : 'ok'
    return { id: pool.id || `pool-${i}`, name: [pool.vendor, pool.label].filter(Boolean).join(' · ') || 'Pool', used, readAt, stale, tone }
  })
}

export type DataTile = { name: string; tone: Tone; state: string; figure: string; detail: string; href: string }

/**
 * The one count on the whole site (design system v2 §10.1): what is yours, on For you and the app
 * badge. Loading shows nothing; a count that could not be read shows "?", never a zero.
 */
export function yoursNavBadge(count: NeedsYouCount): ConsoleNavBadge | undefined {
  if (count.state === 'loading') return undefined
  if (count.state === 'unknown') return { text: '?', title: `Yours: unknown — ${count.reason}`, tone: 'unknown' }
  if (count.count === 0) return undefined
  return { text: count.count > 99 ? '99+' : String(count.count), title: `Yours: ${count.count}`, tone: 'attention' }
}
