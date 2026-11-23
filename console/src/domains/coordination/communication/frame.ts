import type { StatusWord, WorldTime } from '@bifrost/ui/city/world'
import type { CommunicationProjection, CommunicationSnapshot } from '@bifrost/ui/communication'
import type { TelemetryAlertsResponse } from '@/api/clusterTypes'
import { alertCount } from '@/api/alertCount'
import type { SharedObservation } from '@/domains/foundation/runtime-observation/sharedObservationStore'
import { cityHref } from '@/domains/owner/city/gen2/content/looks'

/**
 * What Agents hands the world frame (箭10 2026-11-23, cockpit/ui-world/frame): where you are, the time the view
 * shows and the alerts corner, all from reads the page already makes. The frame is the engine's (WorldFrame).
 */

/** Agents › the person in focus › the selected letter, by name; a request not in the loaded window says so. */
export function crumbsOf(snapshot: CommunicationSnapshot, projection: CommunicationProjection | null): { label: string; href?: string }[] {
  const out: { label: string; href?: string }[] = [{ label: 'Agents', href: '#collaboration' }]
  const name = (id?: string) => snapshot.members.find(m => m.id === id)?.label ?? id ?? 'Unknown member'
  if (projection?.scope.member) out.push({ label: name(projection.scope.member) })
  if (!projection?.requested) return out
  const x = projection.resolved ? projection.exchanges.find(e => e.channel + ':' + e.id === projection.resolved) : undefined
  return [...out, { label: x ? name(x.from) + ' → ' + name(x.to) : 'Not in loaded evidence' }]
}

/** Live is the newest ledger read; a fixed cutoff is pinned at that cutoff. */
export function frameTime(snapshot: CommunicationSnapshot, through: string): WorldTime {
  return through ? { mode: 'pinned', at: through } : { mode: 'live', ...(snapshot.coverage.updatedAt ? { at: snapshot.coverage.updatedAt } : {}) }
}

const WORD = { critical: 'fail', warning: 'warn', info: 'info' } as const
/** The alerts corner counted the one way every view counts (alertCount); not read is not 0. */
export function frameAlerts(data: TelemetryAlertsResponse | undefined, error: unknown): { count: number | null; worst?: StatusWord; why?: string } {
  if (error) return { count: null, why: error instanceof Error ? error.message : String(error) }
  if (!data) return { count: null, why: 'Reading alerts' }
  const c = alertCount(data.alerts)
  return { count: c.count, ...(c.worst ? { worst: WORD[c.worst] } : {}), why: 'From /api/v1/telemetry/alerts' }
}

/**
 * When Signals holds a pinned snapshot (the shared context above the routes, World's gap table item 9), the time it
 * was captured; else null. Agents' own reads stay live: the frame's second row only says Signals kept its pin.
 */
export function signalsPin(shared: SharedObservation): string | null {
  return shared.state === 'ready' && shared.observation.pinned ? shared.observation.snapshot.capturedAt : null
}

/**
 * Where "City" lands from Agents (brand layer item 8): the district of the domain the person in focus sits (else the
 * selected letter's sender), by their first membership with a domain. A thread with no domain known lands on the
 * whole city with that line picked on the Lines card (City reads sel=<session> there); anyone else, the whole city.
 * Memberships come from the frozen roster copy until the seat roster is served (then from that read).
 */
export function cityLanding(snapshot: CommunicationSnapshot, projection: CommunicationProjection | null): string {
  const x = projection?.resolved ? projection.exchanges.find(e => e.channel + ':' + e.id === projection.resolved) : undefined
  const who = projection?.scope.member || x?.from
  if (!who) return '#city'
  const domain = snapshot.members.find(m => m.id === who)?.memberships.find(m => m.domain)?.domain
  if (domain) return cityHref([domain])
  return who.startsWith('thread:') ? cityHref([], who.slice('thread:'.length)) : '#city'
}
