import { ago } from '../progress/attemptActionWords'
export { ago } from '../progress/attemptActionWords'
import type { TelemetryAlertEntry } from '@/api/clusterTypes'
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

/** A waiting item from Needs you, as a row of Yours. */
export function waitingRow(item: WaitingItem): OwnerRow {
  return { id: item.id, title: item.title, detail: item.instruction, tone: 'attention', href: item.href, action: 'Open', since: item.since }
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
  lane: string
  nodes: { ready: number; total: number } | null
}): Verdict {
  const facts = [
    input.approvals == null ? 'approvals unknown' : input.approvals > 0 ? `${plural(input.approvals, 'request')} to approve` : 'Nothing to approve',
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

/** A time of day, with the date when it is not today. */
export function clockTime(ms: number, now: number): string {
  const d = new Date(ms)
  const hm = d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false })
  return d.toDateString() === new Date(now).toDateString() ? hm : `${d.toLocaleDateString([], { month: 'short', day: 'numeric' })} ${hm}`
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

/** When a read last answered, or that it has not since this page opened: what every failed read says (gap 3, review 488 B1). */
export function lastReadWords(readAt: number | null, openedAt: number, now: number): string {
  return readAt != null ? `last read ${clockTime(readAt, now)} (${ago(readAt, now)})` : `not read since this page opened at ${clockTime(openedAt, now)}`
}

/** A reason followed by more of the sentence: its own full stop dropped. */
export const reasonText = (reason: string): string => reason.replace(/\.+$/, '')

/** A read's last answer time from React Query: none before its first answer. */
export const answeredAt = (updatedAt: number): number | null => (updatedAt > 0 ? updatedAt : null)

/** When the runtime was last read, or when this page began waiting for it. */
export function runtimeUnreadLine(readAt: number | null, openedAt: number, now: number): string {
  return `The agent runtime could not be read; ${lastReadWords(readAt, openedAt, now)}.`
}
